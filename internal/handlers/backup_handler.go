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

// parseRequestedVolumeNames parses a comma/newline separated list into
// validated, de-duplicated names. Empty input returns nil (auto-detect).
func parseRequestedVolumeNames(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, ",", "\n")
	parts := strings.Split(raw, "\n")
	names := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !volumex.ValidVolumeName(part) {
			return nil, fmt.Errorf("invalid Docker volume name %q", part)
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		names = append(names, part)
	}
	return names, nil
}

// parseRequestedVolumeName is the single-name variant used by "volume" type;
// it rejects multi-name input.
func parseRequestedVolumeName(raw string) (string, error) {
	names, err := parseRequestedVolumeNames(raw)
	if err != nil {
		return "", err
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

const manualBackupRetention = 0

// validBackupType: "volume" = single Docker volume, "app" = workspace files
// only, "full" = workspace + one or more volumes wrapped in a single .tar.gz.
func validBackupType(t string) bool {
	switch t {
	case "volume", "app", "full":
		return true
	}
	return false
}

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
	if !validBackupType(backupType) {
		return c.Status(400).JSON(fiber.Map{"error": "type must be 'volume', 'app' or 'full'"})
	}
	rawVolumes := c.FormValue("volume_names")
	var volumeField string
	switch backupType {
	case "volume":
		name, err := parseRequestedVolumeName(rawVolumes)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		volumeField = name
	case "full":
		names, err := parseRequestedVolumeNames(rawVolumes)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		if len(names) == 0 {
			return c.Status(400).JSON(fiber.Map{"error": "full app backup needs at least one Docker volume name"})
		}
		volumeField = strings.Join(names, ",")
	case "app":
		volumeField = ""
	}

	dest, err := p.DB.GetBackupDestination(c.UserContext(), int64(destID))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "destination not found"})
	}

	historyID, err := p.DB.CreateBackupHistory(c.UserContext(), app.ID, dest.ID, backupType, "", "running", "", 0)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	go p.runBackupJob(context.Background(), historyID, app.ID, dest, backupType, volumeField, manualBackupRetention)

	return c.JSON(fiber.Map{"message": "backup started", "history_id": historyID})
}

const backupJobMaxDuration = 6 * time.Hour

