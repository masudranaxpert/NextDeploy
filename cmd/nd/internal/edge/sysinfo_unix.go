//go:build !windows

package edge

import (
	"bufio"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func getDiskFreePct(path string) float64 {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil || st.Blocks == 0 {
		return 0
	}
	pct := (float64(st.Bavail) / float64(st.Blocks)) * 100.0
	if pct < 0 {
		return 0
	}
	return float64(int(pct*10)) / 10
}

func getMemFreeMB() int64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()

	var availKB, freeKB int64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		v, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemAvailable:":
			availKB = v
		case "MemFree:":
			freeKB = v
		}
	}
	if availKB > 0 {
		return availKB / 1024
	}
	return freeKB / 1024
}
