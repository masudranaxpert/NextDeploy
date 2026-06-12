package workspace

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"panel/internal/caddy"
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

const maxUncompressedZipBytes = 2 << 30

func (s *Store) ExtractZip(wsID string, r io.ReaderAt, size int64) error {
	base := s.Path(wsID)
	if err := os.MkdirAll(base, 0750); err != nil {
		return err
	}
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return err
	}

	var declaredTotal uint64
	for _, f := range zr.File {
		declaredTotal += f.UncompressedSize64
		if declaredTotal > maxUncompressedZipBytes {
			return fmt.Errorf("zip archive too large: declared uncompressed total exceeds %d bytes", maxUncompressedZipBytes)
		}
	}

	var extractedTotal uint64
	for _, f := range zr.File {
		p := filepath.Clean(f.Name)
		if p == "." || strings.HasPrefix(p, "..") {
			continue
		}
		if filepath.Base(p) == caddy.GeneratedCompose {
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
		written, err := io.CopyN(out, rc, int64(f.UncompressedSize64)+1)
		_ = out.Close()
		_ = rc.Close()
		if err != nil && err != io.EOF {
			return err
		}
		if written > int64(f.UncompressedSize64) {
			_ = os.Remove(dest)
			return fmt.Errorf("file %s: actual size exceeds declared size", f.Name)
		}
		extractedTotal += uint64(written)
		if extractedTotal > maxUncompressedZipBytes {
			_ = os.Remove(dest)
			return fmt.Errorf("extracted bytes exceed limit (%d)", maxUncompressedZipBytes)
		}
	}
	return nil
}

// ValidateZipArchive checks path safety, total declared uncompressed size, and per-entry
// stream sizes without writing to a workspace. Use before ClearAllUserFiles so a bad
// archive does not wipe existing files.
func ValidateZipArchive(r io.ReaderAt, size int64) error {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return err
	}
	var totalUncompressed uint64
	for _, f := range zr.File {
		totalUncompressed += f.UncompressedSize64
		if totalUncompressed > maxUncompressedZipBytes {
			return fmt.Errorf("zip archive too large: uncompressed size exceeds %d bytes", maxUncompressedZipBytes)
		}
	}
	fakeBase := filepath.Clean(filepath.Join(os.TempDir(), "nextdeploy-zip-check"))
	for _, f := range zr.File {
		p := filepath.Clean(f.Name)
		if p == "." || strings.HasPrefix(p, "..") {
			continue
		}
		dest := filepath.Join(fakeBase, p)
		relToBase, err := filepath.Rel(filepath.Clean(fakeBase), filepath.Clean(dest))
		if err != nil || relToBase == ".." || strings.HasPrefix(relToBase, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("illegal zip path: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		written, err := io.CopyN(io.Discard, rc, int64(f.UncompressedSize64)+1)
		_ = rc.Close()
		if err != nil && err != io.EOF {
			return err
		}
		if written > int64(f.UncompressedSize64) {
			return fmt.Errorf("file %s: actual size exceeds declared size", f.Name)
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
		if name == ".panel-meta" || name == ReservedDir || name == caddy.GeneratedCompose {
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
			Perms:   info.Mode().Perm().String(),
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

// ListGitRepoChildren lists files and directories under the git checkout root (…/repo).
// The .git directory is never exposed. Path traversal and ".git" path segments are rejected.
func (s *Store) ListGitRepoChildren(wsID, rel string) ([]FileEntry, error) {
	base := filepath.Clean(filepath.Join(s.ReservedPath(wsID), "repo"))
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	rel = strings.Trim(rel, "/")
	if rel != "" {
		for _, seg := range strings.Split(rel, "/") {
			if seg == ".git" {
				return nil, os.ErrInvalid
			}
		}
	}
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
			if p == ".git" {
				return nil, os.ErrInvalid
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
	rp, err := filepath.Rel(cleanBase, cleanDir)
	if err != nil || rp == ".." || strings.HasPrefix(rp, ".."+string(os.PathSeparator)) {
		return nil, os.ErrInvalid
	}
	gitDir := filepath.Join(cleanBase, ".git")
	if cleanDir == gitDir || strings.HasPrefix(cleanDir, gitDir+string(os.PathSeparator)) {
		return nil, os.ErrInvalid
	}
	entries, err := os.ReadDir(cleanDir)
	if err != nil {
		return nil, err
	}
	var list []FileEntry
	for _, e := range entries {
		name := e.Name()
		if name == ".git" {
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
			Perms:   info.Mode().Perm().String(),
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

// ParentRel strips the last path segment from a relative path.
func ParentRel(rel string) string {
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

func (s *Store) ParentRel(rel string) string {
	return ParentRel(rel)
}
