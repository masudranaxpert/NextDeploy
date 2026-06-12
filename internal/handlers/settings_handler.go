package handlers

import (
	"panel/internal/handlers/utils"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/migrate"
	"panel/internal/dockerapi"
	"panel/internal/dockerx"
	"panel/internal/runutil"
	"panel/internal/sandbox"
	"panel/internal/volumex"

	"github.com/gofiber/fiber/v2"
)

// panelTempPatterns are the glob patterns NextDeploy uses for temp files in os.TempDir().
var panelTempPatterns = []string{
	"vol-backup-*.tar.gz",
	"vol-restore-*.tar.gz",
	"vol-restore-*.zip",
	"panel-upload-*.zip",
	".nextdeploy-atomic-*",
}

// Leftover artefacts inside DATA_DIR/backup-staging when a job crashes.
var backupStagingFilePatterns = []string{
	"*-app-*.tar.gz",
	"*-full-*.tar.gz",
	"*.tar.gz",
}

var backupStagingDirPatterns = []string{
	"work-*",
	"full-*",
	"restore-*",
	"export-*",
	"import-*",
}

var migrateStagingFilePatterns = []string{
	"panel-migrate-*.nd-migrate",
}

// orphanAge is the minimum age before a staging entry is considered safe to
// delete; anything newer may still belong to an in-flight job.
const orphanAge = 2 * time.Hour

func backupStagingRoot() string {
	if d := strings.TrimSpace(os.Getenv("DATA_DIR")); d != "" {
		return filepath.Join(d, "backup-staging")
	}
	return ""
}

func migrateStagingRoot() string {
	if d := strings.TrimSpace(os.Getenv("DATA_DIR")); d != "" {
		return filepath.Join(d, "migrate-staging")
	}
	return ""
}

// scanStagingOrphans returns staging files + dirs older than orphanAge.
func scanStagingOrphans() (files []string, dirs []string, totalBytes int64) {
	root := backupStagingRoot()
	if root == "" {
		return
	}
	cutoff := time.Now().Add(-orphanAge)
	for _, pat := range backupStagingFilePatterns {
		matches, _ := filepath.Glob(filepath.Join(root, pat))
		for _, m := range matches {
			st, err := os.Stat(m)
			if err != nil || st.IsDir() {
				continue
			}
			if st.ModTime().After(cutoff) {
				continue
			}
			files = append(files, m)
			totalBytes += st.Size()
		}
	}
	for _, pat := range backupStagingDirPatterns {
		matches, _ := filepath.Glob(filepath.Join(root, pat))
		for _, m := range matches {
			st, err := os.Stat(m)
			if err != nil || !st.IsDir() {
				continue
			}
			if st.ModTime().After(cutoff) {
				continue
			}
			dirs = append(dirs, m)
			totalBytes += dirSize(m)
		}
	}
	return
}

func dirSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total
}

// ScanPanelTempFiles returns count and total size of orphaned NextDeploy temp files.
func ScanPanelTempFiles(protected map[string]struct{}) (count int, totalBytes int64, paths []string) {
	if protected == nil {
		protected = map[string]struct{}{}
	}
	dirs := []string{os.TempDir(), volumex.HostStagingDir()}
	seen := map[string]struct{}{}
	for _, dir := range dirs {
		for _, pattern := range panelTempPatterns {
			matches, err := filepath.Glob(filepath.Join(dir, pattern))
			if err != nil {
				continue
			}
			for _, m := range matches {
				if _, ok := seen[m]; ok {
					continue
				}
				seen[m] = struct{}{}
				st, err := os.Stat(m)
				if err != nil {
					continue
				}
				count++
				totalBytes += st.Size()
				paths = append(paths, m)
			}
		}
	}
	stagingFiles, stagingDirs, stagingBytes := scanStagingOrphans()
	migrateFiles, migrateDirs, migrateBytes := scanMigrateStagingOrphans(protected)
	for _, f := range stagingFiles {
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		count++
		paths = append(paths, f)
	}
	for _, d := range stagingDirs {
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		count++
		paths = append(paths, d)
	}
	totalBytes += stagingBytes
	for _, f := range migrateFiles {
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		count++
		paths = append(paths, f)
	}
	for _, d := range migrateDirs {
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		count++
		paths = append(paths, d)
	}
	totalBytes += migrateBytes
	return
}

