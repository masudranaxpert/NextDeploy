//go:build !windows

package phphosting

import (
	"os"
	"syscall"
)

func ownedBy(info os.FileInfo, uid int) bool {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return int(st.Uid) == uid && int(st.Gid) == uid
	}
	return true
}
