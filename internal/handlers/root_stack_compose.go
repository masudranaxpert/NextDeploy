package handlers

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/caddy"
	"panel/internal/dockerx"
)

//go:embed defaults/root_stack_compose.yml
var embeddedRootStackComposeDefault []byte

const (
	settingRootStackComposeBase   = "root_stack_compose_base"
	settingRootStackComposeMerged = "root_stack_compose"
)

func panelDataDir() string {
	d := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if d == "" {
		return "/data"
	}
	return d
}

func panelHostDataDir() string {
	d := strings.TrimSpace(os.Getenv("PANEL_HOST_DATA_DIR"))
	if d == "" {
		return "/data"
	}
	return d
}

func panelHostInstallDir() string {
	d := strings.TrimSpace(os.Getenv("PANEL_HOST_INSTALL_DIR"))
	if d == "" {
		return "/opt/nextdeploy"
	}
	return d
}

func isRegularComposeFile(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return fi.Mode().IsRegular()
}

// nextDeployComposePath resolves a host-mounted compose file for one-time migration from file-based root stack.
func (p *Panel) nextDeployComposePath() string {
	if custom := strings.TrimSpace(os.Getenv("PANEL_STACK_COMPOSE_FILE")); custom != "" {
		if isRegularComposeFile(custom) {
			return custom
		}
		const legacyStack = "/stack/docker-compose.yml"
		if custom != legacyStack && isRegularComposeFile(legacyStack) {
			return legacyStack
		}
		return custom
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
	return local
}

func (p *Panel) loadRootStackComposeBase(ctx context.Context) ([]byte, error) {
	s := strings.TrimSpace(p.DB.GetSetting(ctx, settingRootStackComposeBase))
	if s != "" {
		return []byte(s), nil
	}
	path := p.nextDeployComposePath()
	if isRegularComposeFile(path) {
		if err := rootStackComposePathMustBeFile(path); err != nil {
			return nil, err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if err := p.DB.SetSetting(ctx, settingRootStackComposeBase, string(b)); err != nil {
			return nil, err
		}
		return b, nil
	}
	if len(embeddedRootStackComposeDefault) == 0 {
		return nil, fmt.Errorf("root stack compose base is empty and embedded default is missing")
	}
	if err := p.DB.SetSetting(ctx, settingRootStackComposeBase, string(embeddedRootStackComposeDefault)); err != nil {
		return nil, err
	}
	return embeddedRootStackComposeDefault, nil
}

func rootStackComposePathMustBeFile(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return fmt.Errorf("%s is a directory, not a file", path)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	return nil
}

// rootStackComposeProjectName is the docker compose -p project name for the root NextDeploy stack.
func rootStackComposeProjectName() string {
	if v := strings.TrimSpace(os.Getenv("PANEL_STACK_COMPOSE_PROJECT")); v != "" {
		return v
	}
	return "nextdeploy"
}

func composeHelperPruneAfterRun() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("PANEL_COMPOSE_HELPER_PRUNE_IMAGE")))
	switch v {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func useDockerComposeHelper() bool {
	_, err := os.Stat("/var/run/docker.sock")
	return err == nil
}

// runRootStackComposeViaHelperContainer runs compose in a one-off container using merged YAML under /data in the helper.
func runRootStackComposeViaHelperContainer(ctx context.Context, hostInstallDir, hostDataDir, projectName, composeFileInContainer string) error {
	hostInstallDir = filepath.Clean(hostInstallDir)
	hostDataDir = filepath.Clean(hostDataDir)
	img := strings.TrimSpace(os.Getenv("PANEL_COMPOSE_HELPER_IMAGE"))
	if img == "" {
		img = "docker:cli"
	}
	name := fmt.Sprintf("nextdeploy-compose-apply-%d", time.Now().UnixNano())
	args := []string{
		"run", "--rm", "--name", name,
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		"-v", hostInstallDir + ":/work",
		"-v", hostDataDir + ":/data",
		"-w", "/work",
		img,
		"compose", "--project-directory", "/work", "-p", projectName,
		"-f", composeFileInContainer,
		"up", "-d", "panel",
	}
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

// applyRootStackPanelMerged runs docker compose up -d panel from applyFile, then removes applyFile (under DATA_DIR).
func (p *Panel) applyRootStackPanelMerged(applyFile string) {
	applyFile = filepath.Clean(applyFile)
	project := rootStackComposeProjectName()
	hostInstall := panelHostInstallDir()
	hostData := panelHostDataDir()
	dd := filepath.Clean(panelDataDir())
	rel, relErr := filepath.Rel(dd, applyFile)
	composeInHelper := applyFile
	if relErr == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		composeInHelper = "/data/" + filepath.ToSlash(rel)
	}
	go func() {
		defer func() { _ = os.Remove(applyFile) }()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if useDockerComposeHelper() {
			if err := runRootStackComposeViaHelperContainer(ctx, hostInstall, hostData, project, composeInHelper); err != nil {
				log.Printf("root stack compose helper: %v", err)
			}
			return
		}
		if res := dockerx.ComposeApplyServices(ctx, filepath.Dir(applyFile), []string{applyFile}, project, nil, nil, "panel"); !res.OK {
			log.Printf("root stack compose apply panel service: %s", strings.TrimSpace(res.Output))
		}
	}()
}

// syncRootStackCompose merges panel domain and proxy settings into the root stack YAML stored in the database.
// When applyPanel is true, writes a temporary compose file under DATA_DIR and applies it in the background, then deletes the file.
func (p *Panel) syncRootStackCompose(ctx context.Context, applyPanel bool) error {
	base, err := p.loadRootStackComposeBase(ctx)
	if err != nil {
		return err
	}
	panelDomain := p.DB.GetSetting(ctx, settingPanelDomain)
	enableWWW := settingBool(p.DB.GetSetting(ctx, settingPanelEnableWWW), false)
	email := p.DB.GetCaddyConfig(ctx, "email")
	caddyImage := p.DB.GetCaddyConfig(ctx, "caddy_image")
	merged, err := caddy.GenerateRootStackCompose(base, panelDomain, enableWWW, email, caddyImage)
	if err != nil {
		return err
	}
	if err := p.DB.SetSetting(ctx, settingRootStackComposeMerged, string(merged)); err != nil {
		return err
	}
	if !applyPanel {
		return nil
	}
	dataDir := panelDataDir()
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return fmt.Errorf("data dir: %w", err)
	}
	tmp, err := os.CreateTemp(dataDir, ".nextdeploy-root-apply-*.yml")
	if err != nil {
		return fmt.Errorf("temp compose: %w", err)
	}
	tmpPath := tmp.Name()
	if _, werr := tmp.Write(merged); werr != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return werr
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, 0640); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	p.applyRootStackPanelMerged(tmpPath)
	return nil
}

// SyncRootStackComposeOnStart refreshes merged root stack in the database (no compose apply).
func (p *Panel) SyncRootStackComposeOnStart() error {
	return p.syncRootStackCompose(context.Background(), false)
}
