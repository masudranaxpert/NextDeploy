package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"nd/internal/client"

	"github.com/gorilla/websocket"
	"golang.org/x/term"
)

// RunShell opens an interactive TTY shell into an application container or PHP runtime.
// Usage: nd shell [app_id] [-c container]
func RunShell(cl *client.Client, args []string) error {
	var (
		container string
		cleanArgs []string
	)

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-h" || arg == "--help" {
			fmt.Println("Usage: nd shell [app_id] [-c/--container <name>]")
			fmt.Println("\nOpen an interactive TTY bash/sh shell inside an app container or PHP runtime.")
			fmt.Println("\nFlags:")
			fmt.Println("  -a, --app <app_id>               Target application ID (optional if linked)")
			fmt.Println("  -c, --container, -s, --service   Target container or service name (optional)")
			return nil
		} else if (arg == "-c" || arg == "--container" || arg == "-s" || arg == "--service") && i+1 < len(args) {
			container = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "--container=") {
			container = strings.TrimPrefix(arg, "--container=")
		} else if strings.HasPrefix(arg, "--service=") {
			container = strings.TrimPrefix(arg, "--service=")
		} else {
			cleanArgs = append(cleanArgs, arg)
		}
	}

	appID, _, err := resolveAppID(cleanArgs)
	if err != nil {
		return err
	}

	return runPTYSession(cl, "/api/v1/apps/"+appID+"/ws/terminal", container, "")
}

// RunServerShell opens an interactive admin host root terminal.
// Usage: nd server-shell
func RunServerShell(cl *client.Client, args []string) error {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			fmt.Println("Usage: nd server-shell")
			fmt.Println("\nOpen an interactive administrator host shell on the VPS.")
			return nil
		}
	}
	return runPTYSession(cl, "/api/v1/ws/host-terminal", "", "")
}

// runPTYSession handles the bidirectional TTY session over WebSocket.
func runPTYSession(cl *client.Client, path, container, cmd string) error {
	stdinFd := int(os.Stdin.Fd())
	isTerminal := term.IsTerminal(stdinFd)

	cols, rows := 80, 24
	if isTerminal {
		if w, h, err := term.GetSize(stdinFd); err == nil && w > 0 && h > 0 {
			cols, rows = w, h
		}
	}

	q := url.Values{}
	q.Set("cols", strconv.Itoa(cols))
	q.Set("rows", strconv.Itoa(rows))
	if container != "" {
		q.Set("container", container)
	}
	if cmd != "" {
		q.Set("cmd", cmd)
	}

	ws, resp, err := cl.DialWebSocket(path, q)
	if err != nil {
		if resp != nil && resp.StatusCode == 403 {
			return fmt.Errorf("forbidden: permission denied for terminal execution. Ensure your API token has 'Allow Container Exec' or 'Allow Server Exec' enabled")
		}
		if resp != nil && resp.StatusCode == 401 {
			return fmt.Errorf("unauthorized: please login with 'nd login'")
		}
		return fmt.Errorf("websocket connection failed: %w", err)
	}
	defer ws.Close()

	if isTerminal {
		oldState, terr := term.MakeRaw(stdinFd)
		if terr == nil {
			defer func() {
				_ = term.Restore(stdinFd, oldState)
				fmt.Println()
			}()
		}

		// Handle terminal resize dynamically via SIGWINCH
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGWINCH)
		defer signal.Stop(sigChan)

		go func() {
			for range sigChan {
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
		}()
	}

	done := make(chan struct{})

	// WebSocket -> Stdout
	go func() {
		defer close(done)
		for {
			mt, message, rerr := ws.ReadMessage()
			if rerr != nil {
				return
			}
			if mt == websocket.BinaryMessage || mt == websocket.TextMessage {
				_, _ = os.Stdout.Write(message)
			}
		}
	}()

	// Stdin -> WebSocket
	go func() {
		buf := make([]byte, 1024)
		for {
			n, rerr := os.Stdin.Read(buf)
			if n > 0 {
				if werr := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					return
				}
			}
			if rerr != nil {
				if rerr == io.EOF {
					_ = ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				}
				return
			}
		}
	}()

	<-done
	return nil
}
