//go:build !windows

package cmd

import (
	"encoding/json"
	"os"
	"os/signal"
	"syscall"

	"github.com/gorilla/websocket"
	"golang.org/x/term"
)

// monitorTerminalResize listens for SIGWINCH signals on POSIX systems and notifies the server.
func monitorTerminalResize(stdinFd int, ws *websocket.Conn) func() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGWINCH)

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case <-sigChan:
				if w, h, err := term.GetSize(stdinFd); err == nil && w > 0 && h > 0 {
					resizeMsg := map[string]interface{}{
						"op":   "resize",
						"cols": w,
						"rows": h,
					}
					b, _ := json.Marshal(resizeMsg)
					_ = ws.WriteMessage(websocket.TextMessage, b)
				}
			}
		}
	}()

	return func() {
		signal.Stop(sigChan)
		close(done)
	}
}
