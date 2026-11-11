//go:build windows

package edge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// ErrLockHeld is returned when another nd-edge instance already holds the lock.
var ErrLockHeld = errors.New("nd-edge lock is already held by another process")

// FileLock wraps an exclusive Windows file lock handle for local dev/tests.
type FileLock struct {
	file *os.File
	path string
}

// AcquireLock attempts a non-blocking exclusive file lock on path.
func AcquireLock(path string) (*FileLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir lock dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", path, err)
	}
	var ol windows.Overlapped
	err = windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		&ol,
	)
	if err != nil {
		_ = f.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, ErrLockHeld
		}
		return nil, fmt.Errorf("lock file %s: %w", path, err)
	}
	return &FileLock{file: f, path: path}, nil
}

// AcquireLockBlocking polls until the exclusive lock is acquired or ctx expires.
func AcquireLockBlocking(ctx context.Context, path string) (*FileLock, error) {
	for {
		l, err := AcquireLock(path)
		if err == nil {
			return l, nil
		}
		if !errors.Is(err, ErrLockHeld) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Release unlocks and closes the lock file handle.
func (l *FileLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	var ol windows.Overlapped
	_ = windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, 1, 0, &ol)
	err := l.file.Close()
	l.file = nil
	return err
}

// WritePIDFile writes the current process PID atomically to path.
func WritePIDFile(path string) error {
	return WriteFileAtomic(path, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644)
}

func wrapDiskError(err error) error {
	// ERROR_DISK_FULL (112) / ERROR_HANDLE_DISK_FULL (39)
	var errno syscall.Errno
	if errors.As(err, &errno) && (errno == 112 || errno == 39) {
		return fmt.Errorf("%w: %v", ErrDiskFull, err)
	}
	return err
}

// SwapDirLinkAtomic attempts a real symlink and falls back to an atomic target
// pointer file when running unprivileged on Windows without symlink privileges.
func SwapDirLinkAtomic(linkPath, targetDir string) error {
	if err := SwapSymlinkAtomic(linkPath, targetDir); err == nil {
		return nil
	}
	return WriteFileAtomic(linkPath, []byte(targetDir), 0o644)
}

// ReadDirLink reads a symlink target or Windows pointer file written by SwapDirLinkAtomic.
func ReadDirLink(linkPath string) (string, error) {
	if target, err := os.Readlink(linkPath); err == nil {
		return target, nil
	}
	b, err := os.ReadFile(linkPath)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
