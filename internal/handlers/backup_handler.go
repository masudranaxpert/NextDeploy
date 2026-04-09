package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"panel/internal/backup"
	"panel/internal/db"
	"panel/internal/rclone"

	"github.com/gofiber/fiber/v2"
)

func (p *Panel) AppBackupManual(c *fiber.Ctx) error {
	appID := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), appID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "app not found"})
	}

	destID, err := c.ParamsInt("destid")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid destination"})
	}

	backupType := strings.TrimSpace(c.FormValue("type"))
	if backupType != "volume" && backupType != "full" {
		return c.Status(400).JSON(fiber.Map{"error": "type must be 'volume' or 'full'"})
	}

	dest, err := p.DB.GetBackupDestination(c.UserContext(), int64(destID))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "destination not found"})
	}

	historyID, err := p.DB.CreateBackupHistory(c.UserContext(), app.ID, dest.ID, backupType, "", "running", "", 0)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	go p.runBackupJob(context.Background(), historyID, app.ID, dest, backupType)

	return c.JSON(fiber.Map{"message": "backup started", "history_id": historyID})
}

func (p *Panel) runBackupJob(ctx context.Context, historyID int64, appID string, dest db.BackupDestination, backupType string) {
	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		_ = p.DB.UpdateBackupHistoryStatus(ctx, historyID, "failed", err.Error())
		return
	}

	var configMap map[string]string
	if err := json.Unmarshal([]byte(dest.Config), &configMap); err != nil {
		_ = p.DB.UpdateBackupHistoryStatus(ctx, historyID, "failed", "invalid config")
		return
	}

	var remotePath string
	var size int64

	switch backupType {
	case "volume":
		volumeName := strings.TrimSpace(app.Name) + "_data"
		tarPath, err := backup.BackupVolume(ctx, volumeName)
		if err != nil {
			_ = p.DB.UpdateBackupHistoryStatus(ctx, historyID, "failed", err.Error())
			return
		}

		remotePath = fmt.Sprintf("backups/%s/volumes/%s_%d.tar.gz", app.Name, volumeName, time.Now().Unix())

		switch dest.Provider {
		case "gdrive":
			err = rclone.UploadToGoogleDrive(ctx, configMap["token"], configMap["folder_id"], tarPath, remotePath)
		case "r2":
			err = rclone.UploadToCloudflareR2(ctx, configMap["account_id"], configMap["access_key_id"],
				configMap["secret_access_key"], configMap["bucket"], tarPath, remotePath)
		default:
			err = fmt.Errorf("unsupported provider: %s", dest.Provider)
		}

		if err != nil {
			_ = p.DB.UpdateBackupHistoryStatus(ctx, historyID, "failed", err.Error())
			return
		}

	case "full":
		tarPath, err := backup.BackupFullApp(ctx, app.Name, app.ComposeFile)
		if err != nil {
			_ = p.DB.UpdateBackupHistoryStatus(ctx, historyID, "failed", err.Error())
			return
		}

		remotePath = fmt.Sprintf("backups/%s/full/%s_%d.tar.gz", app.Name, app.Name, time.Now().Unix())

		switch dest.Provider {
		case "gdrive":
			err = rclone.UploadToGoogleDrive(ctx, configMap["token"], configMap["folder_id"], tarPath, remotePath)
		case "r2":
			err = rclone.UploadToCloudflareR2(ctx, configMap["account_id"], configMap["access_key_id"],
				configMap["secret_access_key"], configMap["bucket"], tarPath, remotePath)
		default:
			err = fmt.Errorf("unsupported provider: %s", dest.Provider)
		}

		if err != nil {
			_ = p.DB.UpdateBackupHistoryStatus(ctx, historyID, "failed", err.Error())
			return
		}
	}

	_ = p.DB.UpdateBackupHistoryStatus(ctx, historyID, "completed", "")
	_, _ = p.DB.ExecRaw(ctx, `UPDATE backup_history SET remote_path = ?, size_bytes = ? WHERE id = ?`, remotePath, size, historyID)
}

