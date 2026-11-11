//go:build !windows

package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

func edgeGetEUID() int {
	return os.Geteuid()
}

func edgeDiskFreeBytes(path string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

func edgeChownGroup(rootPath, groupName string, recursive bool) error {
	if groupName == "" {
		return nil
	}
	grp, err := user.LookupGroup(groupName)
	if err != nil {
		// Fall back to user lookup if group name differs from primary gid.
		u, uErr := user.Lookup(groupName)
		if uErr != nil {
			return fmt.Errorf("lookup group %q: %w", groupName, err)
		}
		gid, convErr := strconv.Atoi(u.Gid)
		if convErr != nil {
			return convErr
		}
		return chownTreeGID(rootPath, gid, recursive)
	}
	gid, err := strconv.Atoi(grp.Gid)
	if err != nil {
		return err
	}
	return chownTreeGID(rootPath, gid, recursive)
}

func chownTreeGID(rootPath string, gid int, recursive bool) error {
	if !recursive {
		return os.Chown(rootPath, 0, gid)
	}
	return filepath.WalkDir(rootPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return os.Lchown(p, 0, gid)
		}
		return os.Chown(p, 0, gid)
	})
}

func edgeDaemonSignalNotify() (<-chan os.Signal, <-chan os.Signal, func()) {
	reloadCh := make(chan os.Signal, 4)
	stopCh := make(chan os.Signal, 2)
	signal.Notify(reloadCh, syscall.SIGHUP)
	signal.Notify(stopCh, os.Interrupt, syscall.SIGTERM)
	cleanup := func() {
		signal.Stop(reloadCh)
		signal.Stop(stopCh)
	}
	return reloadCh, stopCh, cleanup
}

func edgeSendSIGHUP(pid int) error {
	return syscall.Kill(pid, syscall.SIGHUP)
}
