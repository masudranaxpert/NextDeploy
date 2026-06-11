package volumex

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"panel/internal/perflog"
	"panel/internal/workspace"
)

var volNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
var uidGidRe = regexp.MustCompile(`^\d+:\d+$`)

// HostStagingDir returns a directory that is bind-mounted on the Docker host
// (via DATA_DIR) so that temp files written here can be passed as -v src:/dst
// to sibling docker run containers. Falls back to os.TempDir() for local dev.
func HostStagingDir() string {
	if d := strings.TrimSpace(os.Getenv("DATA_DIR")); d != "" {
		dir := filepath.Join(d, "restore-staging")
		_ = os.MkdirAll(dir, 0700)
		return dir
	}
	return os.TempDir()
}

func hostStagingDir() string { return HostStagingDir() }

// ValidVolumeName rejects names that could break docker -v syntax or confuse the CLI.
func ValidVolumeName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || !volNameRe.MatchString(name) {
		return false
	}
	// Defense in depth: regex allows '.' but not ':', '/', '\', '%' (volume names must not).
	if strings.ContainsAny(name, ":/\\%") {
		return false
	}
	return true
}

// ParentRel delegates to workspace.ParentRel for consistency.
func ParentRel(rel string) string {
	return workspace.ParentRel(rel)
}

func List(ctx context.Context) ([]string, string) {
	start := time.Now()
	cmd := exec.CommandContext(ctx, "docker", "volume", "ls", "-q")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		perflog.DockerOp("VolumeList", time.Since(start), "cli error")
		return nil, strings.TrimSpace(out.String())
	}
	var names []string
	for _, line := range strings.Split(out.String(), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}
	perflog.DockerOp("VolumeList", time.Since(start), fmt.Sprintf("cli count=%d", len(names)))
	return names, ""
}

func VolumeExists(ctx context.Context, volumeName string) (bool, string) {
	volumeName = strings.TrimSpace(volumeName)
	if !ValidVolumeName(volumeName) {
		return false, "invalid volume name"
	}
	cmd := exec.CommandContext(ctx, "docker", "volume", "inspect", volumeName)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return false, strings.TrimSpace(out.String())
	}
	return true, ""
}

func listByComposeProjectLabel(ctx context.Context, composeProject string) ([]string, string) {
	composeProject = strings.TrimSpace(composeProject)
	if composeProject == "" {
		return nil, ""
	}
	cmd := exec.CommandContext(ctx, "docker", "volume", "ls", "-q", "--filter", "label=com.docker.compose.project="+composeProject)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return nil, strings.TrimSpace(out.String())
	}
	var names []string
	for _, line := range strings.Split(out.String(), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}
	return names, ""
}

func inspectComposeVolumeLabel(ctx context.Context, volumeName string) string {
	volumeName = strings.TrimSpace(volumeName)
	if volumeName == "" {
		return ""
	}
	cmd := exec.CommandContext(ctx, "docker", "volume", "inspect", "--format", "{{ index .Labels \"com.docker.compose.volume\" }}", volumeName)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(out.String())
}

// ListForApp returns Docker volume names that likely belong to this app.
// composeProjects lists compose project names (e.g. from COMPOSE_PROJECT_NAME or app slug) so volumes
// like "myproject_data" match even when the panel app id is a UUID.
func ListForApp(ctx context.Context, appID string, composeProjects []string) ([]string, string) {
	start := time.Now()
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return nil, "app id is required"
	}
	names, errMsg := List(ctx)
	if errMsg != "" {
		perflog.DockerOp("ListForApp", time.Since(start), "list error")
		return nil, errMsg
	}
	u := strings.ReplaceAll(appID, "-", "_")
	seen := map[string]struct{}{}
	var out []string
	add := func(vol string) {
		if _, ok := seen[vol]; ok {
			return
		}
		seen[vol] = struct{}{}
		out = append(out, vol)
	}
	for _, proj := range composeProjects {
		matched, msg := listByComposeProjectLabel(ctx, proj)
		if msg != "" {
			return nil, msg
		}
		for _, vol := range matched {
			add(vol)
		}
	}
	for _, n := range names {
		if matchesVolumeKey(n, appID) || matchesVolumeKey(n, u) {
			add(n)
			continue
		}
		for _, pref := range composeProjects {
			pref = strings.TrimSpace(pref)
			if pref == "" {
				continue
			}
			if n == pref || strings.HasPrefix(n, pref+"_") {
				add(n)
				break
			}
		}
	}
	sort.Strings(out)
	perflog.DockerOp("ListForApp", time.Since(start), fmt.Sprintf("app=%s projects=%d matched=%d", appID, len(composeProjects), len(out)))
	return out, ""
}

