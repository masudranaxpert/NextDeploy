package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"panel/internal/volumex"

	"github.com/gofiber/fiber/v2"
)

// volumeRestoreBodyReader returns the raw POST body for multipart parsing.
func volumeRestoreBodyReader(c *fiber.Ctx) (io.Reader, error) {
	if r := c.Context().RequestBodyStream(); r != nil {
		return r, nil
	}
	return nil, errors.New("request body stream unavailable; ensure StreamRequestBody and DisablePreParseMultipartForm are enabled")
}

// maxVolumeRestoreBytes must match main.go BodyLimit for /volumes/restore.
const maxVolumeRestoreBytes int64 = 2 << 30 // 2 GiB

const (
	archiveKindTarGz = "tar-gz"
	archiveKindZip   = "zip"
)

// backupArchiveKind infers archive type from the uploaded filename (browser sends FileName).
func backupArchiveKind(filename string) (kind string, ok bool) {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(filename)))
	if base == "" {
		return archiveKindTarGz, true
	}
	if strings.HasSuffix(base, ".zip") {
		return archiveKindZip, true
	}
	if strings.HasSuffix(base, ".tar.gz") || strings.HasSuffix(base, ".tgz") {
		return archiveKindTarGz, true
	}
	if strings.HasSuffix(base, ".gz") {
		return archiveKindTarGz, true
	}
	return "", false
}

func tempPatternForArchive(kind string) string {
	if kind == archiveKindZip {
		return "vol-restore-*.zip"
	}
	return "vol-restore-*.tar.gz"
}

// parseVolumeRestoreMultipart returns archiveKind "zip" or "tar-gz".
func parseVolumeRestoreMultipart(c *fiber.Ctx, saveBackupToTemp bool) (vol string, fromApp string, tmpPath string, syncReader io.ReadCloser, archiveKind string, err error) {
	ct := c.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		return "", "", "", nil, "", errors.New("multipart form required")
	}
	boundary, ok := params["boundary"]
	if !ok || boundary == "" {
		return "", "", "", nil, "", errors.New("invalid multipart boundary")
	}

	body, err := volumeRestoreBodyReader(c)
	if err != nil {
		return "", "", "", nil, "", err
	}

	mr := multipart.NewReader(body, boundary)

	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", "", "", nil, "", err
		}

		switch part.FormName() {
		case "name", "volume":
			b, err := io.ReadAll(io.LimitReader(part, 512))
			_ = part.Close()
			if err != nil {
				return "", "", "", nil, "", err
			}
			vol = strings.TrimSpace(string(b))

		case "from_app":
			b, err := io.ReadAll(io.LimitReader(part, 512))
			_ = part.Close()
			if err != nil {
				return "", "", "", nil, "", err
			}
			fromApp = strings.TrimSpace(string(b))

		case "backup":
			filename := part.FileName()
			kind, ok := backupArchiveKind(filename)
			if !ok {
				_ = part.Close()
				return "", "", "", nil, "", errors.New("unsupported file type; use .tar.gz, .tgz, .gz, or .zip")
			}

			needTemp := saveBackupToTemp || kind == archiveKindZip || vol == ""
			if needTemp {
				f, err := os.CreateTemp(volumex.HostStagingDir(), tempPatternForArchive(kind))
				if err != nil {
					_ = part.Close()
					return "", "", "", nil, "", err
				}
				destPath := f.Name()
				n, copyErr := io.Copy(f, io.LimitReader(part, maxVolumeRestoreBytes+1))
				closeErr := f.Close()
				_ = part.Close()
				if copyErr != nil {
					_ = os.Remove(destPath)
					return "", "", "", nil, "", copyErr
				}
				if closeErr != nil {
					_ = os.Remove(destPath)
					return "", "", "", nil, "", closeErr
				}
				if n > maxVolumeRestoreBytes {
					_ = os.Remove(destPath)
					return "", "", "", nil, "", fmt.Errorf("backup exceeds maximum size (%d bytes)", maxVolumeRestoreBytes)
				}
				if vol == "" {
					tmpPath = destPath
					archiveKind = kind
					continue
				}
				return vol, fromApp, destPath, nil, kind, nil
			}
			return vol, fromApp, "", part, kind, nil

		default:
			_ = part.Close()
		}
	}

	if vol == "" {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
		return "", fromApp, "", nil, "", errors.New("volume name is required")
	}
	if tmpPath != "" {
		return vol, fromApp, tmpPath, nil, archiveKind, nil
	}
	return vol, fromApp, "", nil, "", errors.New("upload a backup archive (.tar.gz or .zip)")
}

