//go:build windows

package edge

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

func getDiskFreePct(path string) float64 {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	ptr, err := windows.UTF16PtrFromString(abs)
	if err != nil {
		return 0
	}
	var freeAvail, totalBytes, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(ptr, &freeAvail, &totalBytes, &totalFree); err != nil || totalBytes == 0 {
		return 0
	}
	pct := (float64(freeAvail) / float64(totalBytes)) * 100.0
	return float64(int(pct*10)) / 10
}

func getMemFreeMB() int64 {
	return 0
}