func volumeNameInList(name string, list []string) bool {
	for _, v := range list {
		if v == name {
			return true
		}
	}
	return false
}

func matchesVolumeKey(name, key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	return name == key || strings.HasPrefix(name, key+"_") || strings.HasPrefix(name, key+"-")
}

func isInfraBackupVolumeName(v string) bool {
	l := strings.ToLower(v)
	return strings.Contains(l, "caddy") || strings.Contains(l, "letsencrypt") || strings.Contains(l, "acme")
}

// ResolveBackupDataVolumeName picks the Docker volume for panel "volume" backup and restore.
// composeProjectCandidates should match the Volumes tab: active compose project first, then composeProjectCandidates (see Panel.backupVolumeComposeProjects).
// This replaces the old app.Name+"_data" heuristic, which breaks when COMPOSE_PROJECT_NAME / folder slug differs from the display name.
func ResolveBackupDataVolumeName(ctx context.Context, appID, appDisplayName string, composeProjectCandidates []string) (string, string) {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return "", "app id is required"
	}
	vols, errMsg := ListForApp(ctx, appID, composeProjectCandidates)
	if errMsg != "" {
		return "", errMsg
	}
	if len(vols) == 0 {
		return "", "no Docker volumes matched this app"
	}
	legacy := strings.TrimSpace(appDisplayName) + "_data"

	// 1) Exact Docker Compose label "data" is the strongest signal.
	for _, v := range vols {
		if inspectComposeVolumeLabel(ctx, v) == "data" {
			return v, ""
		}
	}

	// 2) Standard compose named volume "data" → project_data
	for _, proj := range composeProjectCandidates {
		proj = strings.TrimSpace(proj)
		if proj == "" {
			continue
		}
		want := proj + "_data"
		if volumeNameInList(want, vols) {
			return want, ""
		}
	}

	// 3) Legacy panel heuristic if that volume exists
	if ValidVolumeName(legacy) && volumeNameInList(legacy, vols) {
		return legacy, ""
	}

	if len(vols) == 1 {
		return vols[0], ""
	}

	// 4) Under a compose project prefix, prefer non-infra volumes
	for _, proj := range composeProjectCandidates {
		proj = strings.TrimSpace(proj)
		if proj == "" {
			continue
		}
		pref := proj + "_"
		var prefixed []string
		for _, v := range vols {
			if strings.HasPrefix(v, pref) && !isInfraBackupVolumeName(v) {
				prefixed = append(prefixed, v)
			}
		}
		if len(prefixed) == 1 {
			return prefixed[0], ""
		}
		if len(prefixed) > 1 {
			for _, hint := range []string{proj + "_app_data", proj + "_postgres_data", proj + "_mysql_data", proj + "_mariadb_data"} {
				if volumeNameInList(hint, prefixed) {
					return hint, ""
				}
			}
			var noLogs []string
			for _, v := range prefixed {
				if !strings.Contains(strings.ToLower(v), "logs") {
					noLogs = append(noLogs, v)
				}
			}
			if len(noLogs) == 1 {
				return noLogs[0], ""
			}
			if len(noLogs) > 0 {
				sort.Strings(noLogs)
				return noLogs[0], ""
			}
			sort.Strings(prefixed)
			return prefixed[0], ""
		}
	}

	for _, v := range vols {
		if !isInfraBackupVolumeName(v) {
			return v, ""
		}
	}
	return vols[0], ""
}

// RemoveMatching deletes Docker volumes whose names match ListForApp filters (best-effort).
func RemoveMatching(ctx context.Context, appID string) string {
	names, errMsg := ListForApp(ctx, appID, nil)
	if errMsg != "" {
		return errMsg
	}
	var errs []string
	for _, n := range names {
		cmd := exec.CommandContext(ctx, "docker", "volume", "rm", "-f", n)
		var b bytes.Buffer
		cmd.Stderr = &b
		if err := cmd.Run(); err != nil {
			msg := strings.TrimSpace(b.String())
			if msg == "" {
				msg = err.Error()
			}
			errs = append(errs, n+": "+msg)
		}
	}
	return strings.Join(errs, "; ")
}

type Entry struct {
	Name  string
	IsDir bool
}

