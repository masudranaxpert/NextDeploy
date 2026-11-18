package proxy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
)

var linkSeq atomic.Uint64

// WriteFileAtomic writes data to path via a temporary file in the same directory,
// syncing to disk before atomically renaming over path.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	for len(data) > 0 {
		n, werr := tmp.Write(data)
		if werr != nil {
			return fmt.Errorf("write temp %s: %w", tmpName, werr)
		}
		data = data[n:]
	}
	if runtime.GOOS != "windows" {
		if err := tmp.Chmod(perm); err != nil {
			return fmt.Errorf("chmod temp %s: %w", tmpName, err)
		}
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("fsync temp %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", tmpName, path, err)
	}
	cleanup = false
	return nil
}

// SwapDirLinkAtomic atomically updates linkPath to point to targetBaseName.
// targetBaseName must be a single relative directory name (e.g. "sites.gen-abc12345")
// so host/panel mounts and proxy container bind-mounts resolve the symlink identically.
func SwapDirLinkAtomic(linkPath, targetBaseName string) error {
	if err := validateRelativeTarget(targetBaseName); err != nil {
		return err
	}
	dir := filepath.Dir(linkPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmpLink := filepath.Join(dir, fmt.Sprintf(".tmp-link-%d-%d", os.Getpid(), linkSeq.Add(1)))
	_ = os.Remove(tmpLink)

	if err := os.Symlink(targetBaseName, tmpLink); err != nil {
		if runtime.GOOS == "windows" {
			// Unprivileged Windows environments without Developer Mode lack symlink privileges.
			return WriteFileAtomic(linkPath, []byte(targetBaseName), 0o644)
		}
		return fmt.Errorf("create temp symlink %s -> %s: %w", tmpLink, targetBaseName, err)
	}

	if err := os.Rename(tmpLink, linkPath); err != nil {
		if runtime.GOOS == "windows" {
			_ = os.Remove(linkPath)
			if rerr := os.Rename(tmpLink, linkPath); rerr == nil {
				return nil
			}
		}
		_ = os.Remove(tmpLink)
		return fmt.Errorf("rename symlink %s -> %s: %w", tmpLink, linkPath, err)
	}
	return nil
}

// ReadDirLink reads the symlink target of linkPath, or its Windows pointer fallback.
func ReadDirLink(linkPath string) (string, error) {
	if target, err := os.Readlink(linkPath); err == nil {
		return target, nil
	} else if runtime.GOOS != "windows" {
		return "", err
	}
	data, err := os.ReadFile(linkPath)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// RemoveDirLink removes linkPath if it exists.
func RemoveDirLink(linkPath string) error {
	err := os.Remove(linkPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func validateRelativeTarget(target string) error {
	if target == "" || target == "." || target == ".." {
		return fmt.Errorf("invalid relative symlink target %q", target)
	}
	if filepath.IsAbs(target) || strings.ContainsAny(target, `/\`) {
		return fmt.Errorf("symlink target %q must be a single relative directory name", target)
	}
	return nil
}
