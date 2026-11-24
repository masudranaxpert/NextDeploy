package workspace

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Store struct {
	Root string
}

const ReservedDir = ".nextdeploy"

// Standard POSIX permissions for app workspace files and directories so non-root
// container users (e.g. USER node/app/worker in Dockerfiles or bind mounts) can
// read workspace files and traverse directories, matching git clone defaults.
const (
	WorkspaceDirMode    os.FileMode = 0755
	WorkspaceFileMode   os.FileMode = 0644
	WorkspaceExecMode   os.FileMode = 0755
	WorkspaceSecretMode os.FileMode = 0600
)

// FileModeForPath returns the target POSIX file mode for a workspace file:
// secret .env files stay 0600, scripts/executables get 0755, and regular files get 0644.
func FileModeForPath(relPath string, data []byte, hint os.FileMode) os.FileMode {
	if IsProtectedEnvFile(relPath) {
		return WorkspaceSecretMode
	}
	if hint&0111 != 0 {
		return WorkspaceExecMode
	}
	ext := strings.ToLower(filepath.Ext(relPath))
	if ext == ".sh" || ext == ".bash" || bytes.HasPrefix(data, []byte("#!")) {
		return WorkspaceExecMode
	}
	return WorkspaceFileMode
}

// EnsureReadableDir creates dir (0755) and repairs any restrictive existing directories
// along the chain between base and dir (inclusive) so non-root container users can traverse them.
func EnsureReadableDir(base, dir string) error {
	if err := os.MkdirAll(dir, WorkspaceDirMode); err != nil {
		return err
	}
	cleanBase := filepath.Clean(base)
	cleanDir := filepath.Clean(dir)
	for cur := cleanDir; ; {
		if fi, err := os.Stat(cur); err == nil && fi.IsDir() && fi.Mode().Perm()&0055 != 0055 {
			_ = os.Chmod(cur, fi.Mode().Perm()|WorkspaceDirMode)
		}
		if cur == cleanBase {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		if rel, err := filepath.Rel(cleanBase, parent); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			break
		}
		cur = parent
	}
	return nil
}

// WriteFile writes data to dest with explicit mode and enforces permissions even when overwriting
// an existing file that previously had restrictive (e.g. 0600/0640) permissions.
func WriteFile(dest string, data []byte, mode os.FileMode) error {
	if err := os.WriteFile(dest, data, mode); err != nil {
		return err
	}
	return os.Chmod(dest, mode)
}

func NewStore(root string) *Store {
	return &Store{Root: root}
}

func (s *Store) ensureRoot() error {
	return os.MkdirAll(s.Root, WorkspaceDirMode)
}

func (s *Store) Path(id string) string {
	return filepath.Join(s.Root, id)
}

func (s *Store) ReservedPath(id string) string {
	return filepath.Join(s.Path(id), ReservedDir)
}

func (s *Store) SaveUploadedFile(wsID, relPath string, r io.Reader) (string, error) {
	base := s.Path(wsID)
	relPath = strings.ReplaceAll(relPath, "\\", "/")
	parts := strings.Split(relPath, "/")
	var safeParts []string
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			continue
		}
		safeParts = append(safeParts, p)
	}
	if len(safeParts) == 0 {
		return "", os.ErrInvalid
	}
	full := filepath.Join(append([]string{base}, safeParts...)...)
	if !strings.HasPrefix(full, filepath.Clean(base)+string(os.PathSeparator)) && full != filepath.Clean(base) {
		return "", os.ErrInvalid
	}
	if err := containedInBase(filepath.Clean(base), full); err != nil {
		return "", err
	}
	if err := EnsureReadableDir(base, filepath.Dir(full)); err != nil {
		return "", err
	}
	// Re-check after the directories exist, then refuse to follow a link at the last step.
	if err := containedInBase(filepath.Clean(base), full); err != nil {
		return "", err
	}
	mode := FileModeForPath(strings.Join(safeParts, "/"), nil, 0)
	f, err := os.OpenFile(full, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|noFollowFlag, mode)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		return "", err
	}
	_ = f.Chmod(mode)
	return full, nil
}

// ClearUploadedProjectForGitSource removes everything under the app workspace root except
// .panel-meta so switching from file uploads to Git does not leave stale compose/files on disk.
// Also removes .nextdeploy so the next clone uses a clean checkout directory.
func (s *Store) ClearUploadedProjectForGitSource(wsID string) error {
	base := s.Path(wsID)
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(base, WorkspaceDirMode)
		}
		return err
	}
	for _, e := range entries {
		if e.Name() == ".panel-meta" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(base, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) RemoveRel(wsID, rel string) error {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	rel = strings.Trim(rel, "/")
	if rel == "" {
		return os.ErrInvalid
	}
	parts := strings.Split(rel, "/")
	var safe []string
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			continue
		}
		safe = append(safe, p)
	}
	if len(safe) == 0 {
		return os.ErrInvalid
	}
	joined := strings.Join(safe, "/")
	if joined == ".panel-meta" || strings.HasPrefix(joined, ".panel-meta/") {
		return os.ErrInvalid
	}
	base := filepath.Clean(s.Path(wsID))
	full := filepath.Join(append([]string{base}, safe...)...)
	cf := filepath.Clean(full)
	r, err := filepath.Rel(base, cf)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(os.PathSeparator)) {
		return os.ErrInvalid
	}
	if r == "." {
		return os.ErrInvalid
	}
	return os.RemoveAll(cf)
}

