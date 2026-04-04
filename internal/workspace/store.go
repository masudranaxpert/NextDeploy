package workspace

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Meta struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

type Store struct {
	Root string
}

func NewStore(root string) *Store {
	return &Store{Root: root}
}

func (s *Store) ensureRoot() error {
	return os.MkdirAll(s.Root, 0750)
}

func (s *Store) Create(name string) (Meta, error) {
	if err := s.ensureRoot(); err != nil {
		return Meta{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "workspace"
	}
	id := uuid.NewString()
	dir := filepath.Join(s.Root, id)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return Meta{}, err
	}
	meta := Meta{ID: id, Name: name, CreatedAt: time.Now().UTC()}
	if err := os.WriteFile(filepath.Join(dir, ".panel-meta"), []byte(name+"\n"), 0600); err != nil {
		return Meta{}, err
	}
	return meta, nil
}

func (s *Store) Path(id string) string {
	return filepath.Join(s.Root, id)
}

func (s *Store) List() ([]Meta, error) {
	if err := s.ensureRoot(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return nil, err
	}
	var out []Meta
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		st, err := e.Info()
		if err != nil {
			continue
		}
		name := id
		if b, err := os.ReadFile(filepath.Join(s.Root, id, ".panel-meta")); err == nil {
			name = strings.TrimSpace(string(b))
		}
		out = append(out, Meta{ID: id, Name: name, CreatedAt: st.ModTime()})
	}
	return out, nil
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

type FileEntry struct {
	Name    string
	RelPath string
	Size    int64
	ModTime time.Time
	IsDir   bool
}

func (s *Store) ListFiles(wsID string) ([]FileEntry, error) {
	base := s.Path(wsID)
	var list []FileEntry
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if strings.HasPrefix(rel, ".panel-meta") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		list = append(list, FileEntry{
			Name:    filepath.Base(rel),
			RelPath: filepath.ToSlash(rel),
			Size:    info.Size(),
			ModTime: info.ModTime(),
			IsDir:   d.IsDir(),
		})
		return nil
	})
	return list, err
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

func (s *Store) ComposeFilePath(wsID string) string {
	base := s.Path(wsID)
	for _, n := range []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"} {
		p := filepath.Join(base, n)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return filepath.Join(base, "docker-compose.yml")
}