func scanMigrateStagingOrphans(protected map[string]struct{}) (files []string, dirs []string, totalBytes int64) {
	root := migrateStagingRoot()
	if root == "" {
		return
	}
	if protected == nil {
		protected = map[string]struct{}{}
	}
	cutoff := time.Now().Add(-orphanAge)
	for _, pat := range backupStagingDirPatterns {
		if !strings.HasPrefix(pat, "export-") && !strings.HasPrefix(pat, "import-") {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(root, pat))
		for _, m := range matches {
			if _, ok := protected[m]; ok {
				continue
			}
			st, err := os.Stat(m)
			if err != nil || !st.IsDir() || st.ModTime().After(cutoff) {
				continue
			}
			dirs = append(dirs, m)
			totalBytes += dirSize(m)
		}
	}
	for _, pat := range migrateStagingFilePatterns {
		matches, _ := filepath.Glob(filepath.Join(root, pat))
		for _, m := range matches {
			if _, ok := protected[m]; ok {
				continue
			}
			st, err := os.Stat(m)
			if err != nil || st.IsDir() || st.ModTime().After(cutoff) {
				continue
			}
			files = append(files, m)
			totalBytes += st.Size()
		}
	}
	return
}

// CleanPanelTempFiles removes all orphaned NextDeploy temp files and returns
// the number removed and the freed bytes.
func CleanPanelTempFiles(protected map[string]struct{}) (removed int, freedBytes int64) {
	_, _, paths := ScanPanelTempFiles(protected)
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		var size int64
		if st.IsDir() {
			size = dirSize(p)
			if err := os.RemoveAll(p); err == nil {
				removed++
				freedBytes += size
			}
			continue
		}
		size = st.Size()
		if err := os.Remove(p); err == nil {
			removed++
			freedBytes += size
		}
	}
	return
}

// formatBytes returns a human-readable byte size string.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

const (
	settingCleanupEnabled         = "cleanup_enabled"
	settingCleanupInterval        = "cleanup_interval"
	settingCleanupLastRun         = "cleanup_last_run"
	settingCleanupLastLog         = "cleanup_last_log"
	settingCleanupIncludeBuildCache = "cleanup_include_build_cache"
	settingPanelDomain     = "panel_domain"
	settingPanelEnableHTTPS = "panel_enable_https"
	settingPanelEnableWWW  = "panel_enable_www"
	settingRootApplyStatus = "root_apply_status"
	settingCaddySharedMountPrefix = "caddy_shared_mount_prefix"
	settingCaddySharedVolumeNames = "caddy_shared_volume_names"
)

type intervalOption struct {
	Value string
	Label string
}

func cleanupIntervalOptions() []intervalOption {
	return []intervalOption{
		{Value: "1h", Label: "Every 1 hour"},
		{Value: "6h", Label: "Every 6 hours"},
		{Value: "12h", Label: "Every 12 hours"},
		{Value: "24h", Label: "Every day"},
		{Value: "168h", Label: "Every week"},
	}
}

func normalizeCleanupInterval(v string) string {
	switch strings.TrimSpace(v) {
	case "1h", "6h", "12h", "24h", "168h":
		return strings.TrimSpace(v)
	default:
		return "1h"
	}
}

func parseCleanupInterval(v string) time.Duration {
	d, err := time.ParseDuration(normalizeCleanupInterval(v))
	if err != nil || d <= 0 {
		return time.Hour
	}
	return d
}

func settingBool(v string, def bool) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// panelStackComposeContainerPath is the bind-mount path inside the panel container (docker-compose.yml).
const panelStackComposeContainerPath = "/stack/docker-compose.yml"

func isRegularComposeFile(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return fi.Mode().IsRegular()
}

func rootStackComposeFileOrError(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return fmt.Errorf("%s is a directory, not a file: on the host, the bind-mount source for this path must be the docker-compose.yml file — if the host has a folder named docker-compose.yml, remove it and put the real YAML file there (Docker sometimes creates an empty directory when the source was missing)", path)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	return nil
}