func safeVolSubpath(rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return ".", nil
	}
	// Decode %2F and other encodings (repeat until stable) so "%2e%2e" cannot hide "..".
	dec := rel
	for i := 0; i < 8; i++ {
		next, err := url.PathUnescape(dec)
		if err != nil {
			return "", errors.New("invalid path encoding")
		}
		next = filepath.ToSlash(strings.TrimSpace(next))
		if next == dec {
			break
		}
		dec = next
	}
	dec = strings.Trim(dec, "/")
	if strings.ContainsRune(dec, '\x00') {
		return "", errors.New("invalid path")
	}
	parts := strings.Split(dec, "/")
	var safe []string
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			continue
		}
		if strings.Contains(p, "..") {
			return "", errors.New("invalid path segment")
		}
		safe = append(safe, p)
	}
	if len(safe) == 0 {
		return ".", nil
	}
	return strings.Join(safe, "/"), nil
}

func ListDir(ctx context.Context, vol, rel string) ([]Entry, string) {
	if !ValidVolumeName(vol) {
		return nil, "invalid volume name"
	}
	sub, err := safeVolSubpath(rel)
	if err != nil {
		return nil, err.Error()
	}
	
	// Use POSIX paths for paths inside the Linux container (Windows hosts must not use filepath.Join).
	targetPath := "/vol"
	if sub != "." && sub != "" {
		targetPath = path.Join("/vol", sub)
	}
	
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "-v", vol+":/vol:ro",
		"alpine:3.20", "ls", "-1Ap", targetPath)
	var out bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	
	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(errBuf.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return nil, fmt.Sprintf("failed to read directory: %v", errMsg)
	}
	
	var list []Entry
	for _, line := range strings.Split(out.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		isDir := strings.HasSuffix(line, "/")
		name := strings.TrimSuffix(line, "/")
		if name == "" || name == "." || name == ".." {
			continue
		}
		list = append(list, Entry{
			Name:  name,
			IsDir: isDir,
		})
	}
	
	return list, ""
}

// BackupToTemp creates a gzipped tar archive of vol into a temp file, returning
// the path. The caller must os.Remove(path) after use on success; on error this function removes the temp file.
// This is synchronous — it blocks until docker tar finishes, which ensures the
// full archive is ready before the HTTP response begins.
func BackupToTemp(ctx context.Context, vol string) (path string, err error) {
	if !ValidVolumeName(vol) {
		return "", errors.New("invalid volume")
	}
	f, err := os.CreateTemp(hostStagingDir(), "vol-backup-*.tar.gz")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := f.Name()
	defer func() {
		f.Close()
		if err != nil {
			os.Remove(tmpPath)
		}
	}()

	var stderrBuf bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "-v", vol+":/b:ro",
		"alpine:3.20", "tar", "czf", "-", "-C", "/b", ".")
	cmd.Stdout = f
	cmd.Stderr = &stderrBuf

	if runErr := cmd.Run(); runErr != nil {
		errMsg := strings.TrimSpace(stderrBuf.String())
		if errMsg == "" {
			errMsg = runErr.Error()
		}
		return "", fmt.Errorf("docker tar: %s", errMsg)
	}
	return tmpPath, nil
}

// RestoreTarGz streams a gzipped tar into the root of Docker volume vol.
func RestoreTarGz(ctx context.Context, vol string, in io.Reader) string {
	return restoreTarGzFromReader(ctx, vol, in)
}

func detectDominantVolumeOwner(ctx context.Context, dockerVolume, mountPoint string) string {
	switch mountPoint {
	case "/b", "/restore":
	default:
		return ""
	}
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"-v", dockerVolume+":"+mountPoint,
		"alpine:3.20", "sh", "-c",
		"find "+mountPoint+" -mindepth 1 -exec stat -c '%u:%g' {} + 2>/dev/null | sort | uniq -c | sort -nr | awk 'NR==1{print $2}'")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return ""
	}
	owner := strings.TrimSpace(out.String())
	if !uidGidRe.MatchString(owner) {
		return ""
	}
	return owner
}

func applyRecursiveVolumeOwner(ctx context.Context, dockerVolume, mountPoint, owner string) string {
	if !uidGidRe.MatchString(strings.TrimSpace(owner)) {
		return ""
	}
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"-v", dockerVolume+":"+mountPoint,
		"alpine:3.20", "sh", "-c",
		"chown -R "+owner+" "+mountPoint)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		s := strings.TrimSpace(out.String())
		if s == "" {
			s = err.Error()
		}
		return "chown: " + s
	}
	return ""
}

