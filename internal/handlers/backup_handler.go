package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"panel/internal/backup"
	"panel/internal/db"
	"panel/internal/rclone"
	"panel/internal/volumex"

	"github.com/gofiber/fiber/v2"
)

func validateBackupDestinationConfig(provider string, m map[string]string) error {
	switch provider {
	case "gdrive":
		for _, k := range []string{"client_id", "client_secret", "token", "folder_id"} {
			if strings.TrimSpace(m[k]) == "" {
				return fmt.Errorf("missing %s in destination config", k)
			}
		}
	case "r2":
		for _, k := range []string{"account_id", "access_key_id", "secret_access_key", "bucket"} {
			if strings.TrimSpace(m[k]) == "" {
				return fmt.Errorf("missing %s in destination config", k)
			}
		}
	default:
		return fmt.Errorf("unsupported provider %q", provider)
	}
	return nil
}

func parseRequestedVolumeName(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, ",", "\n")
	parts := strings.Split(raw, "\n")
	var names []string
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !volumex.ValidVolumeName(part) {
			return "", fmt.Errorf("invalid Docker volume name %q", part)
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		names = append(names, part)
	}
	if len(names) == 0 {
		return "", nil
	}
	if len(names) > 1 {
		return "", errors.New("enter one exact Docker volume name for now")
	}
	return names[0], nil
}

func (p *Panel) resolveRequestedBackupVolume(ctx context.Context, app db.App, requested string) (string, string) {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		ok, msg := volumex.VolumeExists(ctx, requested)
		if !ok {
			if strings.TrimSpace(msg) == "" {
				msg = "Docker volume not found"
			}
			return "", msg
		}
		return requested, ""
	}
	volProjects := p.backupVolumeComposeProjects(ctx, app, app.ID)
	return volumex.ResolveBackupDataVolumeName(ctx, app.ID, app.Name, volProjects)
}

func (p *Panel) pruneRemoteBackups(ctx context.Context, dest db.BackupDestination, backupType, appID string, keepCount int) {
	if keepCount < 1 {
		return
	}
	rows, err := p.DB.ListOldBackupsForPrune(ctx, appID, dest.ID, backupType, keepCount)
	if err != nil || len(rows) == 0 {
		return
	}
	var configMap map[string]string
	if err := json.Unmarshal([]byte(dest.Config), &configMap); err != nil {
		log.Printf("backup prune config: %v", err)
		return
	}
	for _, h := range rows {
		if strings.TrimSpace(h.RemotePath) != "" {
			if err := rclone.DeleteRemoteObject(ctx, dest.Provider, configMap, h.RemotePath); err != nil {
				log.Printf("backup prune remote id=%d path=%s: %v", h.ID, h.RemotePath, err)
				continue
			}
		}
		if err := p.DB.DeleteBackupHistoryByID(ctx, h.ID); err != nil {
			log.Printf("backup prune db id=%d: %v", h.ID, err)
		}
	}
}

func backupVolumeNameFromHistory(history db.BackupHistory) string {
	if volumex.ValidVolumeName(strings.TrimSpace(history.VolumeName)) {
		return strings.TrimSpace(history.VolumeName)
	}
	base := filepath.Base(strings.TrimSpace(history.RemotePath))
	base = strings.TrimSuffix(base, ".tar.gz")
	if base == "" {
		return ""
	}
	parts := strings.Split(base, "-")
	if len(parts) >= 3 {
		candidate := strings.Join(parts[:len(parts)-2], "-")
		if volumex.ValidVolumeName(candidate) {
			return candidate
		}
	}
	if volumex.ValidVolumeName(base) {
		return base
	}
	return ""
}

const manualBackupRetention = 0 // do not auto-delete history rows after manual backups

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
	volumeName, err := parseRequestedVolumeName(c.FormValue("volume_names"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	if backupType != "volume" {
		volumeName = ""
	}

	dest, err := p.DB.GetBackupDestination(c.UserContext(), int64(destID))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "destination not found"})
	}

	historyID, err := p.DB.CreateBackupHistory(c.UserContext(), app.ID, dest.ID, backupType, "", "running", "", 0)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	go p.runBackupJob(context.Background(), historyID, app.ID, dest, backupType, volumeName, manualBackupRetention)

	return c.JSON(fiber.Map{"message": "backup started", "history_id": historyID})
}

