//go:build windows

package main

import "syscall"

func setSmallRcvBuf(fd uintptr) error {
	return syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF, 4096)
}
