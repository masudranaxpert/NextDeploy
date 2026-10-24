package phphosting

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"panel/internal/db"
	"panel/internal/runutil"
)

// Manager owns every Docker-facing operation of the PHP hosting module.
type Manager struct {
	DB             *db.Store
	WorkspacesRoot string // workspaces root as seen by the panel process

	// CgroupParent returns the cgroup parent for an owner's containers ("" = none).
	CgroupParent func(ctx context.Context, owner db.User) string

	IngressNetwork string // network Caddy watches for labelled containers
	DBNetwork      string // private network shared by PHP containers and MariaDB

	locks     sync.Map // runtimeKey -> *sync.Mutex
	fixTimers sync.Map // appID -> *time.Timer
	builds    buildRegistry
	pma       pmaRegistry

	instMu   sync.Mutex
	instAt   time.Time
	instVals []string

	hostMu    sync.Mutex
	hostMount []mountInfo
	hostReady bool

	provMu sync.Mutex
	prov   map[string]*Provision

	cronMu      sync.Mutex
	cronRunning map[int64]bool
}

func NewManager(store *db.Store, workspacesRoot string) *Manager {
	m := &Manager{
		DB:             store,
		WorkspacesRoot: workspacesRoot,
		IngressNetwork: envOr("PHP_INGRESS_NETWORK", DefaultIngress),
		DBNetwork:      envOr("PHP_DB_NETWORK", DefaultDBNet),
		prov:           map[string]*Provision{},
		cronRunning:    map[int64]bool{},
	}
	m.builds.init()
	return m
}

