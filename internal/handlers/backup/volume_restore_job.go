package backup

import (
	"panel/internal/handlers/utils"
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"panel/internal/handlers"
	"panel/internal/volumex"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

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
// Empty root usually means the backup archive contained no files at the volume root (e.g. backup was taken while the volume was empty).
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
func (h *Handler) VolumeRestore(c *fiber.Ctx) error {
	wantJSON := strings.Contains(c.Get("Accept"), "application/json")
	// Read all multipart fields from the raw stream in one place. Calling
	// c.FormValue() before manual parsing can consume the body and break large uploads.
	vol, fromApp, tmpPath, syncR, archKind, err := parseVolumeRestoreMultipart(c, wantJSON)
	if err != nil {
		if wantJSON {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		utils.SetFlashError(c, err.Error())
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
		utils.SetFlashError(c, "Invalid volume name")
		return c.Redirect(buildBrowseURL(vol, fromApp))
	}

	if !wantJSON {
		if _, loaded := h.P.VolRestoreActive.LoadOrStore(vol, struct{}{}); loaded {
			if syncR != nil {
				_ = syncR.Close()
			}
			if tmpPath != "" {
				_ = os.Remove(tmpPath)
			}
			utils.SetFlashError(c, "Another restore is already running for this volume.")
			return c.Redirect(buildBrowseURL(vol, fromApp))
		}
		defer h.P.VolRestoreActive.Delete(vol)
		var msg string
		if syncR != nil {
			defer func() { _ = syncR.Close() }()
			msg = volumex.RestoreTarGz(c.UserContext(), vol, syncR)
		} else {
			defer func() { _ = os.Remove(tmpPath) }()
			msg = volumex.RestoreVolumeArchiveFromPath(c.UserContext(), vol, tmpPath, archKind, nil)
		}
		if msg != "" {
			utils.SetFlashError(c, msg)
		} else {
			utils.SetFlash(c, volumeRestoreFlashAfterSuccess(c.UserContext(), vol))
		}
		return c.Redirect(buildBrowseURL(vol, fromApp))
	}

	if _, loaded := h.P.VolRestoreActive.LoadOrStore(vol, struct{}{}); loaded {
		_ = os.Remove(tmpPath)
		return c.Status(409).JSON(fiber.Map{"error": "a restore is already running for this volume"})
	}
	jobScheduled := false
	defer func() {
		if !jobScheduled {
			h.P.VolRestoreActive.Delete(vol)
		}
	}()

	handedOff := false
	defer func() {
		if !handedOff {
			_ = os.Remove(tmpPath)
		}
	}()

	job := &handlers.VolumeRestoreJob{
		ID:             uuid.New().String(),
		Volume:         vol,
		Status:         handlers.VolRestoreStatusExtracting,
		ExtractPercent: 0,
	}
	h.P.PutVolRestoreJob(job)
	handedOff = true
	jobScheduled = true

	go func() {
		defer h.P.VolRestoreActive.Delete(vol)
		defer os.Remove(tmpPath)
		ctx := context.Background()
		msg := volumex.RestoreVolumeArchiveFromPath(ctx, vol, tmpPath, archKind, func(pct int) {
			job.SetExtractPct(pct)
		})
		job.Finish(msg)
		h.P.ExpireVolRestoreJob(job.ID, 15*time.Minute)
	}()

	return c.Status(202).JSON(fiber.Map{
		"ok":     true,
		"job_id": job.ID,
		"volume": vol,
	})
}

// VolumeRestoreStatus returns JSON progress for a background volume restore job.
// When the job completes successfully it also sets the p_flash cookie so the
// browser can redirect without putting the message in the URL.
func (h *Handler) VolumeRestoreStatus(c *fiber.Ctx) error {
	id := strings.TrimSpace(c.Query("id"))
	if id == "" {
		return c.Status(400).JSON(fiber.Map{"error": "missing job id"})
	}
	j := h.P.GetVolRestoreJob(id)
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
	if st == handlers.VolRestoreStatusCompleted && errMsg == "" {
		flashMsg := volumeRestoreFlashAfterSuccess(c.UserContext(), vol)
		utils.SetFlash(c, flashMsg)
		resp["flash"] = flashMsg
	}
	return c.JSON(resp)
}
