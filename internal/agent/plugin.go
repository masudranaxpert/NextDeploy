package agent

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// The AI assistant is an optional plugin. The panel image does not ship the omp engine
// (~250 MB); enabling AI downloads the release binary for this CPU into
// <DataDir>/plugins/omp after checking it against the release's SHA256SUMS, so it survives
// panel updates. Disabling removes the binary but keeps omp's logins, settings and chat
// history under <DataDir>/omp, so enabling it again needs no new setup.

const (
	// SettingAIEnabled is "1" when the admin turned AI on, "0" when off. Empty means the
	// panel has not decided yet (first start after the update that made AI optional).
	SettingAIEnabled = "ai_enabled"
	// SettingAIPluginVersion overrides the omp release to install (e.g. "v19.1.0" or "latest").
	// Empty means DefaultEngineVersion.
	SettingAIPluginVersion = "ai_plugin_version"

	// DefaultEngineVersion is the omp release this panel version is tested with. It is pinned
	// on purpose: a new omp release can change its RPC protocol, so it is raised together with
	// a panel release instead of following "latest".
	DefaultEngineVersion = "v19.0.0"

	pluginReleaseRepo = "https://github.com/can1357/oh-my-pi/releases"
	// pluginInstallMarker is written by install.sh --with-ai; the panel installs AI on its
	// first start and deletes the marker.
	pluginInstallMarker = "install-ai"
)

// ErrAINotInstalled is returned by every AI action while the plugin is off.
var ErrAINotInstalled = errors.New("AI is not installed. Enable it in Settings → AI or run 'nd ai enable'")

var pluginVersionPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$`)

// PluginStatus is what the Settings page, the CLI and the bootstrap show.
type PluginStatus struct {
	Enabled    bool   `json:"enabled"`    // AI is on and usable
	Installed  bool   `json:"installed"`  // an omp binary is present
	Installing bool   `json:"installing"` // a download is running
	Wanted     bool   `json:"wanted"`     // the admin turned AI on
	Version    string `json:"version,omitempty"`
	Target     string `json:"target"` // version Enable installs
	Source     string `json:"source,omitempty"` // "plugin" (downloaded), "system" (in the image or OMP_BIN)
	Path       string `json:"path,omitempty"`
	SizeMB     int64  `json:"size_mb,omitempty"`
	Arch       string `json:"arch"`
	Error      string `json:"error,omitempty"`
	Progress   int    `json:"progress,omitempty"` // download percent while installing
	DiskMB     int64  `json:"disk_mb"`            // engine plus the tools omp downloaded itself
}

type pluginState struct {
	mu         sync.Mutex
	installing bool
	lastErr    string
	progress   int
}

// pluginHTTP is replaceable in tests.
var pluginHTTP = &http.Client{Timeout: 20 * time.Minute}

// systemOMP finds an engine installed outside the plugin dir (older images shipped one in
// /usr/local/bin). Replaceable in tests.
var systemOMP = findOMPBinary

func (m *Manager) pluginDir() string {
	base := m.dataDir
	if base == "" {
		base = filepath.Join(os.TempDir(), "nextdeploy")
	}
	return filepath.Join(base, "plugins")
}

func (m *Manager) pluginBinary() string  { return filepath.Join(m.pluginDir(), "omp") }
func (m *Manager) pluginVerFile() string { return filepath.Join(m.pluginDir(), "omp.version") }

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// binary is the omp executable to run, or "" when none is present. The downloaded plugin
// wins over one baked into an older image.
func (m *Manager) binary() (string, string) {
	if env := os.Getenv("OMP_BIN"); env != "" && isFile(env) {
		return env, "system"
	}
	if p := m.pluginBinary(); isFile(p) {
		return p, "plugin"
	}
	if p := systemOMP(); p != "omp" && isFile(p) {
		return p, "system"
	}
	return "", ""
}

// aiWanted: the admin turned AI on. Without a settings store (tools and tests that run the
// engine directly) there is nothing to turn off, so it counts as on.
func (m *Manager) aiWanted(ctx context.Context) bool {
	return m.DB == nil || m.DB.GetSetting(ctx, SettingAIEnabled) == "1"
}

// Available reports whether AI is turned on and its engine is present.
func (m *Manager) Available(ctx context.Context) bool {
	if m == nil || !m.aiWanted(ctx) {
		return false
	}
	p, _ := m.binary()
	return p != ""
}

func (m *Manager) ompBin(ctx context.Context) (string, error) {
	if !m.aiWanted(ctx) {
		return "", ErrAINotInstalled
	}
	p, _ := m.binary()
	if p == "" {
		return "", ErrAINotInstalled
	}
	return p, nil
}

func pluginArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "arm64":
		return "arm64"
	default:
		return ""
	}
}

// PluginStatus reports the plugin state.
func (m *Manager) PluginStatus(ctx context.Context) PluginStatus {
	st := PluginStatus{Wanted: m.aiWanted(ctx), Arch: runtime.GOARCH}
	st.Target = m.targetVersion(ctx)
	if p, src := m.binary(); p != "" {
		st.Installed, st.Path, st.Source = true, p, src
		if fi, err := os.Stat(p); err == nil {
			st.SizeMB = (fi.Size() + (1<<20 - 1)) >> 20
		}
		if src == "plugin" {
			if b, err := os.ReadFile(m.pluginVerFile()); err == nil {
				st.Version = strings.TrimSpace(string(b))
			} else {
				st.Version = "manual"
			}
		}
	}
	m.plugin.mu.Lock()
	st.Installing, st.Error, st.Progress = m.plugin.installing, m.plugin.lastErr, m.plugin.progress
	m.plugin.mu.Unlock()
	st.Enabled = st.Wanted && st.Installed && !st.Installing
	var total int64
	if st.Source == "plugin" {
		total += dirSize(m.pluginDir())
	}
	for _, d := range m.engineDownloads() {
		total += dirSize(d)
	}
	st.DiskMB = (total + (1<<20 - 1)) >> 20
	return st
}

// targetVersion is the omp release Enable installs: the admin override or the pinned default.
func (m *Manager) targetVersion(ctx context.Context) string {
	if m.DB != nil {
		if v := normalizeVersion(m.DB.GetSetting(ctx, SettingAIPluginVersion)); v != "" {
			return v
		}
	}
	return DefaultEngineVersion
}

// engineDownloads are the directories omp fills by itself on first use under its home:
// the Antigravity CLI binaries (~230 MB), native modules (~135 MB), logs, daemon state and
// caches. omp downloads them again when needed, so Disable removes them. Logins
// (antigravity-cli/<provider>) and the agent dir (chats, settings) are kept.
func (m *Manager) engineDownloads() []string {
	if m.dataDir == "" {
		return nil
	}
	root := filepath.Join(m.HomeDir(), ".omp")
	return []string{
		filepath.Join(root, "antigravity-cli", "bin"),
		filepath.Join(root, "natives"),
		filepath.Join(root, "logs"),
		filepath.Join(root, "run"),
		filepath.Join(root, "cache"),
	}
}

func dirSize(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, e := d.Info(); e == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	return n
}

// EnableAI turns AI on and downloads the engine in the background when it is missing or
// not the target version. version "" keeps the current target (default DefaultEngineVersion).
func (m *Manager) EnableAI(ctx context.Context, version string) error {
	version = strings.TrimSpace(version)
	if version != "" && version != "latest" && !pluginVersionPattern.MatchString(version) {
		return fmt.Errorf("invalid version %q (use e.g. %s, or latest)", version, DefaultEngineVersion)
	}
	if pluginArch() == "" {
		return fmt.Errorf("AI is not available for %s CPUs", runtime.GOARCH)
	}
	if m.DB == nil {
		return errors.New("no database")
	}
	if version != "" {
		val := normalizeVersion(version)
		if val == DefaultEngineVersion {
			val = "" // follow the pinned default in future panel releases
		}
		if err := m.DB.SetSetting(ctx, SettingAIPluginVersion, val); err != nil {
			return err
		}
	}
	if err := m.DB.SetSetting(ctx, SettingAIEnabled, "1"); err != nil {
		return err
	}
	explicit := false
	if version != "" {
		// A version asked for by name replaces even a hand-placed engine.
		_, src := m.binary()
		cur, _ := os.ReadFile(m.pluginVerFile())
		explicit = src == "plugin" && (version == "latest" || strings.TrimSpace(string(cur)) != m.targetVersion(ctx))
	}
	if explicit || m.needsDownload(ctx) {
		m.startInstall(m.targetVersion(ctx))
	}
	return nil
}

// needsDownload: no engine at all, or the downloaded one is not the target version. An
// engine baked into an older image (source "system") is used as it is.
func (m *Manager) needsDownload(ctx context.Context) bool {
	_, src := m.binary()
	switch src {
	case "":
		return true
	case "plugin":
		want := m.targetVersion(ctx)
		cur, err := os.ReadFile(m.pluginVerFile())
		if err != nil {
			// Placed by hand (offline servers): keep it unless a version is asked for.
			return false
		}
		return want == "latest" || strings.TrimSpace(string(cur)) != want
	default:
		return false
	}
}

// DisableAI turns AI off, stops the engine and deletes everything it downloaded: the omp
// binary and the tools omp fetched itself (Antigravity CLI, native modules, logs, caches).
// Logins, settings and chat history are kept unless purge is set, so enabling again needs no
// setup. purge also deletes those and every stored AI key.
func (m *Manager) DisableAI(ctx context.Context, purge bool) error {
	if m.DB == nil {
		return errors.New("no database")
	}
	m.plugin.mu.Lock()
	busy := m.plugin.installing
	m.plugin.mu.Unlock()
	if busy {
		return errors.New("AI is being installed; wait until the download finishes")
	}
	if m.activePrompt.Load() {
		return errors.New("AI is answering a request; stop it first")
	}
	if err := m.DB.SetSetting(ctx, SettingAIEnabled, "0"); err != nil {
		return err
	}
	m.stopEngine()
	m.killEngineProcesses()
	var errs []string
	remove := func(p string) {
		if err := os.RemoveAll(p); err != nil {
			errs = append(errs, err.Error())
		}
	}
	remove(m.pluginBinary())
	remove(m.pluginVerFile())
	for _, d := range m.engineDownloads() {
		remove(d)
	}
	if purge && m.dataDir != "" {
		remove(filepath.Join(m.dataDir, "omp"))
		if all, err := m.DB.GetAllSettings(ctx); err == nil {
			for k := range all {
				if strings.HasPrefix(k, "ai_") && k != SettingAIEnabled {
					_ = m.DB.DeleteSetting(ctx, k)
				}
			}
		}
	}
	m.plugin.mu.Lock()
	m.plugin.lastErr, m.plugin.progress = "", 0
	m.plugin.mu.Unlock()
	if len(errs) > 0 {
		return fmt.Errorf("AI is off, but some files could not be removed: %s", strings.Join(errs, "; "))
	}
	return nil
}

// killEngineProcesses ends helper processes omp may have left running (its background
// daemons, the Antigravity CLI), so their files can be deleted. Linux only.
func (m *Manager) killEngineProcesses() {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return
	}
	marks := []string{m.pluginBinary()}
	if m.dataDir != "" {
		marks = append(marks, filepath.Join(m.HomeDir(), ".omp")+"/")
	}
	self := os.Getpid()
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 1 || pid == self {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil {
			continue
		}
		cmd := strings.ReplaceAll(string(raw), "\x00", " ")
		for _, mark := range marks {
			if strings.Contains(cmd, mark) {
				if proc, err := os.FindProcess(pid); err == nil {
					_ = proc.Signal(syscall.SIGTERM)
				}
				break
			}
		}
	}
}

// stopEngine ends a running omp process without closing the manager.
func (m *Manager) stopEngine() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client != nil {
		c := m.client
		m.client = nil
		_ = c.Close()
	}
}

func (m *Manager) startInstall(version string) {
	m.plugin.mu.Lock()
	if m.plugin.installing {
		m.plugin.mu.Unlock()
		return
	}
	m.plugin.installing, m.plugin.lastErr, m.plugin.progress = true, "", 0
	m.plugin.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
		defer cancel()
		err := m.installPlugin(ctx, version)
		m.plugin.mu.Lock()
		m.plugin.installing = false
		if err != nil {
			m.plugin.lastErr = err.Error()
			log.Printf("ai plugin: install failed: %v", err)
		} else {
			m.plugin.progress = 100
			log.Printf("ai plugin: installed")
		}
		m.plugin.mu.Unlock()
		if err == nil {
			m.stopEngine() // the next request starts the new binary
			m.pruneEngineDownloads()
		}
	}()
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if v != "" && v != "latest" && !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}

// resolveLatest turns "latest" into the concrete release tag, so the version on disk and
// the checksum file always come from the same release.
func resolveLatest(ctx context.Context, base string) (string, error) {
	client := *pluginHTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/latest", nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("find latest AI engine release: %w", err)
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	tag := path.Base(loc)
	if loc == "" || !pluginVersionPattern.MatchString(tag) {
		return "", fmt.Errorf("find latest AI engine release: unexpected answer %s", resp.Status)
	}
	return tag, nil
}

var pluginBaseURL = pluginReleaseRepo

func (m *Manager) installPlugin(ctx context.Context, version string) error {
	arch := pluginArch()
	if arch == "" {
		return fmt.Errorf("AI is not available for %s CPUs", runtime.GOARCH)
	}
	version = normalizeVersion(version)
	if version == "" || version == "latest" {
		v, err := resolveLatest(ctx, pluginBaseURL)
		if err != nil {
			return err
		}
		version = v
	}
	asset := "omp-linux-musl-" + arch
	base := pluginBaseURL + "/download/" + version

	sums, err := fetchSmall(ctx, base+"/SHA256SUMS.txt")
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	want := checksumFor(sums, asset)
	if want == "" {
		return fmt.Errorf("release %s has no checksum for %s", version, asset)
	}

	dir := m.pluginDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".omp-download-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/"+asset, nil)
	if err != nil {
		tmp.Close()
		return err
	}
	resp, err := pluginHTTP.Do(req)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("download AI engine: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		tmp.Close()
		return fmt.Errorf("download AI engine %s: %s", version, resp.Status)
	}
	h := sha256.New()
	pw := &progressWriter{total: resp.ContentLength, set: m.setProgress}
	if _, err := io.Copy(io.MultiWriter(tmp, h, pw), resp.Body); err != nil {
		tmp.Close()
		return fmt.Errorf("download AI engine: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, want) {
		return fmt.Errorf("AI engine checksum mismatch (got %s, want %s); nothing was installed", got, want)
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	m.stopEngine()
	if err := os.Rename(tmp.Name(), m.pluginBinary()); err != nil {
		return err
	}
	return os.WriteFile(m.pluginVerFile(), []byte(version+"\n"), 0o644)
}

func (m *Manager) setProgress(p int) {
	m.plugin.mu.Lock()
	m.plugin.progress = p
	m.plugin.mu.Unlock()
}

type progressWriter struct {
	total, done int64
	set         func(int)
	last        int
}

func (w *progressWriter) Write(b []byte) (int, error) {
	w.done += int64(len(b))
	if w.total > 0 {
		if p := int(w.done * 99 / w.total); p != w.last {
			w.last = p
			w.set(p)
		}
	}
	return len(b), nil
}

func fetchSmall(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := pluginHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return string(b), err
}

// checksumFor finds the sha256 of asset in a SHA256SUMS file ("<hex>  <name>" lines).
func checksumFor(sums, asset string) string {
	sc := bufio.NewScanner(strings.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == asset && len(f[0]) == 64 {
			return strings.ToLower(f[0])
		}
	}
	return ""
}

// StartupPlugin runs once when the panel starts:
//   - install.sh --with-ai left a marker: turn AI on.
//   - First start after AI became optional: keep AI on for panels that already used it
//     (the engine is in the old image, or omp has saved logins/chats), off otherwise.
//   - AI is on but its binary is gone (for example a new image without omp): download it.
func (m *Manager) StartupPlugin(ctx context.Context) {
	if m == nil || m.DB == nil {
		return
	}
	marker := filepath.Join(m.pluginDir(), pluginInstallMarker)
	if isFile(marker) {
		if err := m.DB.SetSetting(ctx, SettingAIEnabled, "1"); err == nil {
			_ = os.Remove(marker)
		}
	}
	m.pruneEngineDownloads()
	switch m.DB.GetSetting(ctx, SettingAIEnabled) {
	case "":
		used := false
		if p, _ := m.binary(); p != "" {
			used = true
		} else if entries, err := os.ReadDir(m.AgentDir()); err == nil && len(entries) > 0 {
			used = true
		}
		val := "0"
		if used {
			val = "1"
		}
		_ = m.DB.SetSetting(ctx, SettingAIEnabled, val)
		if !used {
			return
		}
	case "0":
		return
	}
	if m.needsDownload(ctx) && pluginArch() != "" {
		log.Printf("ai plugin: AI is on but its engine is missing; downloading it")
		m.startInstall(m.targetVersion(ctx))
	}
}

// QuickState is the cheap part of PluginStatus (no disk walk), for the page bootstrap.
func (m *Manager) QuickState(ctx context.Context) (enabled, installing bool) {
	if m == nil {
		return false, false
	}
	m.plugin.mu.Lock()
	installing = m.plugin.installing
	m.plugin.mu.Unlock()
	return m.Available(ctx) && !installing, installing
}

// pruneEngineDownloads deletes old versions of the tools omp downloads by itself. omp
// updates the Antigravity CLI (~230 MB per version) and keeps one native-module folder per
// omp version, but never deletes the previous ones, so each update leaves the old copy
// behind. Only the version in use is kept.
func (m *Manager) pruneEngineDownloads() {
	if m.dataDir == "" {
		return
	}
	root := filepath.Join(m.HomeDir(), ".omp")

	// Antigravity CLI: install.json names the version omp uses.
	agyDir := filepath.Join(root, "antigravity-cli", "bin")
	if raw, err := os.ReadFile(filepath.Join(agyDir, "install.json")); err == nil {
		var info struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(raw, &info) == nil && info.Version != "" && isDir(filepath.Join(agyDir, info.Version)) {
			removeOtherVersionDirs(agyDir, info.Version)
		}
	}

	// Native modules: one folder per omp version, named without the "v".
	cur := ""
	if b, err := os.ReadFile(m.pluginVerFile()); err == nil {
		cur = strings.TrimPrefix(strings.TrimSpace(string(b)), "v")
	}
	natDir := filepath.Join(root, "natives")
	if cur != "" && isDir(filepath.Join(natDir, cur)) {
		removeOtherVersionDirs(natDir, cur)
	}
}

var versionDirPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$`)

// removeOtherVersionDirs deletes version-named folders in dir except keep. Anything that is
// not a version folder (logins, config files) is left alone.
func removeOtherVersionDirs(dir, keep string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == keep || !versionDirPattern.MatchString(e.Name()) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err == nil {
			log.Printf("ai plugin: removed unused %s", filepath.Join(filepath.Base(dir), e.Name()))
		}
	}
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