// runBackupJob runs a backup; retention is max completed rows to keep per app+destination+type (0 = skip retention trim).
func (p *Panel) runBackupJob(ctx context.Context, historyID int64, appID string, dest db.BackupDestination, backupType, requestedVolumeName string, retention int) {
	var logBuffer strings.Builder
	logBuffer.WriteString(time.Now().Format(time.RFC3339) + " started\n")

	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		logBuffer.WriteString(fmt.Sprintf("Error: Failed to get app: %v\n", err))
		_ = p.DB.UpdateBackupHistoryStatusWithLog(ctx, historyID, "failed", err.Error(), logBuffer.String())
		return
	}

	var configMap map[string]string
	if err := json.Unmarshal([]byte(dest.Config), &configMap); err != nil {
		logBuffer.WriteString(fmt.Sprintf("Error: Invalid config: %v\n", err))
		_ = p.DB.UpdateBackupHistoryStatusWithLog(ctx, historyID, "failed", "invalid config", logBuffer.String())
		return
	}
	if err := validateBackupDestinationConfig(dest.Provider, configMap); err != nil {
		logBuffer.WriteString(fmt.Sprintf("Error: %v\n", err))
		_ = p.DB.UpdateBackupHistoryStatusWithLog(ctx, historyID, "failed", err.Error(), logBuffer.String())
		return
	}

	var remotePath string
	var size int64
	var tarPath string
	var actualVolumeName string
	defer func() {
		if strings.TrimSpace(tarPath) != "" {
			_ = os.Remove(tarPath)
		}
	}()

	switch backupType {
	case "volume":
		volumeName, vmsg := p.resolveRequestedBackupVolume(ctx, app, requestedVolumeName)
		if vmsg != "" {
			logBuffer.WriteString("error: " + vmsg + "\n")
			_ = p.DB.UpdateBackupHistoryStatusWithLog(ctx, historyID, "failed", vmsg, logBuffer.String())
			return
		}
		actualVolumeName = volumeName
		logBuffer.WriteString("archive " + volumeName + "\n")

		tarPath, err = backup.BackupVolume(ctx, volumeName)
		if err != nil {
			logBuffer.WriteString("error: " + err.Error() + "\n")
			_ = p.DB.UpdateBackupHistoryStatusWithLog(ctx, historyID, "failed", err.Error(), logBuffer.String())
			return
		}
		logBuffer.WriteString("tar " + filepath.Base(tarPath) + "\n")

		remotePath = path.Join(app.Name, "volumes", filepath.Base(tarPath))

		uploadProg := func(snap string) {
			s := logBuffer.String() + "\n── upload ──\n" + snap
			if len(s) > 65536 {
				s = "…\n" + s[len(s)-65536:]
			}
			_ = p.DB.UpdateBackupHistoryStatusWithLog(ctx, historyID, "running", "", s)
		}
		var uploadLog string
		switch dest.Provider {
		case "gdrive":
			uploadLog, err = rclone.UploadToGoogleDrive(ctx, configMap["client_id"], configMap["client_secret"],
				configMap["token"], configMap["folder_id"], tarPath, remotePath, uploadProg)
		case "r2":
			uploadLog, err = rclone.UploadToCloudflareR2(ctx, configMap["account_id"], configMap["access_key_id"],
				configMap["secret_access_key"], configMap["bucket"], tarPath, remotePath, uploadProg)
		default:
			err = fmt.Errorf("unsupported provider: %s", dest.Provider)
		}
		logBuffer.WriteString(uploadLog)

		if err != nil {
			logBuffer.WriteString(fmt.Sprintf("Error: Upload failed: %v\n", err))
			_ = p.DB.UpdateBackupHistoryStatusWithLog(ctx, historyID, "failed", err.Error(), logBuffer.String())
			return
		}
		if st, err := os.Stat(tarPath); err == nil {
			size = st.Size()
		}

	case "full":
		logBuffer.WriteString("full app " + app.Name + "\n")

		workspaceRoot := p.composeWorkspaceRoot(ctx, appID)
		tarPath, err = backup.BackupFullApp(ctx, app.Name, workspaceRoot)
		if err != nil {
			logBuffer.WriteString("error: " + err.Error() + "\n")
			_ = p.DB.UpdateBackupHistoryStatusWithLog(ctx, historyID, "failed", err.Error(), logBuffer.String())
			return
		}
		logBuffer.WriteString("tar " + filepath.Base(tarPath) + "\n")

		remotePath = path.Join(app.Name, "full", filepath.Base(tarPath))

		uploadProg := func(snap string) {
			s := logBuffer.String() + "\n── upload ──\n" + snap
			if len(s) > 65536 {
				s = "…\n" + s[len(s)-65536:]
			}
			_ = p.DB.UpdateBackupHistoryStatusWithLog(ctx, historyID, "running", "", s)
		}
		var uploadLog string
		switch dest.Provider {
		case "gdrive":
			uploadLog, err = rclone.UploadToGoogleDrive(ctx, configMap["client_id"], configMap["client_secret"],
				configMap["token"], configMap["folder_id"], tarPath, remotePath, uploadProg)
		case "r2":
			uploadLog, err = rclone.UploadToCloudflareR2(ctx, configMap["account_id"], configMap["access_key_id"],
				configMap["secret_access_key"], configMap["bucket"], tarPath, remotePath, uploadProg)
		default:
			err = fmt.Errorf("unsupported provider: %s", dest.Provider)
		}
		logBuffer.WriteString(uploadLog)

		if err != nil {
			logBuffer.WriteString(fmt.Sprintf("Error: Upload failed: %v\n", err))
			_ = p.DB.UpdateBackupHistoryStatusWithLog(ctx, historyID, "failed", err.Error(), logBuffer.String())
			return
		}
		if st, err := os.Stat(tarPath); err == nil {
			size = st.Size()
		}
	}

	logBuffer.WriteString(time.Now().Format(time.RFC3339) + " completed\n")
	if err := p.DB.UpdateBackupHistoryCompleted(ctx, historyID, actualVolumeName, remotePath, size, logBuffer.String()); err != nil {
		log.Printf("backup history complete id=%d: %v", historyID, err)
		return
	}
	p.pruneRemoteBackups(ctx, dest, backupType, appID, retention)
}

