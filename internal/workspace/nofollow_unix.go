//go:build !windows

package workspace

import "syscall"

// noFollowFlag makes open() fail when the final path part is a symlink.
const noFollowFlag = syscall.O_NOFOLLOW