// RemoveGitRepoRel removes a file or directory under the git checkout root (…/repo).
// Deletion of .git and path traversal are rejected.
func (s *Store) RemoveGitRepoRel(wsID, rel string) error {
	full, err := s.SafeGitRepoFilePath(wsID, rel)
	if err != nil {
		return err
	}
	return os.RemoveAll(full)
}

// SafeFilePath returns the absolute filesystem path for rel under wsID if it stays inside the workspace and is not under .panel-meta.
func (s *Store) SafeFilePath(wsID, rel string) (string, error) {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	rel = strings.Trim(rel, "/")
	if rel == "" {
		return "", os.ErrInvalid
	}
	parts := strings.Split(rel, "/")
	var safe []string
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			continue
		}
		safe = append(safe, p)
	}
	if len(safe) == 0 {
		return "", os.ErrInvalid
	}
	joined := strings.Join(safe, "/")
	if joined == ".panel-meta" || strings.HasPrefix(joined, ".panel-meta/") {
		return "", os.ErrInvalid
	}
	base := filepath.Clean(s.Path(wsID))
	full := filepath.Join(append([]string{base}, safe...)...)
	cf := filepath.Clean(full)
	rp, err := filepath.Rel(base, cf)
	if err != nil || rp == ".." || strings.HasPrefix(rp, ".."+string(os.PathSeparator)) {
		return "", os.ErrInvalid
	}
	if rp == "." {
		return "", os.ErrInvalid
	}
	if err := containedInBase(base, cf); err != nil {
		return "", err
	}
	return cf, nil
}

// SafeGitRepoFilePath resolves rel under <workspace>/.nextdeploy/repo (the git checkout).
// Access to .git and paths outside the checkout is denied.
func (s *Store) SafeGitRepoFilePath(wsID, rel string) (string, error) {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	rel = strings.Trim(rel, "/")
	base := filepath.Clean(filepath.Join(s.ReservedPath(wsID), "repo"))
	if rel == "" {
		return "", os.ErrInvalid
	}
	parts := strings.Split(rel, "/")
	var safe []string
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			continue
		}
		if p == ".git" {
			return "", os.ErrInvalid
		}
		safe = append(safe, p)
	}
	if len(safe) == 0 {
		return "", os.ErrInvalid
	}
	full := filepath.Join(append([]string{base}, safe...)...)
	cf := filepath.Clean(full)
	rp, err := filepath.Rel(base, cf)
	if err != nil || rp == ".." || strings.HasPrefix(rp, ".."+string(os.PathSeparator)) {
		return "", os.ErrInvalid
	}
	if rp == ".git" || strings.HasPrefix(rp, ".git"+string(os.PathSeparator)) {
		return "", os.ErrInvalid
	}
	if err := containedInBase(base, cf); err != nil {
		return "", err
	}
	return cf, nil
}

type FileEntry struct {
	Name    string
	RelPath string
	Size    int64
	ModTime time.Time
	IsDir   bool
	Perms   string
}

func (s *Store) HasDockerArtifacts(wsID string) (hasDockerfile, hasCompose bool) {
	repoDir := filepath.Join(s.ReservedPath(wsID), "repo")
	targetDirs := []string{s.Path(wsID)}
	if st, err := os.Stat(repoDir); err == nil && st.IsDir() {
		targetDirs = []string{repoDir}
	}
	for _, base := range targetDirs {
		for _, n := range []string{"Dockerfile", "dockerfile"} {
			if st, err := os.Stat(filepath.Join(base, n)); err == nil && !st.IsDir() {
				hasDockerfile = true
			}
		}
		for _, n := range []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"} {
			if st, err := os.Stat(filepath.Join(base, n)); err == nil && !st.IsDir() {
				hasCompose = true
			}
		}
	}
	return hasDockerfile, hasCompose
}

// DotEnvPath returns the path to the project .env under the compose workspace root.
func (s *Store) DotEnvPath(baseDir string) string {
	return filepath.Join(filepath.Clean(baseDir), ".env")
}

// ReadDotEnv reads project .env from baseDir when the file exists.
func (s *Store) ReadDotEnv(baseDir string) (string, error) {
	p := s.DotEnvPath(baseDir)
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// containedInBase fails when target, after following symlinks, lands outside base. The path may
// not exist yet (a file about to be created): then its deepest existing parent is resolved, so a
// planted link such as htdocs/l -> /data/workspaces/<other app> cannot be used to write or read
// through, even for a name that does not exist yet.
func containedInBase(base, target string) error {
	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		realBase = base
	}
	existing := target
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing || len(parent) < len(base) {
			existing = base
			break
		}
		existing = parent
	}
	realExisting, err := filepath.EvalSymlinks(existing)
	if err != nil {
		if existing == base {
			return nil // the workspace folder does not exist yet, so nothing in it can be a link
		}
		return os.ErrPermission
	}
	rel, err := filepath.Rel(realBase, realExisting)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return os.ErrPermission
	}
	return nil
}
