//go:build windows

package cmd

import (
	"errors"
	"os"
	"os/signal"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func edgeGetEUID() int {
	return -1
}

func edgeDiskFreeBytes(path string) (uint64, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	ptr, err := windows.UTF16PtrFromString(abs)
	if err != nil {
		return 0, err
	}
	var freeAvail, totalBytes, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(ptr, &freeAvail, &totalBytes, &totalFree); err != nil {
		return 0, err
	}
	return freeAvail, nil
}

func edgeChownGroup(_, _ string, _ bool) error {
	return nil
}

func edgeDaemonSignalNotify() (<-chan os.Signal, <-chan os.Signal, func()) {
	reloadCh := make(chan os.Signal, 1)
	stopCh := make(chan os.Signal, 2)
	signal.Notify(stopCh, os.Interrupt)
	cleanup := func() {
		signal.Stop(stopCh)
	}
	return reloadCh, stopCh, cleanup
}

func edgeSendSIGHUP(_ int) error {
	return errors.New("SIGHUP is not supported on Windows")
}
