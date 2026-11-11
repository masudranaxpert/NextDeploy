//go:build !windows

package cmd

import (
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"

	"nd/internal/edge"
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
	return edge.ChownGroup(rootPath, groupName, recursive)
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
