package workspace

import (
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

// PanelComposeEnvFile is written under ReservedDir; passed to docker compose --env-file (does not touch project .env).
const PanelComposeEnvFile = "panel.compose.env"

func NewStore(root string) *Store {
	return &Store{Root: root}
}

func (s *Store) ensureRoot() error {
	return os.MkdirAll(s.Root, 0750)
}

func (s *Store) Path(id string) string {
	return filepath.Join(s.Root, id)
}

func (s *Store) ReservedPath(id string) string {
	return filepath.Join(s.Path(id), ReservedDir)
}

// PanelComposeEnvPath is the absolute path to the panel-managed env file used by docker compose.
func (s *Store) PanelComposeEnvPath(id string) string {
	return filepath.Join(s.ReservedPath(id), PanelComposeEnvFile)
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
	if err := os.MkdirAll(filepath.Dir(full), 0750); err != nil {
		return "", err
	}
	f, err := os.OpenFile(full, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0640)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		return "", err
	}
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
			return os.MkdirAll(base, 0750)
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
	return cf, nil
}

type FileEntry struct {
	Name    string
	RelPath string
	Size    int64
	ModTime time.Time
	IsDir   bool
}

func (s *Store) HasDockerArtifacts(wsID string) (hasDockerfile, hasCompose bool) {
	base := s.Path(wsID)
	for _, n := range []string{"Dockerfile", "dockerfile"} {
		if st, err := os.Stat(filepath.Join(base, n)); err == nil && !st.IsDir() {
			hasDockerfile = true
			break
		}
	}
	for _, n := range []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"} {
		if st, err := os.Stat(filepath.Join(base, n)); err == nil && !st.IsDir() {
			hasCompose = true
			break
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
