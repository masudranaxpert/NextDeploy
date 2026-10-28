//go:build !linux && !windows

package phphosting

import "os"

// The panel's PHP runtimes only run on Linux hosts; elsewhere (macOS dev builds) ownership
// is not checked so the package still compiles.
func ownedBy(info os.FileInfo, uid int) bool { return true }