// composeHelperPruneAfterRun removes the helper CLI image after apply to save disk (next pull on following save).
// Set PANEL_COMPOSE_HELPER_PRUNE_IMAGE=false to keep the image cached (default: true).
func composeHelperPruneAfterRun() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("PANEL_COMPOSE_HELPER_PRUNE_IMAGE")))
	switch v {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func (p *Panel) nextDeployComposePath() string {
	if custom := strings.TrimSpace(os.Getenv("PANEL_STACK_COMPOSE_FILE")); custom != "" {
		if isRegularComposeFile(custom) {
			return custom
		}
		// Legacy/broken installs sometimes set PANEL_STACK_COMPOSE_FILE to /docker-compose.yml or a missing path
		// while the real file is bind-mounted at /stack/docker-compose.yml.
		if custom != panelStackComposeContainerPath {
			if isRegularComposeFile(panelStackComposeContainerPath) {
				return panelStackComposeContainerPath
			}
		}
	}
	// Default bind mount in container: ./docker-compose.yml -> /stack/docker-compose.yml (no PANEL_STACK_COMPOSE_FILE needed).
	if isRegularComposeFile(panelStackComposeContainerPath) {
		return panelStackComposeContainerPath
	}
	wd, err := os.Getwd()
	if err != nil {
		return "docker-compose.yml"
	}
	local := filepath.Join(wd, "docker-compose.yml")
	if isRegularComposeFile(local) {
		return local
	}
	for d := wd; ; {
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
		candidate := filepath.Join(d, "docker-compose.yml")
		if isRegularComposeFile(candidate) {
			return candidate
		}
	}
	return panelStackComposeContainerPath
}

func (p *Panel) syncRootStackCompose(ctx context.Context) error {
	path := p.nextDeployComposePath()
	if err := rootStackComposeFileOrError(path); err != nil {
		return err
	}
	base, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	panelDomain := p.DB.GetSetting(ctx, settingPanelDomain)
	enableHTTPS := settingBool(p.DB.GetSetting(ctx, settingPanelEnableHTTPS), true)
	enableWWW := settingBool(p.DB.GetSetting(ctx, settingPanelEnableWWW), false)
	email := p.DB.GetCaddyConfig(ctx, "email")
	caddyImage := p.DB.GetCaddyConfig(ctx, "caddy_image")
	sharedMounts := p.caddySharedMountsFromSettings(ctx)
	merged, err := caddy.GenerateRootStackCompose(base, panelDomain, enableHTTPS, enableWWW, email, caddyImage, sharedMounts)
	if err != nil {
		return err
	}
	return os.WriteFile(path, merged, 0640)
}

// formValues returns duplicate POST keys (e.g. several checkboxes sharing one name).
// fetch(FormData) sends multipart/form-data; those fields are not in PostArgs — use MultipartForm.
func formValues(c *fiber.Ctx, key string) []string {
	if c.Request() == nil {
		return nil
	}
	ct := strings.ToLower(c.Get("Content-Type"))
	if strings.HasPrefix(ct, "multipart/form-data") {
		form, err := c.MultipartForm()
		if err != nil {
			log.Printf("formValues: multipart parse: %v", err)
		} else if form != nil {
			if v := form.Value[key]; len(v) > 0 {
				return append([]string(nil), v...)
			}
			return nil
		}
	}
	var out []string
	c.Request().PostArgs().VisitAll(func(k, v []byte) {
		if string(k) == key {
			out = append(out, string(v))
		}
	})
	return out
}

func normalizeCaddySharedMountPrefix(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\\", "/"))
	s = strings.TrimRight(s, "/")
	if s == "" {
		return "/mnt/shared"
	}
	if s[0] != '/' {
		s = "/" + s
	}
	return s
}

func parseCaddySharedVolumeNamesJSON(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || s == "[]" {
		return nil
	}
	var names []string
	if err := json.Unmarshal([]byte(s), &names); err != nil {
		return nil
	}
	out := make([]string, 0, len(names))
	seen := map[string]struct{}{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || !volumex.ValidVolumeName(n) {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

func (p *Panel) caddySharedMountsFromSettings(ctx context.Context) []caddy.NDSharedMount {
	prefix := normalizeCaddySharedMountPrefix(p.DB.GetSetting(ctx, settingCaddySharedMountPrefix))
	names := parseCaddySharedVolumeNamesJSON(p.DB.GetSetting(ctx, settingCaddySharedVolumeNames))
	if len(names) == 0 {
		return nil
	}
	out := make([]caddy.NDSharedMount, 0, len(names))
	for _, n := range names {
		target := prefix + "/" + n
		if !caddy.ValidNDSharedTarget(target) {
			continue
		}
		out = append(out, caddy.NDSharedMount{VolumeName: n, Target: target})
	}
	return out
}

// rootStackComposeProjectName is the docker compose -p project name for the root NextDeploy stack.
func rootStackComposeProjectName(projectDir string) string {
	base := filepath.Base(filepath.Clean(projectDir))
	if strings.EqualFold(base, "stack") {
		// Panel mounts host compose at /stack/docker-compose.yml; host dir is usually .../nextdeploy
		return "nextdeploy"
	}
	if base == "." || base == string(filepath.Separator) || base == "" {
		return "nextdeploy"
	}
	return base
}

// useDockerComposeHelper is true when the panel can reach the host Docker socket (avoid running compose from inside the panel container process).
func useDockerComposeHelper() bool {
	_, err := os.Stat("/var/run/docker.sock")
	return err == nil
}

func (p *Panel) setRootApplyStatus(status string) {
	_ = p.DB.SetSetting(context.Background(), settingRootApplyStatus, strings.TrimSpace(status))
}

// rootStackComposeHelperTarget discovers the host compose file path and compose project of
// the running panel stack without requiring extra env vars in docker-compose.yml.
func rootStackComposeHelperTarget(ctx context.Context) (hostComposePath, project string, err error) {
	project, source, err := dockerapi.ContainerComposeProjectAndMountSource(ctx, "panel", panelStackComposeContainerPath)
	legacyCompose := filepath.Join("/opt/nextdeploy", "docker-compose.yml")
	if err != nil {
		log.Printf("root stack compose helper: inspect fallback to %s after error: %v", legacyCompose, err)
		return legacyCompose, rootStackComposeProjectName(filepath.Dir(legacyCompose)), nil
	}
	if strings.TrimSpace(source) == "" {
		return "", "", fmt.Errorf("host compose source is empty")
	}
	cleanSource := filepath.ToSlash(filepath.Clean(source))
	if cleanSource == "/docker-compose.yml" || cleanSource == panelStackComposeContainerPath || strings.HasPrefix(cleanSource, "/work/") {
		log.Printf("root stack compose helper: ignoring non-host compose source %s, using fallback %s", cleanSource, legacyCompose)
		return legacyCompose, rootStackComposeProjectName(filepath.Dir(legacyCompose)), nil
	}
	hostComposePath = filepath.Clean(source)
	if strings.TrimSpace(project) == "" {
		project = rootStackComposeProjectName(filepath.Dir(hostComposePath))
	}
	return hostComposePath, project, nil
}

// runRootStackComposeViaHelperContainer runs compose in a one-off container (docker:cli) so the panel container
// is not the client process that triggers its own recreate. services are passed to `docker compose up -d` (e.g. "panel" or "caddy").
func runRootStackComposeViaHelperContainer(ctx context.Context, hostComposePath, projectName string, services []string) error {
	if len(services) == 0 {
		return fmt.Errorf("compose apply: no services specified")
	}
	hostComposePath = filepath.Clean(hostComposePath)
	hostInstallDir := filepath.Dir(hostComposePath)
	img := strings.TrimSpace(os.Getenv("PANEL_COMPOSE_HELPER_IMAGE"))
	if img == "" {
		img = "docker:cli"
	}
	name := fmt.Sprintf("nextdeploy-compose-apply-%d", time.Now().UnixNano())
	// No -d: wait for the helper to exit so we capture success/failure and compose logs on stderr.
	//
	// IMPORTANT: mount the host project directory at its real host path (not /work).
	// docker compose resolves relative bind-mount sources (e.g. ./docker-compose.yml)
	// using --project-directory.  If that directory is /work inside the helper but
	// /opt/nextdeploy on the host, Docker daemon will try to bind /work/docker-compose.yml
	// as the panel volume source — a host path that does not exist — and Docker silently
	// creates an empty *directory* there.  The next panel restart then mounts that
	// directory instead of the compose file, breaking everything.
	// By mounting hostInstallDir at its own path we guarantee that the resolved absolute
	// source paths match real host filesystem locations.
	args := []string{
		"run", "--rm", "--name", name,
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		"-v", hostInstallDir + ":" + hostInstallDir,
		"-w", hostInstallDir,
		img,
		"compose", "--project-directory", hostInstallDir, "-p", projectName,
		"-f", hostComposePath,
		"up", "-d",
	}
	args = append(args, services...)
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if composeHelperPruneAfterRun() {
		pruneCtx, pruneCancel := context.WithTimeout(context.Background(), 90*time.Second)
		rmiOut, rmiErr := exec.CommandContext(pruneCtx, "docker", "rmi", img).CombinedOutput()
		pruneCancel()
		if rmiErr != nil {
			log.Printf("root stack compose helper: docker rmi %s (optional): %v %s", img, rmiErr, strings.TrimSpace(string(rmiOut)))
		} else {
			log.Printf("root stack compose helper: removed image %s to free disk (set PANEL_COMPOSE_HELPER_PRUNE_IMAGE=false to keep)", img)
		}
	}
	if err != nil {
		return fmt.Errorf("%w: %s", err, text)
	}
	if text != "" {
		log.Printf("root stack compose helper: %s", text)
	}
	return nil
}

// applyRootStackComposeBackground runs `docker compose up -d` for the given services only.
// Panel domain saves use ["panel"] (new Caddy labels on the panel service); shared volume saves use ["caddy"] (mounts on the proxy).
func (p *Panel) applyRootStackComposeBackground(composeFile string, services ...string) {
	if len(services) == 0 {
		return
	}
	composeFile = filepath.Clean(composeFile)
	projectDir := filepath.Dir(composeFile)
	project := rootStackComposeProjectName(projectDir)
	spec := strings.Join(services, " ")
	p.setRootApplyStatus("Queued: docker compose up -d " + spec + ".")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if useDockerComposeHelper() {
			hostComposePath, detectedProject, err := rootStackComposeHelperTarget(ctx)
			if err != nil {
				p.setRootApplyStatus("Compose apply failed while resolving host compose target: " + err.Error())
				log.Printf("root stack compose helper target: %v", err)
				return
			}
			project = detectedProject
			if err := runRootStackComposeViaHelperContainer(ctx, hostComposePath, project, services); err != nil {
				p.setRootApplyStatus("Compose apply failed: " + err.Error())
				log.Printf("root stack compose helper: %v", err)
			} else {
				p.setRootApplyStatus("Apply completed (docker compose up -d " + spec + ").")
			}
			return
		}
		if res := dockerx.ComposeApplyServices(ctx, projectDir, []string{composeFile}, project, nil, nil, services...); !res.OK {
			p.setRootApplyStatus("Compose apply failed: " + strings.TrimSpace(res.Output))
			log.Printf("root stack compose apply %s: %s", spec, strings.TrimSpace(res.Output))
		} else {
			p.setRootApplyStatus("Apply completed (docker compose up -d " + spec + ").")
		}
	}()
}

func (p *Panel) SyncRootStackComposeOnStart() error {
	return p.syncRootStackCompose(context.Background())
}

func (p *Panel) SettingsPage(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok || u.Role != db.RoleAdmin {
		return c.Status(fiber.StatusForbidden).SendString("forbidden")
	}
	ctx := c.UserContext()
	cfg, _ := p.DB.GetAllSettings(ctx)
	tmpCount, tmpBytes, _ := ScanPanelTempFiles(migrate.ProtectedStagingPaths(p.DB))

	return c.Render("pages/settings", WithUser(c, fiber.Map{
		"Nav":                        "settings",
		"Title":                      "Settings",
		"Flash":                      utils.ReadFlash(c),
		"CleanupEnabled":             settingBool(cfg[settingCleanupEnabled], true),
		"CleanupInterval":            normalizeCleanupInterval(cfg[settingCleanupInterval]),
		"CleanupIntervals":           cleanupIntervalOptions(),
		"CleanupLastRun":             cfg[settingCleanupLastRun],
		"CleanupLastLog":             cfg[settingCleanupLastLog],
		"CleanupIncludeBuildCache":   settingBool(cfg[settingCleanupIncludeBuildCache], false),
		"TmpFileCount":               tmpCount,
		"TmpFileSize":                formatBytes(tmpBytes),
		"TmpFileSizeRaw":             tmpBytes,
	}), "layouts/shell")
}

// TempCleanupRun deletes orphaned NextDeploy temp files immediately.
func (p *Panel) TempCleanupRun(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok || u.Role != db.RoleAdmin {
		return c.Status(fiber.StatusForbidden).SendString("forbidden")
	}
	removed, freed := CleanPanelTempFiles(migrate.ProtectedStagingPaths(p.DB))
	msg := fmt.Sprintf("Removed %d temp file(s), freed %s.", removed, formatBytes(freed))
	_ = p.DB.SetSetting(c.UserContext(), "tmp_cleanup_last_run", time.Now().UTC().Format(time.RFC3339))
	_ = p.DB.SetSetting(c.UserContext(), "tmp_cleanup_last_log", msg)
	utils.SetFlash(c, "tmp_cleaned")
	return c.Redirect("/settings")
}

// TempCleanupInfo returns live temp file info as JSON (for AJAX refresh).
func (p *Panel) TempCleanupInfo(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok || u.Role != db.RoleAdmin {
		return c.Status(fiber.StatusForbidden).SendString("forbidden")
	}
	count, totalBytes, _ := ScanPanelTempFiles(migrate.ProtectedStagingPaths(p.DB))
	return c.JSON(fiber.Map{
		"count": count,
		"size":  formatBytes(totalBytes),
		"bytes": totalBytes,
	})
}

func (p *Panel) SettingsSave(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok || u.Role != db.RoleAdmin {
		return c.Status(fiber.StatusForbidden).SendString("forbidden")
	}
	ctx := c.UserContext()
	enabled := c.FormValue(settingCleanupEnabled) == "on"
	interval := normalizeCleanupInterval(c.FormValue(settingCleanupInterval))
	includeBuildCache := c.FormValue(settingCleanupIncludeBuildCache) == "on"
	if err := p.DB.SetSetting(ctx, settingCleanupEnabled, boolString(enabled)); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.DB.SetSetting(ctx, settingCleanupInterval, interval); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if err := p.DB.SetSetting(ctx, settingCleanupIncludeBuildCache, boolString(includeBuildCache)); err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if p.DB.GetSetting(ctx, settingCleanupLastRun) == "" {
		_ = p.DB.SetSetting(ctx, settingCleanupLastRun, time.Now().UTC().Format(time.RFC3339))
	}
	return c.Redirect("/settings")
}

func (p *Panel) SaveNextDeployPanelConfig(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok || u.Role != db.RoleAdmin {
		return c.Status(fiber.StatusForbidden).SendString("forbidden")
	}
	ctx := c.UserContext()
	wantsJSON := c.Get("Accept") == "application/json" || c.Get("X-Requested-With") == "XMLHttpRequest"

	jsonErr := func(msg string) error {
		return c.Status(500).JSON(fiber.Map{"ok": false, "error": msg})
	}

	panelDomain := strings.TrimSpace(c.FormValue(settingPanelDomain))
	enableHTTPS := c.FormValue(settingPanelEnableHTTPS) == "on"
	enableWWW := c.FormValue(settingPanelEnableWWW) == "on"

	if err := p.DB.SetSetting(ctx, settingPanelDomain, panelDomain); err != nil {
		if wantsJSON {
			return jsonErr(err.Error())
		}
		return c.Status(500).SendString(err.Error())
	}
	if err := p.DB.SetSetting(ctx, settingPanelEnableHTTPS, boolString(enableHTTPS)); err != nil {
		if wantsJSON {
			return jsonErr(err.Error())
		}
		return c.Status(500).SendString(err.Error())
	}
	if err := p.DB.SetSetting(ctx, settingPanelEnableWWW, boolString(enableWWW)); err != nil {
		if wantsJSON {
			return jsonErr(err.Error())
		}
		return c.Status(500).SendString(err.Error())
	}

	if err := p.syncRootStackCompose(ctx); err != nil {
		log.Printf("root stack compose sync on panel config save: %v", err)
		if wantsJSON {
			return c.JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
		utils.SetFlash(c, "panelSaved")
		return c.Redirect("/nextdeploy")
	}

	p.applyRootStackComposeBackground(p.nextDeployComposePath(), "panel")
	if wantsJSON {
		return c.JSON(fiber.Map{"ok": true, "queued": true})
	}
	utils.SetFlash(c, "panelSaved")
	return c.Redirect("/nextdeploy")
}

// SaveNextDeploySharedVolumes persists Caddy shared volume mounts (prefix + selected Docker volume names)
// and syncs the root compose file. Separate from panel domain so each form only updates its own settings.
func (p *Panel) SaveNextDeploySharedVolumes(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok || u.Role != db.RoleAdmin {
		return c.Status(fiber.StatusForbidden).SendString("forbidden")
	}
	ctx := c.UserContext()
	wantsJSON := c.Get("Accept") == "application/json" || c.Get("X-Requested-With") == "XMLHttpRequest"

	jsonErr := func(msg string) error {
		return c.Status(500).JSON(fiber.Map{"ok": false, "error": msg})
	}

	prefix := normalizeCaddySharedMountPrefix(c.FormValue("caddy_shared_mount_prefix"))
	var sharedNames []string
	seenVol := map[string]struct{}{}
	for _, n := range formValues(c, "caddy_shared_volumes") {
		n = strings.TrimSpace(n)
		if n == "" || !volumex.ValidVolumeName(n) {
			continue
		}
		if _, ok := seenVol[n]; ok {
			continue
		}
		seenVol[n] = struct{}{}
		sharedNames = append(sharedNames, n)
	}
	sort.Strings(sharedNames)
	sharedJSON, jerr := json.Marshal(sharedNames)
	if jerr != nil {
		if wantsJSON {
			return jsonErr(jerr.Error())
		}
		return c.Status(500).SendString(jerr.Error())
	}

	if err := p.DB.SetSetting(ctx, settingCaddySharedMountPrefix, prefix); err != nil {
		if wantsJSON {
			return jsonErr(err.Error())
		}
		return c.Status(500).SendString(err.Error())
	}
	if err := p.DB.SetSetting(ctx, settingCaddySharedVolumeNames, string(sharedJSON)); err != nil {
		if wantsJSON {
			return jsonErr(err.Error())
		}
		return c.Status(500).SendString(err.Error())
	}

	if err := p.syncRootStackCompose(ctx); err != nil {
		log.Printf("root stack compose sync on shared volumes save: %v", err)
		if wantsJSON {
			return c.JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
		utils.SetFlash(c, "volumesSaved")
		return c.Redirect("/nextdeploy")
	}

	p.applyRootStackComposeBackground(p.nextDeployComposePath(), "caddy")
	if wantsJSON {
		return c.JSON(fiber.Map{"ok": true, "queued": true})
	}
	utils.SetFlash(c, "volumesSaved")
	return c.Redirect("/nextdeploy")
}

// NextDeployApplyStatus returns the current root-stack compose apply status as JSON.
// Used by the frontend to poll for live apply progress without a page reload.
func (p *Panel) NextDeployApplyStatus(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok || u.Role != db.RoleAdmin {
		return c.Status(fiber.StatusForbidden).SendString("forbidden")
	}
	ctx := c.UserContext()
	status := strings.TrimSpace(p.DB.GetSetting(ctx, settingRootApplyStatus))
	done := status == "" ||
		strings.Contains(status, "completed") ||
		strings.Contains(status, "failed")
	succeeded := status == "" || strings.Contains(status, "completed")
	return c.JSON(fiber.Map{
		"status":    status,
		"done":      done,
		"succeeded": succeeded,
	})
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// ManualCleanupRun triggers an immediate Docker cleanup regardless of schedule.
func (p *Panel) ManualCleanupRun(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok || u.Role != db.RoleAdmin {
		return c.Status(fiber.StatusForbidden).SendString("forbidden")
	}
	go p.runScheduledCleanupForce()
	utils.SetFlash(c, "cleanup_started")
	return c.Redirect("/settings")
}

// currentPruneOptions reads the live cleanup settings so scheduled and manual
// runs pick up the newest "Include build cache" toggle without a restart.
func (p *Panel) currentPruneOptions(ctx context.Context) dockerx.PruneOptions {
	opts := dockerx.DefaultPruneOptions()
	opts.BuildCache = settingBool(p.DB.GetSetting(ctx, settingCleanupIncludeBuildCache), false)
	return opts
}

func (p *Panel) runScheduledCleanupForce() {
	ctx := context.Background()
	runCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	res := dockerx.DockerPruneWithOptions(runCtx, p.currentPruneOptions(ctx))
	now := time.Now().UTC()
	_ = p.DB.SetSetting(ctx, settingCleanupLastRun, now.Format(time.RFC3339))
	_ = p.DB.SetSetting(ctx, settingCleanupLastLog, runutil.StatusText(runutil.Result{OK: res.OK, Output: res.Output}))
}

func (p *Panel) StartBackgroundJobs() {
	go p.prePullAlpineImage()
	go p.cleanupLoop()
	go p.sessionPruneLoop()
	go p.cleanOrphanTempFiles()
	go p.orphanStagingSweepLoop()
	go p.migrateSweepLoop()
	go p.auditLogPruneLoop()
	migrate.StartupSweep(p.DB)
}

func (p *Panel) auditLogPruneLoop() {
	_ = p.DB.PruneAuditLogs(context.Background(), 7)
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		<-ticker.C
		_ = p.DB.PruneAuditLogs(context.Background(), 7)
	}
}

func (p *Panel) prePullAlpineImage() {
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "docker", "pull", "alpine:3.20")
	_ = cmd.Run()
}

// cleanOrphanTempFiles removes any leftover NextDeploy temp files from a previous
// run (e.g. after a panel crash mid-download). Runs once at startup.
func (p *Panel) cleanOrphanTempFiles() {
	removed, freed := CleanPanelTempFiles(migrate.ProtectedStagingPaths(p.DB))
	if removed > 0 {
		msg := fmt.Sprintf("[startup] Removed %d orphaned temp file(s), freed %s.", removed, formatBytes(freed))
		_ = p.DB.SetSetting(context.Background(), "tmp_cleanup_last_run", time.Now().UTC().Format(time.RFC3339))
		_ = p.DB.SetSetting(context.Background(), "tmp_cleanup_last_log", msg)
	}
}

// orphanStagingSweepLoop hourly removes staging entries older than orphanAge,
// catching crashes that prevented the in-process `defer` from running.
func (p *Panel) orphanStagingSweepLoop() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[staging-sweep] panic: %v", r)
		}
	}()
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		files, dirs, _ := scanStagingOrphans()
		for _, f := range files {
			_ = os.Remove(f)
		}
		for _, d := range dirs {
			_ = os.RemoveAll(d)
		}
		<-ticker.C
	}
}

