package edge

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// FaultHook can be set in tests to simulate crashes or disk-full errors
// at specific stages of an atomic file write.
type AtomicWriteStage int

const (
	StageAfterWrite AtomicWriteStage = iota + 1
	StageAfterSync
	StageBeforeRename
)

var (
	// ErrDiskFull indicates ENOSPC / EDQUOT / write failure during atomic IO.
	ErrDiskFull = errors.New("disk full")

	// testAtomicHook is an optional package-internal test hook for crash simulation.
	testAtomicHook func(stage AtomicWriteStage, tmpPath, targetPath string) error
)

// CorruptFileError indicates a persistent state/config file failed to parse.
type CorruptFileError struct {
	Path       string
	Quarantine string
	Err        error
}

func (e *CorruptFileError) Error() string {
	if e.Quarantine != "" {
		return fmt.Sprintf("corrupt file %s (moved to %s): %v", e.Path, e.Quarantine, e.Err)
	}
	return fmt.Sprintf("corrupt file %s: %v", e.Path, e.Err)
}

func (e *CorruptFileError) Unwrap() error { return e.Err }

// WriteFileAtomic writes data to path via temp file in the same directory,
// chmod, fsync, rename, and directory fsync. On error no partial target exists.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return wrapDiskError(fmt.Errorf("create temp in %s: %w", dir, err))
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if err := writeAll(tmp, data); err != nil {
		return wrapDiskError(fmt.Errorf("write temp %s: %w", tmpName, err))
	}

	if testAtomicHook != nil {
		if err := testAtomicHook(StageAfterWrite, tmpName, path); err != nil {
			return err
		}
	}

	if runtime.GOOS != "windows" {
		if err := tmp.Chmod(perm); err != nil {
			return fmt.Errorf("chmod temp %s: %w", tmpName, err)
		}
	}

	if err := tmp.Sync(); err != nil {
		return wrapDiskError(fmt.Errorf("fsync temp %s: %w", tmpName, err))
	}

	if testAtomicHook != nil {
		if err := testAtomicHook(StageAfterSync, tmpName, path); err != nil {
			return err
		}
	}

	if err := tmp.Close(); err != nil {
		return wrapDiskError(fmt.Errorf("close temp %s: %w", tmpName, err))
	}

	if testAtomicHook != nil {
		if err := testAtomicHook(StageBeforeRename, tmpName, path); err != nil {
			return err
		}
	}

	if err := os.Rename(tmpName, path); err != nil {
		return wrapDiskError(fmt.Errorf("rename %s -> %s: %w", tmpName, path, err))
	}
	cleanup = false

	// Best-effort parent directory sync (unsupported on Windows).
	_ = syncDir(dir)
	return nil
}

// WriteJSONAtomic marshals v as indented JSON with a trailing newline and writes it atomically.
func WriteJSONAtomic(path string, v any, perm os.FileMode) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json for %s: %w", path, err)
	}
	b = append(b, '\n')
	return WriteFileAtomic(path, b, perm)
}

func writeAll(f *os.File, data []byte) error {
	for len(data) > 0 {
		n, err := f.Write(data)
		if err != nil {
			return err
		}
		data = data[n:]
	}
	return nil
}

func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// SwapSymlinkAtomic atomically points linkPath at target by creating a temporary
// symlink in the same directory and renaming it over linkPath.
func SwapSymlinkAtomic(linkPath, target string) error {
	dir := filepath.Dir(linkPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmpLink := filepath.Join(dir, fmt.Sprintf(".tmp-link-%d", time.Now().UnixNano()))
	_ = os.Remove(tmpLink)
	if err := os.Symlink(target, tmpLink); err != nil {
		return fmt.Errorf("create temp symlink %s -> %s: %w", tmpLink, target, err)
	}
	if err := os.Rename(tmpLink, linkPath); err != nil {
		_ = os.Remove(tmpLink)
		return fmt.Errorf("rename symlink %s -> %s: %w", tmpLink, linkPath, err)
	}
	_ = syncDir(dir)
	return nil
}

// quarantineCorruptFile renames a corrupt file aside so it is preserved for
// forensics while letting the daemon recover cleanly.
func quarantineCorruptFile(path string, now time.Time) (string, error) {
	dst := fmt.Sprintf("%s.corrupt.%s", path, now.UTC().Format("20060102T150405Z"))
	if err := os.Rename(path, dst); err != nil {
		return "", err
	}
	return dst, nil
}
