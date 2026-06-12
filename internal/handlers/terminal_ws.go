package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"panel/internal/db"
	"panel/internal/dockerapi"

	"github.com/fasthttp/websocket"
	fws "github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
)

type termClientMsg struct {
	Op   string `json:"op"`
	Cols uint   `json:"cols"`
	Rows uint   `json:"rows"`
}

// WSUpgrade is a reusable WebSocket upgrade guard for all WS routes.
func (p *Panel) WSUpgrade(c *fiber.Ctx) error {
	if fws.IsWebSocketUpgrade(c) {
		return c.Next()
	}
	return fiber.ErrUpgradeRequired
}

func parseDim(q string, def uint) uint {
	q = strings.TrimSpace(q)
	if q == "" {
		return def
	}
	n, err := strconv.ParseUint(q, 10, 32)
	if err != nil || n == 0 {
		return def
	}
	return uint(n)
}

// TerminalWebSocket streams a Docker exec TTY to the browser (xterm.js).
func (p *Panel) TerminalWebSocket(c *fws.Conn) {
	appID := strings.TrimSpace(c.Params("id"))
	if appID == "" {
		appID = strings.TrimSpace(c.Query("app"))
	}
	container := strings.TrimPrefix(strings.TrimSpace(c.Query("container")), "/")
	if appID == "" {
		_ = c.WriteMessage(websocket.TextMessage, []byte("missing app id (route or ?app=)"))
		return
	}
	chkCtx, chkCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer chkCancel()
	if _, err := p.DB.GetApp(chkCtx, appID); err != nil {
		_ = c.WriteMessage(websocket.TextMessage, []byte("app not found"))
		return
	}
	if container == "" || !p.containerBelongsToApp(chkCtx, appID, container) {
		_ = c.WriteMessage(websocket.TextMessage, []byte("invalid container for this app"))
		return
	}
	cols := parseDim(c.Query("cols"), 80)
	rows := parseDim(c.Query("rows"), 24)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sess, err := dockerapi.ExecPTYAutoShell(ctx, container, rows, cols)
	if err != nil {
		log.Printf("terminal exec: %v", err)
		_ = c.WriteMessage(websocket.TextMessage, []byte("could not start shell: "+err.Error()))
		return
	}
	defer sess.Close()

	go func() {
		tick := time.NewTicker(10 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				u, uOk := c.Locals(contextUserKey).(db.User)
				if uOk {
					dbUser, err := p.DB.GetUserByID(ctx, u.ID)
					if err != nil || dbUser.Status == db.UserStatusSuspended {
						cancel()
						_ = c.Close()
						return
					}
				}
				if err := c.WriteControl(websocket.PingMessage, nil, time.Now().Add(8*time.Second)); err != nil {
					return
				}
			}
		}
	}()

	hij := sess.Hijack
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := hij.Reader.Read(buf)
			if n > 0 {
				payload := make([]byte, n)
				copy(payload, buf[:n])
				if werr := c.WriteMessage(websocket.BinaryMessage, payload); werr != nil {
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					log.Printf("terminal read docker: %v", err)
				} else {
					_ = c.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[33m[session ended — shell exited or stream closed]\x1b[0m\r\n"))
				}
				_ = c.Close()
				return
			}
		}
	}()

	for {
		mt, msg, err := c.ReadMessage()
		if err != nil {
			cancel()
			break
		}
		if mt == websocket.TextMessage {
			var m termClientMsg
			if json.Unmarshal(msg, &m) == nil && m.Op == "resize" && m.Cols > 0 && m.Rows > 0 {
				if rerr := sess.Resize(ctx, m.Rows, m.Cols); rerr != nil {
					log.Printf("terminal resize: %v", rerr)
				}
				continue
			}
		}
		if mt == websocket.BinaryMessage && len(msg) > 0 {
			if _, werr := hij.Conn.Write(msg); werr != nil {
				cancel()
				break
			}
		}
	}
	cancel()
	_ = hij.CloseWrite()
	_ = sess.Close()
	wg.Wait()
}

// VPSTerminalPage renders the standalone VPS terminal page.
func (p *Panel) VPSTerminalPage(c *fiber.Ctx) error {
	u, ok := currentUser(c)
	if !ok || u.Role != db.RoleAdmin {
		return c.Status(fiber.StatusForbidden).SendString("forbidden")
	}
	return c.Render("pages/vps_terminal", WithUser(c, fiber.Map{
		"Nav":   "terminal",
		"Title": "Server Terminal",
	}), "layouts/shell")
}

// VPSTerminalWebSocket streams a local shell (/bin/sh) to the browser.
// This runs inside the panel container, giving full Docker CLI access.
func (p *Panel) VPSTerminalWebSocket(c *fws.Conn) {
	u, ok := c.Locals(contextUserKey).(db.User)
	if !ok || u.Role != db.RoleAdmin {
		_ = c.WriteMessage(websocket.TextMessage, []byte("forbidden"))
		_ = c.Close()
		return
	}
	cols := parseDim(c.Query("cols"), 80)
	rows := parseDim(c.Query("rows"), 24)

	shell := "/bin/sh"
	if sh := os.Getenv("SHELL"); sh != "" {
		shell = sh
	}
	// Try bash first, fall back to sh
	if _, err := exec.LookPath("bash"); err == nil {
		shell = "bash"
	}

	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"PS1=panel:\\w\\$ ",
	)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	})
	if err != nil {
		_ = c.WriteMessage(websocket.TextMessage, []byte("could not start shell: "+err.Error()))
		return
	}
	defer ptmx.Close()
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	vpsCtx, vpsCancel := context.WithCancel(context.Background())
	defer vpsCancel()
	go func() {
		tick := time.NewTicker(10 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-vpsCtx.Done():
				return
			case <-tick.C:
				u, uOk := c.Locals(contextUserKey).(db.User)
				if uOk {
					dbUser, err := p.DB.GetUserByID(vpsCtx, u.ID)
					if err != nil || dbUser.Status == db.UserStatusSuspended || dbUser.Role != db.RoleAdmin {
						vpsCancel()
						_ = c.Close()
						return
					}
				}
				if err := c.WriteControl(websocket.PingMessage, nil, time.Now().Add(8*time.Second)); err != nil {
					return
				}
			}
		}
	}()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				payload := make([]byte, n)
				copy(payload, buf[:n])
				if werr := c.WriteMessage(websocket.BinaryMessage, payload); werr != nil {
					return
				}
			}
			if err != nil {
				_ = c.Close()
				return
			}
		}
	}()

	for {
		mt, msg, err := c.ReadMessage()
		if err != nil {
			vpsCancel()
			break
		}
		if mt == websocket.TextMessage {
			var m termClientMsg
			if json.Unmarshal(msg, &m) == nil && m.Op == "resize" && m.Cols > 0 && m.Rows > 0 {
				if err := pty.Setsize(ptmx, &pty.Winsize{
					Rows: uint16(m.Rows),
					Cols: uint16(m.Cols),
				}); err != nil {
					log.Printf("vps terminal resize: %v", err)
				}
				continue
			}
		}
		if mt == websocket.BinaryMessage && len(msg) > 0 {
			if _, werr := ptmx.Write(msg); werr != nil {
				vpsCancel()
				break
			}
		}
	}
	vpsCancel()
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	_ = ptmx.Close()
	wg.Wait()
}