func (p *Panel) AppBackupHistory(c *fiber.Ctx) error {
	appID := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), appID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "app not found"})
	}

	perPage := c.QueryInt("per_page", 5)
	if perPage < 1 {
		perPage = 5
	}
	if perPage > 50 {
		perPage = 50
	}
	page := c.QueryInt("page", 1)
	if page < 1 {
		page = 1
	}

	total, err := p.DB.CountBackupHistory(c.UserContext(), app.ID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	totalPages := (total + perPage - 1) / perPage
	if totalPages == 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}
	offset := (page - 1) * perPage
	if offset > total {
		offset = total
	}

	pageSlice, err := p.DB.ListBackupHistoryPage(c.UserContext(), app.ID, perPage, offset)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	now := time.Now()
	needRemoteCheck := false
	for _, h := range pageSlice {
		if h.ShouldRecheckRemotePresence(now) {
			needRemoteCheck = true
			break
		}
	}
	if needRemoteCheck {
		rowsCopy := append([]db.BackupHistory(nil), pageSlice...)
		go p.refreshBackupRemotePresence(context.Background(), rowsCopy)
	}

	type histOut struct {
		db.BackupHistory
		RemoteMissing bool `json:"RemoteMissing"`
	}
	out := make([]histOut, 0, len(pageSlice))
	for _, h := range pageSlice {
		row := histOut{BackupHistory: h, RemoteMissing: h.RemoteMissingCode == 1}
		out = append(out, row)
	}

	return c.JSON(fiber.Map{
		"history":              out,
		"page":                 page,
		"per_page":             perPage,
		"total":                total,
		"total_pages":          totalPages,
		"remote_check_pending": needRemoteCheck,
	})
}

