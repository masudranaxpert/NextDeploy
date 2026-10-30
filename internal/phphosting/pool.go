package phphosting

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultDisableFunctions lists dangerous command execution and process spawning functions disabled by default.
const DefaultDisableFunctions = "exec,passthru,shell_exec,system,proc_open,popen,show_source,pcntl_exec"

// PoolSpec describes one FPM pool (one per site).
type PoolSpec struct {
	AppID    string
	UID      int
	RootDir  string // relative to htdocs ("" or "public")
	Settings SiteSettings
}

// SiteDir is the workspace directory of a site as seen inside PHP/Caddy containers.
func SiteDir(appID string) string { return path.Join(ContainerWorkspaces, appID) }

// DocRoot is the document root as seen inside PHP/Caddy containers.
func DocRoot(appID, rootDir string) string {
	d := path.Join(SiteDir(appID), "htdocs")
	if r := cleanRootDir(rootDir); r != "" {
		d = path.Join(d, r)
	}
	return d
}

// SocketPath is the FPM socket of a site as seen by Caddy.
func SocketPath(ownerID int64, version, appID string) string {
	return path.Join(ContainerWorkspaces, RuntimeDirName, runtimeDirName(ownerID, version), "run", appID+".sock")
}

func runtimeDirName(ownerID int64, version string) string {
	return fmt.Sprintf("u%d-v%s", ownerID, versionTag(version))
}

// cleanRootDir normalises a user supplied root directory to a safe relative path.
func cleanRootDir(r string) string {
	r = strings.ReplaceAll(strings.TrimSpace(r), "\\", "/")
	r = path.Clean("/" + r)
	r = strings.TrimPrefix(r, "/")
	if r == "." {
		return ""
	}
	return r
}

// ValidateRootDir rejects traversal and weird characters.
func ValidateRootDir(r string) (string, error) {
	r = strings.TrimSpace(r)
	if strings.Contains(r, "..") || !rootDirRe.MatchString(r) {
		return "", fmt.Errorf("invalid root directory: use letters, numbers, dots, dashes, underscores and slashes")
	}
	return cleanRootDir(r), nil
}

var rootDirRe = regexp.MustCompile(`^[A-Za-z0-9._/-]*$`)

// RenderPool renders the php-fpm pool file for a site. The pool runs as the site's own
// UID, is confined with open_basedir and listens on a socket only Caddy can reach.
func RenderPool(sp PoolSpec) (string, error) {
	s := sp.Settings.withDefaults()
	extra, err := ParseAdditional(s.Additional)
	if err != nil {
		return "", err
	}
	site := SiteDir(sp.AppID)
	tmp := site + "/tmp"
	logs := site + "/logs"
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	w("[site-%d]", sp.UID)
	w("user = %d", sp.UID)
	w("group = %d", sp.UID)
	w("listen = /run/php/%s.sock", sp.AppID)
	w("listen.mode = 0660")
	if s.ProcessManager == "ondemand" {
		w("pm = ondemand")
		w("pm.max_children = %d", s.MaxChildren)
		w("pm.process_idle_timeout = 60s")
		w("pm.max_requests = 1000")
	} else {
		// Golden standard: dynamic process management eliminates cold start latency.
		// Keep at least 1-2 workers hot in memory so visits always respond instantly (0ms delay).
		startServers := 2
		minSpare := 1
		maxSpare := 3
		if s.MaxChildren < 3 {
			startServers = 1
			minSpare = 1
			maxSpare = s.MaxChildren
		}
		w("pm = dynamic")
		w("pm.max_children = %d", s.MaxChildren)
		w("pm.start_servers = %d", startServers)
		w("pm.min_spare_servers = %d", minSpare)
		w("pm.max_spare_servers = %d", maxSpare)
		w("pm.max_requests = 1000")
	}
	w("chdir = %s", DocRoot(sp.AppID, sp.RootDir))
	w("catch_workers_output = yes")
	w("decorate_workers_output = no")
	w("clear_env = yes")
	w("env[HOME] = %s", tmp)
	w("php_admin_value[open_basedir] = %s:/usr/local/lib/php:/dev/urandom", site)
	w("php_admin_value[sys_temp_dir] = %s", tmp)
	w("php_admin_value[upload_tmp_dir] = %s", tmp)
	w("php_admin_value[session.save_path] = %s", tmp)
	w("php_admin_flag[opcache.validate_permission] = on")
	w("php_admin_flag[opcache.validate_root] = on")
	if !s.AllowShellFunctions {
		dis := strings.TrimSpace(s.DisableFunctions)
		if dis == "" {
			dis = DefaultDisableFunctions
		}
		w("php_admin_value[disable_functions] = %s", dis)
	} else if s.DisableFunctions != "" {
		w("php_admin_value[disable_functions] = %s", strings.TrimSpace(s.DisableFunctions))
	} else {
		w("php_admin_value[disable_functions] = \"\"")
	}
	w("php_admin_value[error_log] = %s/php-error.log", logs)
	w("php_admin_flag[log_errors] = on")
	w("php_admin_flag[display_errors] = off")
	w("php_admin_value[memory_limit] = %s", s.MemoryLimit)
	w("php_admin_value[max_execution_time] = %d", s.MaxExecutionTime)
	w("php_admin_value[max_input_time] = %d", s.MaxInputTime)
	w("php_admin_value[max_input_vars] = %d", s.MaxInputVars)
	w("php_admin_value[post_max_size] = %s", s.PostMaxSize)
	w("php_admin_value[upload_max_filesize] = %s", s.UploadMaxFilesize)
	for _, d := range extra {
		w("php_value[%s] = %s", d.Key, d.Value)
	}
	return b.String(), nil
}

// WritePools writes the given pool files into dir and removes stale ones.
// It returns true if any pool configuration was added, updated, or removed.
func WritePools(dir string, pools map[string]string) (bool, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false, err
	}
	changed := false
	for name, content := range pools {
		c, err := writeFileAtomic(filepath.Join(dir, name+".conf"), []byte(content), 0644)
		if err != nil {
			return false, err
		}
		if c {
			changed = true
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".conf") {
			continue
		}
		if _, ok := pools[strings.TrimSuffix(n, ".conf")]; !ok {
			_ = os.Remove(filepath.Join(dir, n))
			changed = true
		}
	}
	return changed, nil
}

func writeFileAtomic(p string, data []byte, perm os.FileMode) (bool, error) {
	if existing, err := os.ReadFile(p); err == nil && bytes.Equal(existing, data) {
		return false, nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return false, err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return false, err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		os.Remove(name)
		return false, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return false, err
	}
	if err := os.Rename(name, p); err != nil {
		os.Remove(name)
		return false, err
	}
	return true, nil
}
