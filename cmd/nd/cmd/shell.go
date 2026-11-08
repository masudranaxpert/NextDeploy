package cmd

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

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

func isPTYError(text string) bool {
	lower := strings.ToLower(text)
	return strings.HasPrefix(lower, "forbidden") ||
		strings.HasPrefix(lower, "unauthorized") ||
		strings.Contains(lower, "not found") ||
		strings.Contains(lower, "no active container") ||
		strings.Contains(lower, "could not start shell") ||
		strings.Contains(lower, "permission denied") ||
		strings.Contains(lower, "static sites") ||
		strings.Contains(lower, "missing app")
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

	// Detect error frames or early closures sent before entering raw terminal mode.
	var initialData []byte
	var initialType int
	hasInitial := false

	_ = ws.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	firstType, firstData, firstErr := ws.ReadMessage()
	_ = ws.SetReadDeadline(time.Time{})

	if firstErr != nil {
		var netErr net.Error
		if errors.As(firstErr, &netErr) && netErr.Timeout() {
			// Normal timeout: container shell started silently waiting for stdin
		} else {
			// Connection closed or broken before shell session started
			return fmt.Errorf("terminal session ended immediately: %w", firstErr)
		}
	} else {
		msgStr := strings.TrimSpace(string(firstData))
		if firstType == websocket.TextMessage && isPTYError(msgStr) {
			return fmt.Errorf("%s", msgStr)
		}
		initialData = firstData
		initialType = firstType
		hasInitial = true
	}

	if isTerminal {
		oldState, terr := term.MakeRaw(stdinFd)
		if terr == nil {
			defer func() {
				_ = term.Restore(stdinFd, oldState)
				fmt.Println()
			}()
		}

		stopResize := monitorTerminalResize(stdinFd, ws)
		defer stopResize()
	}

	if hasInitial && (initialType == websocket.BinaryMessage || initialType == websocket.TextMessage) {
		_, _ = os.Stdout.Write(initialData)
	}

	var sessionErr error
	var errOnce sync.Once
	setErr := func(err error) {
		errOnce.Do(func() {
			sessionErr = err
		})
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
			if mt == websocket.TextMessage {
				msgStr := strings.TrimSpace(string(message))
				if isPTYError(msgStr) {
					setErr(fmt.Errorf("%s", msgStr))
				}
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
	return sessionErr
}
