//go:build windows

package cmd

import (
	"github.com/gorilla/websocket"
)

// monitorTerminalResize is a no-op on Windows where SIGWINCH is not supported.
func monitorTerminalResize(stdinFd int, ws *websocket.Conn) func() {
	return func() {}
}
