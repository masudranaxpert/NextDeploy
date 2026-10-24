//go:build windows

package phphosting

import "os"

func ownedBy(info os.FileInfo, uid int) bool { return true }