func envOr(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func (m *Manager) lock(key string) func() {
	v, _ := m.locks.LoadOrStore(key, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func docker(ctx context.Context, args ...string) runutil.Result {
	return runutil.Run(ctx, "", nil, append([]string{"docker"}, args...)...)
}

// ---- paths ----

// SitePath is the site workspace as seen by the panel process.
func (m *Manager) SitePath(appID string) string { return filepath.Join(m.WorkspacesRoot, appID) }

func (m *Manager) HtdocsPath(appID string) string { return filepath.Join(m.SitePath(appID), "htdocs") }

func (m *Manager) runtimeDir(ownerID int64, version string) string {
	return filepath.Join(m.WorkspacesRoot, RuntimeDirName, runtimeDirName(ownerID, version))
}

// ---- host path translation ----
//
// docker run -v needs paths as the Docker daemon sees them. When the panel runs in a
// container its own /data is a bind mount from the host, so we resolve the real source
// by inspecting our own container. PHP_HOST_WORKSPACES_ROOT overrides the detection.

type mountInfo struct {
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
}

func (m *Manager) HostPath(ctx context.Context, panelPath string) string {
	if root := strings.TrimSpace(os.Getenv("PHP_HOST_WORKSPACES_ROOT")); root != "" {
		if rel, err := filepath.Rel(m.WorkspacesRoot, panelPath); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.Join(root, rel)
		}
	}
	m.hostMu.Lock()
	if !m.hostReady {
		m.hostMount = loadSelfMounts(ctx)
		m.hostReady = true
	}
	mounts := m.hostMount
	m.hostMu.Unlock()
	return translateHostPath(mounts, panelPath)
}

func translateHostPath(mounts []mountInfo, p string) string {
	best := -1
	for i, mt := range mounts {
		d := filepath.Clean(mt.Destination)
		if p == d || strings.HasPrefix(p, d+string(filepath.Separator)) {
			if best < 0 || len(d) > len(filepath.Clean(mounts[best].Destination)) {
				best = i
			}
		}
	}
	if best < 0 {
		return p
	}
	rel := strings.TrimPrefix(p, filepath.Clean(mounts[best].Destination))
	return filepath.Clean(mounts[best].Source + rel)
}

func loadSelfMounts(ctx context.Context) []mountInfo {
	if _, err := os.Stat("/.dockerenv"); err != nil {
		return nil // panel runs natively on the host: paths are already host paths
	}
	host, _ := os.Hostname()
	for _, ref := range []string{host, "panel"} {
		if ref == "" {
			continue
		}
		res := docker(ctx, "inspect", "--format", "{{json .Mounts}}", ref)
		if !res.OK {
			continue
		}
		var mounts []mountInfo
		if err := json.Unmarshal([]byte(strings.TrimSpace(res.Output)), &mounts); err == nil && len(mounts) > 0 {
			return mounts
		}
	}
	return nil
}

// ---- docker helpers ----

type containerInfo struct {
	Exists  bool
	Running bool
	Labels  map[string]string
	ImageID string
}

func inspectContainer(ctx context.Context, name string) containerInfo {
	res := docker(ctx, "inspect", "--format", "{{.State.Running}}|{{.Image}}|{{json .Config.Labels}}", name)
	if !res.OK {
		return containerInfo{}
	}
	parts := strings.SplitN(strings.TrimSpace(res.Output), "|", 3)
	if len(parts) != 3 {
		return containerInfo{}
	}
	ci := containerInfo{Exists: true, Running: parts[0] == "true", ImageID: parts[1], Labels: map[string]string{}}
	_ = json.Unmarshal([]byte(parts[2]), &ci.Labels)
	return ci
}

func removeContainer(ctx context.Context, name string) {
	_ = docker(ctx, "rm", "-f", name)
}

func imageID(ctx context.Context, image string) string {
	res := docker(ctx, "image", "inspect", "--format", "{{.Id}}", image)
	if !res.OK {
		return ""
	}
	return strings.TrimSpace(res.Output)
}

func (m *Manager) ImageInstalled(ctx context.Context, version string) bool {
	return imageID(ctx, ImageName(version)) != ""
}

func (m *Manager) ensureNetwork(ctx context.Context, name string) error {
	if docker(ctx, "network", "inspect", name).OK {
		return nil
	}
	res := docker(ctx, "network", "create", name)
	if !res.OK && !strings.Contains(res.Output, "already exists") {
		return fmt.Errorf("create network %s: %s", name, res.Output)
	}
	return nil
}

// ContainerState is a compact view used by the Instance page.
type ContainerState struct {
	Name    string `json:"name"`
	Image   string `json:"image"`
	State   string `json:"state"`
	Status  string `json:"status"`
	Kind    string `json:"kind"` // mysql | php | route
	Version string `json:"version,omitempty"`
	OwnerID string `json:"owner_id,omitempty"`
}

// ListContainers returns every container the module manages.
func (m *Manager) ListContainers(ctx context.Context) []ContainerState {
	res := docker(ctx, "ps", "-a", "--filter", "label=nd.php", "--format",
		`{{.Names}}|{{.Image}}|{{.State}}|{{.Status}}|{{.Label "nd.php"}}|{{.Label "nd.php.version"}}|{{.Label "nd.php.owner"}}`)
	out := []ContainerState{}
	if !res.OK {
		return out
	}
	for _, line := range strings.Split(strings.TrimSpace(res.Output), "\n") {
		f := strings.Split(line, "|")
		if len(f) < 7 || f[0] == "" {
			continue
		}
		out = append(out, ContainerState{Name: f[0], Image: f[1], State: f[2], Status: f[3], Kind: f[4], Version: f[5], OwnerID: f[6]})
	}
	return out
}

// RestartContainer restarts one managed container (by exact name).
func (m *Manager) RestartContainer(ctx context.Context, name string) error {
	found := false
	for _, c := range m.ListContainers(ctx) {
		if c.Name == name {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("unknown container %q", name)
	}
	if res := docker(ctx, "restart", name); !res.OK {
		return fmt.Errorf("%s", res.Output)
	}
	return nil
}

// ---- site exec ----

// ExecArgs builds a `docker exec` command line that runs as the site's own user.
func (m *Manager) ExecArgs(site db.PHPSite, interactive bool, cmd ...string) []string {
	args := []string{"exec"}
	if interactive {
		args = append(args, "-it")
	}
	args = append(args,
		"-u", fmt.Sprintf("%d:%d", site.UID, site.UID),
		"-w", DocRoot(site.AppID, site.RootDir))
	for _, e := range SiteEnv(site) {
		args = append(args, "-e", e)
	}
	args = append(args, "-e", "TERM=xterm-256color", RuntimeContainer(site.OwnerID, site.PHPVersion))
	return append(args, cmd...)
}

// SiteEnv is the environment for commands run as the site user. Temporary files and tool
// caches live in the site's own tmp directory (on disk, counted in its storage quota)
// instead of the small shared tmpfs of the container.
func SiteEnv(site db.PHPSite) []string {
	tmp := path.Join(SiteDir(site.AppID), "tmp")
	return []string{
		"HOME=" + tmp,
		"TMPDIR=" + tmp,
		"WP_CLI_CACHE_DIR=" + tmp + "/.wp-cli",
		"COMPOSER_HOME=" + tmp + "/.composer",
	}
}

// RunInSite runs a shell command inside the site's PHP container as the site user.
func (m *Manager) RunInSite(ctx context.Context, site db.PHPSite, shellCmd string) runutil.Result {
	if site.SiteType == SiteTypeStatic {
		return runutil.Result{OK: false, Output: "static sites have no PHP runtime"}
	}
	ci := inspectContainer(ctx, RuntimeContainer(site.OwnerID, site.PHPVersion))
	if !ci.Running {
		return runutil.Result{OK: false, Output: "PHP container is not running"}
	}
	return docker(ctx, m.ExecArgs(site, false, "sh", "-c", shellCmd)...)
}

// ---- site filesystem ----

// PrepareSiteDirs creates htdocs/logs/tmp owned by the site UID.
func (m *Manager) PrepareSiteDirs(site db.PHPSite) error {
	base := m.SitePath(site.AppID)
	// The site's own UID must be able to traverse into the workspace root (it holds
	// root-owned panel metadata, so it stays root-owned and merely gains search permission).
	if err := os.MkdirAll(base, 0750); err != nil {
		return err
	}
	if err := os.Chmod(base, 0711); err != nil {
		return err
	}
	for _, d := range []string{"htdocs", "logs", "tmp"} {
		p := filepath.Join(base, d)
		if err := os.MkdirAll(p, 0750); err != nil {
			return err
		}
		if err := os.Chown(p, site.UID, site.UID); err != nil && !os.IsPermission(err) {
			return err
		}
	}
	return nil
}

// FixOwnership hands every file in the site over to the site UID. Files written through
// the panel (file manager, uploads, archive extraction) are created by the panel user and
// would otherwise be unreadable/unwritable for the FPM pool.
func (m *Manager) FixOwnership(site db.PHPSite) {
	base := m.SitePath(site.AppID)
	for _, d := range []string{"htdocs", "logs", "tmp"} {
		root := filepath.Join(base, d)
		_ = filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if info, err := e.Info(); err == nil {
				if !ownedBy(info, site.UID) {
					_ = os.Lchown(p, site.UID, site.UID)
				}
			}
			return nil
		})
	}
}

// DebounceFixOwnership schedules FixOwnership shortly after the last call.
func (m *Manager) DebounceFixOwnership(appID string) {
	site, err := m.DB.GetPHPSite(context.Background(), appID)
	if err != nil {
		return
	}
	v, loaded := m.fixTimers.LoadOrStore(appID, time.AfterFunc(time.Second, func() {
		m.fixTimers.Delete(appID)
		m.FixOwnership(site)
	}))
	if loaded {
		v.(*time.Timer).Reset(time.Second)
	}
}

// ContainerLogs returns the last n log lines of a managed container.
func (m *Manager) ContainerLogs(ctx context.Context, name string, n int) string {
	res := docker(ctx, "logs", "--tail", fmt.Sprint(n), name)
	return res.Output
}

// RuntimeName is the PHP container that serves a site.
func (m *Manager) RuntimeName(site db.PHPSite) string {
	return RuntimeContainer(site.OwnerID, site.PHPVersion)
}

// DocRootFor is the document root of a site as seen inside the PHP container.
func (m *Manager) DocRootFor(site db.PHPSite) string { return DocRoot(site.AppID, site.RootDir) }

// SiteDirFor is the site workspace as seen inside the PHP container.
func (m *Manager) SiteDirFor(site db.PHPSite) string { return SiteDir(site.AppID) }