// dockerGnuTarExtract wipes dockerVolume at mountPoint and extracts a validated .tar.gz.
// The archive is streamed via stdin so no host-path bind-mount is needed — this works
// correctly whether the panel runs directly on the host or inside a container.
// mountPoint must be "/b" or "/restore".
// onProgress receives values 0–100; may be nil.
func dockerGnuTarExtract(ctx context.Context, dockerVolume, hostTarAbs, mountPoint string, onProgress func(int)) string {
	switch mountPoint {
	case "/b", "/restore":
	default:
		return "invalid mount point for extraction"
	}

	if onProgress != nil {
		onProgress(0)
	}

	f, err := os.Open(hostTarAbs)
	if err != nil {
		return err.Error()
	}
	defer f.Close()

	// Step 1: wipe the volume contents.
	wipe := exec.CommandContext(ctx, "docker", "run", "--rm",
		"-v", dockerVolume+":"+mountPoint,
		"alpine:3.20", "sh", "-c",
		"find "+mountPoint+" -mindepth 1 -maxdepth 1 -exec rm -rf {} +")
	var wipeOut bytes.Buffer
	wipe.Stdout = &wipeOut
	wipe.Stderr = &wipeOut
	if err := wipe.Run(); err != nil {
		s := strings.TrimSpace(wipeOut.String())
		if s == "" {
			s = err.Error()
		}
		return "wipe: " + s
	}

	if onProgress != nil {
		onProgress(10)
	}

	// Step 2: stream the tar.gz via stdin — no host-path bind-mount required.
	// Preserve numeric owner + permissions from the backup archive so database
	// volumes (e.g. Postgres) keep directory ownership such as pg_logical/*.
	extract := exec.CommandContext(ctx, "docker", "run", "--rm", "-i",
		"-v", dockerVolume+":"+mountPoint,
		"alpine:3.20", "sh", "-c",
		"apk add --no-cache tar >/dev/null 2>&1 && tar xzf - -C "+mountPoint+" --same-owner --same-permissions --numeric-owner 2>&1")
	extract.Stdin = f
	var extractOut bytes.Buffer
	extract.Stdout = &extractOut
	extract.Stderr = &extractOut

	// Emit intermediate progress ticks while extraction runs.
	done := make(chan struct{})
	if onProgress != nil {
		go func() {
			pct := 15
			ticker := time.NewTicker(800 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					if pct < 90 {
						pct += 5
					}
					onProgress(pct)
				}
			}
		}()
	}

	runErr := extract.Run()
	close(done)

	if runErr != nil {
		s := strings.TrimSpace(extractOut.String())
		if s == "" {
			s = runErr.Error()
		}
		return s
	}
	if onProgress != nil {
		onProgress(100)
	}
	return ""
}

// ExtractTarGzForBackupRestore extracts a validated .tar.gz into volume at /restore (app backup restore path).
func ExtractTarGzForBackupRestore(ctx context.Context, volumeName, hostTarPath string) string {
	abs, err := filepath.Abs(hostTarPath)
	if err != nil {
		return err.Error()
	}
	if err := ValidateTarGzPaths(abs); err != nil {
		return err.Error()
	}
	if !ValidVolumeName(volumeName) {
		return "invalid volume"
	}
	return dockerGnuTarExtract(ctx, volumeName, abs, "/restore", nil)
}

