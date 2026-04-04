package workspace

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (s *Store) ClearAllUserFiles(wsID string) error {
	base := s.Path(wsID)
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(base, 0750)
		}
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(base, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) WriteMeta(wsID string, name string) error {
	base := s.Path(wsID)
	if err := os.MkdirAll(base, 0750); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(base, ".panel-meta"), []byte(strings.TrimSpace(name)+"\n"), 0600)
}

func (s *Store) ExtractZip(wsID string, r io.ReaderAt, size int64) error {
	base := s.Path(wsID)
	if err := os.MkdirAll(base, 0750); err != nil {
		return err
	}
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		p := filepath.Clean(f.Name)
		if p == "." || strings.HasPrefix(p, "..") {
			continue
		}
		dest := filepath.Join(base, p)
		relToBase, err := filepath.Rel(filepath.Clean(base), filepath.Clean(dest))
		if err != nil || relToBase == ".." || strings.HasPrefix(relToBase, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("illegal zip path: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dest, 0750); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0750); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0640)
		if err != nil {
			_ = rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		_ = out.Close()
		_ = rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListChildren(wsID, rel string) ([]FileEntry, error) {
	base := s.Path(wsID)
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	rel = strings.Trim(rel, "/")
	var dir string
	if rel == "" {
		dir = base
	} else {
		parts := strings.Split(rel, "/")
		var safe []string
		for _, p := range parts {
			if p == "" || p == "." || p == ".." {
				continue
			}
			safe = append(safe, p)
		}
		if len(safe) == 0 {
			dir = base
		} else {
			dir = filepath.Join(append([]string{base}, safe...)...)
		}
	}
	cleanBase := filepath.Clean(base)
	cleanDir := filepath.Clean(dir)
	if !strings.HasPrefix(cleanDir, cleanBase) {
		return nil, os.ErrInvalid
	}
	entries, err := os.ReadDir(cleanDir)
	if err != nil {
		return nil, err
	}
	var list []FileEntry
	for _, e := range entries {
		name := e.Name()
		if name == ".panel-meta" || name == ReservedDir {
			continue
		}
		relPath := name
		if rel != "" {
			relPath = rel + "/" + name
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		list = append(list, FileEntry{
			Name:    name,
			RelPath: filepath.ToSlash(relPath),
			Size:    info.Size(),
			ModTime: info.ModTime(),
			IsDir:   e.IsDir(),
		})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].IsDir != list[j].IsDir {
			return list[i].IsDir
		}
		return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
	})
	return list, nil
}

func (s *Store) ParentRel(rel string) string {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	rel = strings.Trim(rel, "/")
	if rel == "" {
		return ""
	}
	i := strings.LastIndex(rel, "/")
	if i < 0 {
		return ""
	}
	return rel[:i]
}
