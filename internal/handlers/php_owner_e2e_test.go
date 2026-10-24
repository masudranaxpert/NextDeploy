//go:build e2e && !windows

package handlers

import (
	"os"
	"syscall"
)

func phpOwnedBy(info os.FileInfo, uid int) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == uid
}