// zipToTarGzStream reads a zip archive and writes its contents as a gzip-compressed
// tar stream to w. Used to convert zip restores into the stdin-streaming tar path.
func zipToTarGzStream(zipPath string, w io.Writer) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)

	for _, f := range r.File {
		mode := f.Mode()
		perm := int64(mode.Perm())
		if perm == 0 {
			if mode.IsDir() {
				perm = 0755
			} else {
				perm = 0644
			}
		}
		if f.FileInfo().IsDir() {
			hdr := &tar.Header{
				Name:     f.Name + "/",
				Typeflag: tar.TypeDir,
				Mode:     perm,
			}
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		if mode&os.ModeSymlink != 0 {
			linkTarget, err := io.ReadAll(io.LimitReader(rc, 8192))
			_ = rc.Close()
			if err != nil {
				return err
			}
			hdr := &tar.Header{
				Name:     f.Name,
				Typeflag: tar.TypeSymlink,
				Mode:     perm,
				Linkname: string(linkTarget),
			}
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			continue
		}
		hdr := &tar.Header{
			Name:     f.Name,
			Typeflag: tar.TypeReg,
			Size:     int64(f.UncompressedSize64),
			Mode:     perm,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(tw, rc); err != nil {
			rc.Close()
			return err
		}
		rc.Close()
	}

	if err := tw.Close(); err != nil {
		return err
	}
	return gw.Close()
}

// RestoreZipFile extracts a .zip from a host file path into vol (full replace).
// Validates entry paths in Go first (zip slip), then converts to tar.gz and
// streams via stdin — no host-path bind-mount required.
func RestoreZipFile(ctx context.Context, vol, zipPath string, onProgress func(int)) string {
	if !ValidVolumeName(vol) {
		return "invalid volume"
	}
	abs, err := filepath.Abs(zipPath)
	if err != nil {
		return err.Error()
	}
	if err := ValidateZipPaths(abs); err != nil {
		return err.Error()
	}
	if onProgress != nil {
		onProgress(0)
	}
	// Zip archives do not reliably preserve Unix uid/gid metadata. Capture the
	// dominant owner from the existing volume before wiping it so we can reapply
	// ownership after extraction for common single-owner volumes like databases.
	prevOwner := detectDominantVolumeOwner(ctx, vol, "/b")

	// Wipe the volume first.
	wipe := exec.CommandContext(ctx, "docker", "run", "--rm",
		"-v", vol+":/b",
		"alpine:3.20", "sh", "-c",
		"find /b -mindepth 1 -maxdepth 1 -exec rm -rf {} +")
	var wipeOut bytes.Buffer
	wipe.Stdout = &wipeOut
	wipe.Stderr = &wipeOut
	if err := wipe.Run(); err != nil {
		s := strings.TrimSpace(wipeOut.String())
		if s == "" {
			s = err.Error()
		}
		return "wipe: " + s
	}

	if onProgress != nil {
		onProgress(10)
	}

	// Convert zip → tar.gz on-the-fly and stream via stdin.
	pr, pw := io.Pipe()
	var convErr error
	go func() {
		convErr = zipToTarGzStream(abs, pw)
		pw.CloseWithError(convErr)
	}()

	extract := exec.CommandContext(ctx, "docker", "run", "--rm", "-i",
		"-v", vol+":/b",
		"alpine:3.20", "sh", "-c",
		"apk add --no-cache tar >/dev/null 2>&1 && tar xzf - -C /b --same-owner --same-permissions --numeric-owner")
	extract.Stdin = pr
	var extractOut bytes.Buffer
	extract.Stdout = &extractOut
	extract.Stderr = &extractOut

	done := make(chan struct{})
	if onProgress != nil {
		go func() {
			pct := 15
			ticker := time.NewTicker(800 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					if pct < 90 {
						pct += 5
					}
					onProgress(pct)
				}
			}
		}()
	}

	runErr := extract.Run()
	close(done)
	_ = pr.Close()

	if convErr != nil {
		return "zip convert: " + convErr.Error()
	}
	if runErr != nil {
		s := strings.TrimSpace(extractOut.String())
		if s == "" {
			s = runErr.Error()
		}
		return s
	}
	if prevOwner != "" {
		if msg := applyRecursiveVolumeOwner(ctx, vol, "/b", prevOwner); msg != "" {
			return msg
		}
	}

	if onProgress != nil {
		onProgress(100)
	}
	return ""
}

// RestoreVolumeArchiveFromPath restores from a temp file: kind is "zip" or "tar-gz" (gzip-compressed tar).
func RestoreVolumeArchiveFromPath(ctx context.Context, vol, path, kind string, onProgress func(int)) string {
	switch kind {
	case "zip":
		return RestoreZipFile(ctx, vol, path, onProgress)
	default:
		return RestoreTarGzFile(ctx, vol, path, onProgress)
	}
}

// RestoreTarGzFile restores from a local file. Archives are validated in Go (path traversal, symlinks) then extracted with GNU tar in Docker.
func RestoreTarGzFile(ctx context.Context, vol, path string, onProgress func(int)) string {
	if !ValidVolumeName(vol) {
		return "invalid volume"
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err.Error()
	}
	if err := ValidateTarGzPaths(abs); err != nil {
		return err.Error()
	}
	return dockerGnuTarExtract(ctx, vol, abs, "/b", onProgress)
}

func restoreTarGzFromReader(ctx context.Context, vol string, in io.Reader) string {
	if !ValidVolumeName(vol) {
		return "invalid volume"
	}
	tmp, err := os.CreateTemp(hostStagingDir(), "vol-restore-*.tar.gz")
	if err != nil {
		return err.Error()
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := io.Copy(tmp, in); err != nil {
		return err.Error()
	}
	if err := tmp.Close(); err != nil {
		return err.Error()
	}
	if err := ValidateTarGzPaths(tmpPath); err != nil {
		return err.Error()
	}
	return dockerGnuTarExtract(ctx, vol, tmpPath, "/b", nil)
}