func (p *Panel) sessionPruneLoop() {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		<-ticker.C
		_ = p.DB.PruneExpiredSessions(context.Background())
	}
}

func (p *Panel) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		p.runScheduledCleanup()
		<-ticker.C
	}
}

func (p *Panel) runScheduledCleanup() {
	ctx := context.Background()
	cfg, err := p.DB.GetAllSettings(ctx)
	if err != nil {
		return
	}
	if !settingBool(cfg[settingCleanupEnabled], true) {
		return
	}
	interval := parseCleanupInterval(cfg[settingCleanupInterval])
	lastRaw := strings.TrimSpace(cfg[settingCleanupLastRun])
	if lastRaw == "" {
		_ = p.DB.SetSetting(ctx, settingCleanupLastRun, time.Now().UTC().Format(time.RFC3339))
		return
	}
	lastRun, err := time.Parse(time.RFC3339, lastRaw)
	if err != nil {
		_ = p.DB.SetSetting(ctx, settingCleanupLastRun, time.Now().UTC().Format(time.RFC3339))
		return
	}
	if time.Since(lastRun) < interval {
		return
	}
	runCtx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	opts := dockerx.DefaultPruneOptions()
	opts.BuildCache = settingBool(cfg[settingCleanupIncludeBuildCache], false)
	res := dockerx.DockerPruneWithOptions(runCtx, opts)
	now := time.Now().UTC()
	_ = p.DB.SetSetting(ctx, settingCleanupLastRun, now.Format(time.RFC3339))
	_ = p.DB.SetSetting(ctx, settingCleanupLastLog, runutil.StatusText(runutil.Result{OK: res.OK, Output: res.Output}))
}