func (p *Panel) AppBackupHistory(c *fiber.Ctx) error {
	appID := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), appID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "app not found"})
	}

	history, err := p.DB.ListBackupHistory(c.UserContext(), app.ID, 50)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"history": history})
}

func (p *Panel) AppBackupRestore(c *fiber.Ctx) error {
	appID := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), appID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "app not found"})
	}

	historyID, err := c.ParamsInt("historyid")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid history id"})
	}

	history, err := p.DB.GetBackupHistory(c.UserContext(), int64(historyID))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "backup not found"})
	}

	if history.Status != "completed" {
		return c.Status(400).JSON(fiber.Map{"error": "backup not completed"})
	}

	dest, err := p.DB.GetBackupDestination(c.UserContext(), history.DestinationID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "destination not found"})
	}

	var configMap map[string]string
	if err := json.Unmarshal([]byte(dest.Config), &configMap); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "invalid config"})
	}

	go p.runRestoreJob(context.Background(), app, dest, history, configMap)

	return c.JSON(fiber.Map{"message": "restore started"})
}

func (p *Panel) runRestoreJob(ctx context.Context, app db.App, dest db.BackupDestination, history db.BackupHistory, configMap map[string]string) {
	var localPath string
	var err error

	switch dest.Provider {
	case "gdrive":
		localPath, err = rclone.DownloadFromGoogleDrive(ctx, configMap["token"], history.RemotePath)
	case "r2":
		localPath, err = rclone.DownloadFromCloudflareR2(ctx, configMap["account_id"], configMap["access_key_id"],
			configMap["secret_access_key"], configMap["bucket"], history.RemotePath)
	default:
		return
	}

	if err != nil {
		return
	}

	switch history.BackupType {
	case "volume":
		volumeName := strings.TrimSpace(app.Name) + "_data"
		_ = backup.RestoreVolume(ctx, volumeName, localPath, true)
	case "full":
		_ = backup.RestoreFullApp(ctx, app.Name, app.ComposeFile, localPath)
	}
}

func (p *Panel) AppBackupScheduleCreate(c *fiber.Ctx) error {
	appID := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), appID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "app not found"})
	}

	destID, err := c.ParamsInt("destid")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid destination"})
	}

	backupType := strings.TrimSpace(c.FormValue("type"))
	cronExpr := strings.TrimSpace(c.FormValue("cron"))
	retention := c.QueryInt("retention", 7)

	if backupType != "volume" && backupType != "full" {
		return c.Status(400).JSON(fiber.Map{"error": "type must be 'volume' or 'full'"})
	}

	if cronExpr == "" {
		return c.Status(400).JSON(fiber.Map{"error": "cron expression required"})
	}

	id, err := p.DB.CreateBackupSchedule(c.UserContext(), app.ID, int64(destID), backupType, cronExpr, retention)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"id": id, "message": "schedule created"})
}

func (p *Panel) AppBackupScheduleList(c *fiber.Ctx) error {
	appID := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), appID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "app not found"})
	}

	schedules, err := p.DB.ListBackupSchedules(c.UserContext(), app.ID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"schedules": schedules})
}

func (p *Panel) AppBackupScheduleToggle(c *fiber.Ctx) error {
	scheduleID, err := c.ParamsInt("scheduleid")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid schedule id"})
	}

	enabled := c.QueryBool("enabled", true)
	if err := p.DB.UpdateBackupScheduleEnabled(c.UserContext(), int64(scheduleID), enabled); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"message": "schedule updated"})
}

func (p *Panel) AppBackupScheduleDelete(c *fiber.Ctx) error {
	scheduleID, err := c.ParamsInt("scheduleid")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid schedule id"})
	}

	if err := p.DB.DeleteBackupSchedule(c.UserContext(), int64(scheduleID)); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"message": "schedule deleted"})
}