// buildBrowseURL returns /volumes/browse?name=<vol>[&from_app=<app>].
func buildBrowseURL(vol, fromApp string) string {
	q := url.Values{}
	q.Set("name", vol)
	if fromApp != "" {
		q.Set("from_app", fromApp)
	}
	return "/volumes/browse?" + q.Encode()
}

// volumeRestoreFlashAfterSuccess builds a user-facing flash after a successful extract.
func volumeRestoreFlashAfterSuccess(ctx context.Context, vol string) string {
	entries, errMsg := volumex.ListDir(ctx, vol, "")
	if errMsg != "" {
		return "Volume restore completed. Could not verify contents: " + errMsg
	}
	if len(entries) == 0 {
		return "Volume restore completed. The volume root is empty — the archive likely had no files to restore (for example, a backup downloaded while the volume was already empty). Upload a backup that was created when the volume had data, or use a copy from remote backup history."
	}
	if len(entries) == 1 {
		return "Volume restore completed. 1 item at volume root."
	}
	return fmt.Sprintf("Volume restore completed. %d items at volume root.", len(entries))
}

// VolumeRestore uploads a backup into a Docker volume. With Accept: application/json
// the archive is saved and restore runs in the background; the response is 202 + job_id.
// Without that header, restore runs synchronously and redirects back to browse (legacy).
func (p *Panel) VolumeRestore(c *fiber.Ctx) error {
	wantJSON := strings.Contains(c.Get("Accept"), "application/json") || c.Get("X-NextDeploy-Client") == "cli"
	vol, fromApp, tmpPath, syncR, archKind, err := parseVolumeRestoreMultipart(c, wantJSON)
	if err != nil {
		if wantJSON {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		SetFlashError(c, err.Error())
		return c.Redirect(buildBrowseURL(vol, fromApp))
	}

	if !volumex.ValidVolumeName(vol) {
		if syncR != nil {
			_ = syncR.Close()
		}
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
		if wantJSON {
			return c.Status(400).JSON(fiber.Map{"error": "invalid volume"})
		}
		SetFlashError(c, "Invalid volume name")
		return c.Redirect(buildBrowseURL(vol, fromApp))
	}

	if !wantJSON {
		if _, loaded := p.VolRestoreActive.LoadOrStore(vol, struct{}{}); loaded {
			if syncR != nil {
				_ = syncR.Close()
			}
			if tmpPath != "" {
				_ = os.Remove(tmpPath)
			}
			SetFlashError(c, "Another restore is already running for this volume.")
			return c.Redirect(buildBrowseURL(vol, fromApp))
		}
		defer p.VolRestoreActive.Delete(vol)
		var msg string
		if syncR != nil {
			defer func() { _ = syncR.Close() }()
			msg = volumex.RestoreTarGz(c.UserContext(), vol, syncR)
		} else {
			defer func() { _ = os.Remove(tmpPath) }()
			msg = volumex.RestoreVolumeArchiveFromPath(c.UserContext(), vol, tmpPath, archKind, nil)
		}
		if msg != "" {
			SetFlashError(c, msg)
		} else {
			SetFlash(c, volumeRestoreFlashAfterSuccess(c.UserContext(), vol))
		}
		return c.Redirect(buildBrowseURL(vol, fromApp))
	}

	if _, loaded := p.VolRestoreActive.LoadOrStore(vol, struct{}{}); loaded {
		_ = os.Remove(tmpPath)
		return c.Status(409).JSON(fiber.Map{"error": "a restore is already running for this volume"})
	}
	jobScheduled := false
	defer func() {
		if !jobScheduled {
			p.VolRestoreActive.Delete(vol)
		}
	}()

	handedOff := false
	defer func() {
		if !handedOff {
			_ = os.Remove(tmpPath)
		}
	}()

	var jobIDBytes [16]byte
	_, _ = rand.Read(jobIDBytes[:])
	job := &VolumeRestoreJob{
		ID:             hex.EncodeToString(jobIDBytes[:]),
		Volume:         vol,
		Status:         VolRestoreStatusExtracting,
		ExtractPercent: 0,
	}
	p.PutVolRestoreJob(job)
	handedOff = true
	jobScheduled = true

	go func() {
		defer p.VolRestoreActive.Delete(vol)
		defer os.Remove(tmpPath)
		ctx := context.Background()
		msg := volumex.RestoreVolumeArchiveFromPath(ctx, vol, tmpPath, archKind, func(pct int) {
			job.SetExtractPct(pct)
		})
		job.Finish(msg)
		p.ExpireVolRestoreJob(job.ID, 15*time.Minute)
	}()

	return c.Status(202).JSON(fiber.Map{
		"ok":     true,
		"job_id": job.ID,
		"volume": vol,
	})
}

// VolumeRestoreStatus returns JSON progress for a background volume restore job.
func (p *Panel) VolumeRestoreStatus(c *fiber.Ctx) error {
	id := strings.TrimSpace(c.Query("id"))
	if id == "" {
		id = strings.TrimSpace(c.Query("job"))
	}
	if id == "" {
		id = strings.TrimSpace(c.Params("job"))
	}
	if id == "" {
		return c.Status(400).JSON(fiber.Map{"error": "missing job id"})
	}
	j := p.GetVolRestoreJob(id)
	if j == nil {
		return c.Status(404).JSON(fiber.Map{"error": "unknown or expired job"})
	}
	st, errMsg, pct, vol := j.Snapshot()
	resp := fiber.Map{
		"status":          st,
		"error":           errMsg,
		"extract_percent": pct,
		"volume":          vol,
	}
	if st == VolRestoreStatusCompleted && errMsg == "" {
		flashMsg := volumeRestoreFlashAfterSuccess(c.UserContext(), vol)
		SetFlash(c, flashMsg)
		resp["flash"] = flashMsg
	}
	return c.JSON(resp)
}

const (
	VolRestoreStatusExtracting = "extracting"
	VolRestoreStatusCompleted  = "completed"
	VolRestoreStatusFailed     = "failed"
)

type VolumeRestoreJob struct {
	Mu             sync.Mutex
	ID             string
	Volume         string
	Status         string
	ErrMsg         string
	ExtractPercent int
}

func (j *VolumeRestoreJob) SetExtractPct(p int) {
	j.Mu.Lock()
	defer j.Mu.Unlock()
	if p > j.ExtractPercent {
		j.ExtractPercent = p
	}
}

func (j *VolumeRestoreJob) Finish(errMsg string) {
	j.Mu.Lock()
	defer j.Mu.Unlock()
	j.ExtractPercent = 100
	if errMsg != "" {
		j.Status = VolRestoreStatusFailed
		j.ErrMsg = errMsg
	} else {
		j.Status = VolRestoreStatusCompleted
	}
}

func (j *VolumeRestoreJob) Snapshot() (status, errMsg string, pct int, volume string) {
	j.Mu.Lock()
	defer j.Mu.Unlock()
	return j.Status, j.ErrMsg, j.ExtractPercent, j.Volume
}

func (p *Panel) PutVolRestoreJob(j *VolumeRestoreJob) {
	p.VolRestoreMu.Lock()
	defer p.VolRestoreMu.Unlock()
	if p.volRestoreJobs == nil {
		p.volRestoreJobs = make(map[string]*VolumeRestoreJob)
	}
	p.volRestoreJobs[j.ID] = j
}

func (p *Panel) GetVolRestoreJob(id string) *VolumeRestoreJob {
	p.VolRestoreMu.Lock()
	defer p.VolRestoreMu.Unlock()
	return p.volRestoreJobs[id]
}

func (p *Panel) ExpireVolRestoreJob(id string, after time.Duration) {
	time.AfterFunc(after, func() {
		p.VolRestoreMu.Lock()
		delete(p.volRestoreJobs, id)
		p.VolRestoreMu.Unlock()
	})
}