func (p *Panel) refreshBackupRemotePresence(ctx context.Context, rows []db.BackupHistory) {
	now := time.Now()
	destCache := map[int64]db.BackupDestination{}
	for _, h := range rows {
		if !h.ShouldRecheckRemotePresence(now) {
			continue
		}
		d, ok := destCache[h.DestinationID]
		if !ok {
			var err error
			d, err = p.DB.GetBackupDestination(ctx, h.DestinationID)
			if err != nil {
				continue
			}
			destCache[h.DestinationID] = d
		}
		var cm map[string]string
		if json.Unmarshal([]byte(d.Config), &cm) != nil {
			continue
		}
		exists := rclone.RemoteObjectExists(ctx, d.Provider, cm, h.RemotePath)
		if err := p.DB.UpdateBackupHistoryRemoteCheck(ctx, h.ID, !exists); err != nil {
			log.Printf("backup remote check id=%d: %v", h.ID, err)
		}
	}
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
	if history.AppID != app.ID {
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
	if err := validateBackupDestinationConfig(dest.Provider, configMap); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	if !p.startBackupRestore(app.ID, history.ID) {
		return c.Status(409).JSON(fiber.Map{"error": "another restore is already running"})
	}
	go func() {
		errMsg := p.runRestoreJob(context.Background(), app, dest, history, configMap)
		p.finishBackupRestore(app.ID, history.ID, errMsg)
	}()

	return c.JSON(fiber.Map{"message": "restore started"})
}

func (p *Panel) AppBackupRestoreStatus(c *fiber.Ctx) error {
	appID := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), appID); err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "app not found"})
	}
	st := p.backupRestoreSnapshot(appID)
	return c.JSON(fiber.Map{
		"restoring_history_id": st.ActiveHistoryID,
		"history_id":           st.LastHistoryID,
		"status":               st.Status,
		"error":                st.Error,
	})
}

func (p *Panel) runRestoreJob(ctx context.Context, app db.App, dest db.BackupDestination, history db.BackupHistory, configMap map[string]string) string {
	var localPath string
	var err error

	switch dest.Provider {
	case "gdrive":
		localPath, err = rclone.DownloadFromGoogleDrive(ctx, configMap["client_id"], configMap["client_secret"],
			configMap["token"], configMap["folder_id"], history.RemotePath)
	case "r2":
		localPath, err = rclone.DownloadFromCloudflareR2(ctx, configMap["account_id"], configMap["access_key_id"],
			configMap["secret_access_key"], configMap["bucket"], history.RemotePath)
	default:
		return "unsupported destination provider"
	}

	if err != nil {
		log.Printf("restore: download failed: %v", err)
		return err.Error()
	}
	// Remove entire rclone job dir (file lives under .../rclone-temp/dl-*/).
	defer func() {
		if localPath == "" {
			return
		}
		_ = os.RemoveAll(filepath.Dir(localPath))
	}()

	switch history.BackupType {
	case "volume":
		preferred := backupVolumeNameFromHistory(history)
		volumeName, vmsg := p.resolveRequestedBackupVolume(ctx, app, preferred)
		if vmsg != "" {
			log.Printf("restore: volume resolve: %s", vmsg)
			return vmsg
		}
		if err := backup.RestoreVolume(ctx, volumeName, localPath, true); err != nil {
			log.Printf("restore: volume: %v", err)
			return err.Error()
		}
	case "full":
		fullComposePath := p.composeFilePath(ctx, app, app.ID)
		workspaceRoot := p.composeWorkspaceRoot(ctx, app.ID)
		if err := backup.RestoreFullApp(ctx, app.Name, fullComposePath, workspaceRoot, localPath); err != nil {
			log.Printf("restore: full app: %v", err)
			return err.Error()
		}
	default:
		return "unsupported backup type"
	}
	return ""
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
	volumeName, err := parseRequestedVolumeName(c.FormValue("volume_names"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	if backupType != "volume" {
		volumeName = ""
	}
	cronExpr := strings.TrimSpace(c.FormValue("cron"))
	retention := 7
	if v := strings.TrimSpace(c.FormValue("retention")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			retention = n
		}
	}

	if backupType != "volume" && backupType != "full" {
		return c.Status(400).JSON(fiber.Map{"error": "type must be 'volume' or 'full'"})
	}

	if cronExpr == "" {
		return c.Status(400).JSON(fiber.Map{"error": "cron expression required"})
	}
	if _, err := backupCronParser.Parse(cronExpr); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid cron expression"})
	}

	id, err := p.DB.CreateBackupSchedule(c.UserContext(), app.ID, int64(destID), backupType, volumeName, cronExpr, retention)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"id": id, "message": "schedule created"})
}

