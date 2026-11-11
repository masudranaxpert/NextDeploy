//go:build !windows

package edge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// ErrLockHeld is returned when another nd-edge instance already holds the flock.
var ErrLockHeld = errors.New("nd-edge lock is already held by another process")

// FileLock wraps an open advisory flock handle on /run/nd-edge.lock.
type FileLock struct {
	file *os.File
	path string
}

// AcquireLock attempts a non-blocking exclusive flock on path.
func AcquireLock(path string) (*FileLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir lock dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", path, err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrLockHeld
		}
		return nil, fmt.Errorf("flock %s: %w", path, err)
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
	_ = unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
	err := l.file.Close()
	l.file = nil
	return err
}

// WritePIDFile writes the current process PID atomically to path.
func WritePIDFile(path string) error {
	return WriteFileAtomic(path, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644)
}

func wrapDiskError(err error) error {
	if errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT) {
		return fmt.Errorf("%w: %v", ErrDiskFull, err)
	}
	return err
}
