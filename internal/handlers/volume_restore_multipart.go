package handlers

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"

	"panel/internal/volumex"

	"github.com/gofiber/fiber/v2"
)

// volumeRestoreBodyReader returns the raw POST body for multipart parsing.
// Uses c.Context().RequestBodyStream() — the only fasthttp API that truly streams
// without buffering the entire body into RAM first.
// c.Body() and req.BodyStream() are intentionally avoided: both can trigger full
// in-memory buffering for multipart requests when DisablePreParseMultipartForm is false.
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
// Zip always uses a temp file; tar.gz may stream from the part when saveBackupToTemp is false.
func parseVolumeRestoreMultipart(c *fiber.Ctx, saveBackupToTemp bool) (vol string, tmpPath string, syncReader io.ReadCloser, archiveKind string, err error) {
	ct := c.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		return "", "", nil, "", errors.New("multipart form required")
	}
	boundary, ok := params["boundary"]
	if !ok || boundary == "" {
		return "", "", nil, "", errors.New("invalid multipart boundary")
	}

	body, err := volumeRestoreBodyReader(c)
	if err != nil {
		return "", "", nil, "", err
	}

	mr := multipart.NewReader(body, boundary)

	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", "", nil, "", err
		}

		switch part.FormName() {
		case "name":
			b, err := io.ReadAll(io.LimitReader(part, 512))
			_ = part.Close()
			if err != nil {
				return "", "", nil, "", err
			}
			vol = strings.TrimSpace(string(b))

		case "backup":
			if vol == "" {
				_ = part.Close()
				return "", "", nil, "", errors.New("put the volume name field before the backup file in the form")
			}
			filename := part.FileName()
			kind, ok := backupArchiveKind(filename)
			if !ok {
				_ = part.Close()
				return "", "", nil, "", errors.New("unsupported file type; use .tar.gz, .tgz, .gz, or .zip")
			}

			needTemp := saveBackupToTemp || kind == archiveKindZip
			if needTemp {
				f, err := os.CreateTemp(volumex.HostStagingDir(), tempPatternForArchive(kind))
				if err != nil {
					_ = part.Close()
					return "", "", nil, "", err
				}
				destPath := f.Name()
				n, copyErr := io.Copy(f, io.LimitReader(part, maxVolumeRestoreBytes+1))
				closeErr := f.Close()
				_ = part.Close()
				if copyErr != nil {
					_ = os.Remove(destPath)
					return "", "", nil, "", copyErr
				}
				if closeErr != nil {
					_ = os.Remove(destPath)
					return "", "", nil, "", closeErr
				}
				if n > maxVolumeRestoreBytes {
					_ = os.Remove(destPath)
					return "", "", nil, "", fmt.Errorf("backup exceeds maximum size (%d bytes)", maxVolumeRestoreBytes)
				}
				return vol, destPath, nil, kind, nil
			}
			return vol, "", part, kind, nil

		default:
			_ = part.Close()
		}
	}

	if vol == "" {
		return "", "", nil, "", errors.New("volume name is required")
	}
	return "", "", nil, "", errors.New("upload a backup archive (.tar.gz or .zip)")
}