func (p *Panel) AppBackupScheduleUpdate(c *fiber.Ctx) error {
	appID := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), appID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "app not found"})
	}

	scheduleID, err := c.ParamsInt("scheduleid")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid schedule id"})
	}

	destIDStr := strings.TrimSpace(c.FormValue("destination"))
	if destIDStr == "" {
		return c.Status(400).JSON(fiber.Map{"error": "destination required"})
	}
	destIDParsed, err := strconv.ParseInt(destIDStr, 10, 64)
	if err != nil || destIDParsed < 1 {
		return c.Status(400).JSON(fiber.Map{"error": "invalid destination"})
	}
	destID := destIDParsed

	backupType := strings.TrimSpace(c.FormValue("type"))
	volumeName, err := parseRequestedVolumeName(c.FormValue("volume_names"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	if backupType != "volume" {
		volumeName = ""
	}
	cronExpr := strings.TrimSpace(c.FormValue("cron"))
	retention := 7
	if v := strings.TrimSpace(c.FormValue("retention")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			retention = n
		}
	}

	if backupType != "volume" && backupType != "full" {
		return c.Status(400).JSON(fiber.Map{"error": "type must be 'volume' or 'full'"})
	}
	if cronExpr == "" {
		return c.Status(400).JSON(fiber.Map{"error": "cron expression required"})
	}
	if _, err := backupCronParser.Parse(cronExpr); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid cron expression"})
	}

	if _, err := p.DB.GetBackupDestination(c.UserContext(), destID); err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "destination not found"})
	}

	if err := p.DB.UpdateBackupSchedule(c.UserContext(), int64(scheduleID), app.ID, destID, backupType, volumeName, cronExpr, retention); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c.Status(404).JSON(fiber.Map{"error": "schedule not found"})
		}
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"message": "schedule updated"})
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

	type scheduleOut struct {
		db.BackupSchedule
		NextRunAt string `json:"NextRunAt"`
	}
	out := make([]scheduleOut, 0, len(schedules))
	for _, s := range schedules {
		so := scheduleOut{BackupSchedule: s}
		if t, ok := nextBackupScheduleRun(s); ok {
			so.NextRunAt = t.In(time.Local).Format(time.RFC3339)
		}
		out = append(out, so)
	}

	return c.JSON(fiber.Map{"schedules": out})
}

func (p *Panel) AppBackupScheduleToggle(c *fiber.Ctx) error {
	appID := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), appID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "app not found"})
	}
	scheduleID, err := c.ParamsInt("scheduleid")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid schedule id"})
	}

	enabled := c.QueryBool("enabled", true)
	if err := p.DB.UpdateBackupScheduleEnabledForApp(c.UserContext(), int64(scheduleID), app.ID, enabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c.Status(404).JSON(fiber.Map{"error": "schedule not found"})
		}
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"message": "schedule updated"})
}

func (p *Panel) AppBackupScheduleDelete(c *fiber.Ctx) error {
	appID := c.Params("id")
	app, err := p.DB.GetApp(c.UserContext(), appID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "app not found"})
	}

	scheduleID, err := c.ParamsInt("scheduleid")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid schedule id"})
	}

	if err := p.DB.DeleteBackupSchedule(c.UserContext(), app.ID, int64(scheduleID)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c.Status(404).JSON(fiber.Map{"error": "schedule not found"})
		}
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"message": "schedule deleted"})
}

func (p *Panel) AppBackupHistoryLog(c *fiber.Ctx) error {
	appID := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), appID); err != nil {
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
	if history.AppID != appID {
		return c.Status(404).JSON(fiber.Map{"error": "backup not found"})
	}

	logContent := history.Log
	if logContent == "" {
		if history.Status == "failed" && history.ErrorMessage != "" {
			logContent = "Error: " + history.ErrorMessage
		} else {
			logContent = "No log available"
		}
	}

	return c.JSON(fiber.Map{
		"log":    logContent,
		"status": history.Status,
	})
}

func (p *Panel) AppBackupDriveLink(c *fiber.Ctx) error {
	appID := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), appID); err != nil {
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
	if history.AppID != appID {
		return c.Status(404).JSON(fiber.Map{"error": "backup not found"})
	}

	if history.Status != "completed" || history.RemotePath == "" {
		return c.Status(400).JSON(fiber.Map{"error": "backup not completed or no remote path"})
	}

	dest, err := p.DB.GetBackupDestination(c.UserContext(), history.DestinationID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "destination not found"})
	}

	if dest.Provider != "gdrive" {
		return c.Status(400).JSON(fiber.Map{"error": "only Google Drive links supported"})
	}

	var configMap map[string]string
	if err := json.Unmarshal([]byte(dest.Config), &configMap); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "invalid config"})
	}

	link, err := rclone.SearchGoogleDriveFile(c.UserContext(), configMap["token"], configMap["folder_id"], history.RemotePath)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"link": link})
}