func nextDeployPanelDomain(cfg map[string]string) db.AppDomain {
	return db.AppDomain{
		Domain:      strings.TrimSpace(cfg[settingPanelDomain]),
		Port:        8080,
		EnableHTTPS: settingBool(cfg[settingPanelEnableHTTPS], true),
		EnableWWW:   settingBool(cfg[settingPanelEnableWWW], false),
	}
}

func (p *Panel) RegistriesPage(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, _ := c.Locals("auth_user").(db.User)
	var registries []db.PrivateRegistry
	var rerr error
	if u.Role == db.RoleAdmin {
		registries, rerr = p.DB.ListPrivateRegistries(ctx, nil)
	} else {
		registries, rerr = p.DB.ListPrivateRegistries(ctx, &u.ID)
	}
	if rerr != nil {
		log.Printf("error listing private registries: %v", rerr)
	}

	return c.Render("pages/registries", WithUser(c, fiber.Map{
		"Nav":        "registries",
		"Title":      "Registries",
		"Flash":      utils.ReadFlash(c),
		"Registries": registries,
	}), "layouts/shell")
}

func (p *Panel) AddRegistry(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := c.Locals("auth_user").(db.User)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}

	name := strings.TrimSpace(c.FormValue("name"))
	serverAddress := strings.TrimSpace(c.FormValue("server_address"))
	username := strings.TrimSpace(c.FormValue("username"))
	password := c.FormValue("password")

	if name == "" || serverAddress == "" || username == "" || password == "" {
		utils.SetFlash(c, "Error: All fields are required.")
		return c.Redirect("/registries")
	}

	encPassword, err := sandbox.Encrypt(password)
	if err != nil {
		utils.SetFlash(c, "Error encrypting password: "+err.Error())
		return c.Redirect("/registries")
	}

	reg := db.PrivateRegistry{
		Name:              name,
		ServerAddress:     serverAddress,
		Username:          username,
		PasswordEncrypted: encPassword,
	}

	isGlobal := c.FormValue("is_global") == "on" && u.Role == db.RoleAdmin
	if !isGlobal {
		reg.UserID = &u.ID
	}

	_, err = p.DB.AddPrivateRegistry(ctx, reg)
	if err != nil {
		utils.SetFlash(c, "Error adding registry: "+err.Error())
		return c.Redirect("/registries")
	}

	p.RecordAuditLog(c, "add_private_registry", "registry", name, fmt.Sprintf("Added registry %s (Global: %v)", name, isGlobal))
	utils.SetFlash(c, "Registry added successfully.")
	return c.Redirect("/registries")
}

func (p *Panel) DeleteRegistry(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := c.Locals("auth_user").(db.User)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}

	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid ID")
	}

	reg, err := p.DB.GetPrivateRegistry(ctx, int64(id))
	if err != nil {
		utils.SetFlash(c, "Registry not found.")
		return c.Redirect("/registries")
	}

	if u.Role != db.RoleAdmin && (reg.UserID == nil || *reg.UserID != u.ID) {
		return c.Status(fiber.StatusForbidden).SendString("Forbidden")
	}

	err = p.DB.DeletePrivateRegistry(ctx, int64(id))
	if err != nil {
		utils.SetFlash(c, "Error deleting registry: "+err.Error())
		return c.Redirect("/registries")
	}

	p.RecordAuditLog(c, "delete_private_registry", "registry", reg.Name, fmt.Sprintf("Deleted registry %s", reg.Name))
	utils.SetFlash(c, "Registry deleted successfully.")
	return c.Redirect("/registries")
}