// runBackupJob runs a backup. retention = max completed rows to keep per
// app+destination+type (0 skips the trim). requestedVolumeField is a single
// name for "volume", empty for "app", comma-separated list for "full".
func (p *Panel) runBackupJob(_ context.Context, historyID int64, appID string, dest db.BackupDestination, backupType, requestedVolumeField string, retention int) {
	ctx, cancel := context.WithTimeout(context.Background(), backupJobMaxDuration)
	defer cancel()

	var logMu strings.Builder
	logMu.WriteString(time.Now().Format(time.RFC3339) + " started\n")

	defer func() {
		if r := recover(); r != nil {
			msg := fmt.Sprintf("backup crashed: %v", r)
			log.Printf("[backup] %s (id=%d app=%s type=%s)", msg, historyID, appID, backupType)
			logMu.WriteString("\n" + msg + "\n")
			_ = p.DB.UpdateBackupHistoryStatusWithLog(context.Background(), historyID, "failed", msg, logMu.String())
		}
	}()
	appendLog := func(s string) {
		logMu.WriteString(s)
		if !strings.HasSuffix(s, "\n") {
			logMu.WriteString("\n")
		}
	}

	app, err := p.DB.GetApp(ctx, appID)
	if err != nil {
		appendLog(fmt.Sprintf("Error: Failed to get app: %v", err))
		_ = p.DB.UpdateBackupHistoryStatusWithLog(ctx, historyID, "failed", err.Error(), logMu.String())
		return
	}

	var configMap map[string]string
	if err := json.Unmarshal([]byte(dest.Config), &configMap); err != nil {
		appendLog(fmt.Sprintf("Error: Invalid config: %v", err))
		_ = p.DB.UpdateBackupHistoryStatusWithLog(ctx, historyID, "failed", "invalid config", logMu.String())
		return
	}
	if err := validateBackupDestinationConfig(dest.Provider, configMap); err != nil {
		appendLog(fmt.Sprintf("Error: %v", err))
		_ = p.DB.UpdateBackupHistoryStatusWithLog(ctx, historyID, "failed", err.Error(), logMu.String())
		return
	}

	var (
		remotePath       string
		size             int64
		tarPath          string
		volumeFieldStore string
	)
	defer func() {
		if strings.TrimSpace(tarPath) != "" {
			_ = os.Remove(tarPath)
		}
	}()

	fail := func(e error) {
		appendLog("error: " + e.Error())
		_ = p.DB.UpdateBackupHistoryStatusWithLog(ctx, historyID, "failed", e.Error(), logMu.String())
	}

	uploadProg := func(snap string) {
		s := logMu.String() + "\n── upload ──\n" + snap
		if len(s) > 65536 {
			s = "…\n" + s[len(s)-65536:]
		}
		_ = p.DB.UpdateBackupHistoryStatusWithLog(ctx, historyID, "running", "", s)
	}

	progressLog := func(msg string) {
		appendLog(msg)
		_ = p.DB.UpdateBackupHistoryStatusWithLog(ctx, historyID, "running", "", logMu.String())
	}

	switch backupType {
	case "volume":
		volumeName, vmsg := p.resolveRequestedBackupVolume(ctx, app, requestedVolumeField)
		if vmsg != "" {
			fail(fmt.Errorf("%s", vmsg))
			return
		}
		volumeFieldStore = volumeName
		appendLog("archive " + volumeName)
		tarPath, err = backup.BackupVolume(ctx, volumeName)
		if err != nil {
			fail(err)
			return
		}
		appendLog("tar " + filepath.Base(tarPath))
		remotePath = path.Join(app.Name, "volumes", filepath.Base(tarPath))

	case "app":
		appendLog("app workspace " + app.Name)
		workspaceRoot := p.composeWorkspaceRoot(ctx, appID)
		tarPath, err = backup.BackupFullApp(ctx, app.Name, workspaceRoot)
		if err != nil {
			fail(err)
			return
		}
		appendLog("tar " + filepath.Base(tarPath))
		remotePath = path.Join(app.Name, "app", filepath.Base(tarPath))

	case "full":
		volumeNames, parseErr := parseRequestedVolumeNames(requestedVolumeField)
		if parseErr != nil {
			fail(parseErr)
			return
		}
		if len(volumeNames) == 0 {
			fail(fmt.Errorf("full app backup requires one or more Docker volume names"))
			return
		}
		volumeFieldStore = strings.Join(volumeNames, ",")
		appendLog("full app " + app.Name + " (+" + strconv.Itoa(len(volumeNames)) + " volume(s))")
		workspaceRoot := p.composeWorkspaceRoot(ctx, appID)
		tarPath, err = backup.BackupFullWithVolumes(ctx, app.Name, workspaceRoot, volumeNames, progressLog)
		if err != nil {
			fail(err)
			return
		}
		appendLog("tar " + filepath.Base(tarPath))
		remotePath = path.Join(app.Name, "full", filepath.Base(tarPath))

	default:
		fail(fmt.Errorf("unsupported backup type %q", backupType))
		return
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
	logMu.WriteString(uploadLog)

	if err != nil {
		appendLog(fmt.Sprintf("Error: Upload failed: %v", err))
		_ = p.DB.UpdateBackupHistoryStatusWithLog(ctx, historyID, "failed", err.Error(), logMu.String())
		return
	}
	if st, statErr := os.Stat(tarPath); statErr == nil {
		size = st.Size()
	}

	appendLog(time.Now().Format(time.RFC3339) + " completed")
	if err := p.DB.UpdateBackupHistoryCompleted(ctx, historyID, volumeFieldStore, remotePath, size, logMu.String()); err != nil {
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
		appID := app.ID
		historyIDLocal := history.ID
		defer func() {
			if r := recover(); r != nil {
				msg := fmt.Sprintf("restore crashed: %v", r)
				log.Printf("[restore] %s (app=%s history=%d)", msg, appID, historyIDLocal)
				p.finishBackupRestore(appID, historyIDLocal, msg)
			}
		}()
		jobCtx, cancel := context.WithTimeout(context.Background(), backupJobMaxDuration)
		defer cancel()
		errMsg := p.runRestoreJob(jobCtx, app, dest, history, configMap)
		p.finishBackupRestore(appID, historyIDLocal, errMsg)
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
	case "app":
		fullComposePath := p.composeFilePath(ctx, app, app.ID)
		workspaceRoot := p.composeWorkspaceRoot(ctx, app.ID)
		if err := backup.RestoreFullApp(ctx, app.Name, fullComposePath, workspaceRoot, localPath); err != nil {
			log.Printf("restore: app workspace: %v", err)
			return err.Error()
		}
	case "full":
		fullComposePath := p.composeFilePath(ctx, app, app.ID)
		workspaceRoot := p.composeWorkspaceRoot(ctx, app.ID)
		if err := backup.RestoreFullWithVolumes(ctx, app.Name, fullComposePath, workspaceRoot, localPath, nil); err != nil {
			log.Printf("restore: full app+volumes: %v", err)
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
	if !validBackupType(backupType) {
		return c.Status(400).JSON(fiber.Map{"error": "type must be 'volume', 'app' or 'full'"})
	}
	volumeField, err := normalizeScheduleVolumeField(backupType, c.FormValue("volume_names"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	cronExpr := strings.TrimSpace(c.FormValue("cron"))
	retention := 7
	if v := strings.TrimSpace(c.FormValue("retention")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			retention = n
		}
	}

	if cronExpr == "" {
		return c.Status(400).JSON(fiber.Map{"error": "cron expression required"})
	}
	if _, err := backupCronParser.Parse(cronExpr); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid cron expression"})
	}

	id, err := p.DB.CreateBackupSchedule(c.UserContext(), app.ID, int64(destID), backupType, volumeField, cronExpr, retention)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"id": id, "message": "schedule created"})
}

// normalizeScheduleVolumeField validates the raw textarea input for the given
// backup type and returns the string that should be stored in backup_schedules.volume_names.
//   - "volume": one exact Docker volume name (empty allowed for auto-detect)
//   - "app":    ignored, stored as ""
//   - "full":   one or more comma-separated Docker volume names (required)
func normalizeScheduleVolumeField(backupType, raw string) (string, error) {
	switch backupType {
	case "volume":
		name, err := parseRequestedVolumeName(raw)
		if err != nil {
			return "", err
		}
		return name, nil
	case "full":
		names, err := parseRequestedVolumeNames(raw)
		if err != nil {
			return "", err
		}
		if len(names) == 0 {
			return "", errors.New("full app backup needs at least one Docker volume name")
		}
		return strings.Join(names, ","), nil
	default:
		return "", nil
	}
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
	if !validBackupType(backupType) {
		return c.Status(400).JSON(fiber.Map{"error": "type must be 'volume', 'app' or 'full'"})
	}
	volumeField, err := normalizeScheduleVolumeField(backupType, c.FormValue("volume_names"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	cronExpr := strings.TrimSpace(c.FormValue("cron"))
	retention := 7
	if v := strings.TrimSpace(c.FormValue("retention")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			retention = n
		}
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

	if err := p.DB.UpdateBackupSchedule(c.UserContext(), int64(scheduleID), app.ID, destID, backupType, volumeField, cronExpr, retention); err != nil {
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
