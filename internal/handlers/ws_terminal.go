package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"panel/internal/db"
	"panel/internal/dockerapi"
	"panel/internal/phphosting"

	"github.com/creack/pty"
	"golang.org/x/crypto/ssh"

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
// It verifies the connection upgrade headers and validates Origin to prevent CSWSH.
func (p *Panel) WSUpgrade(c *fiber.Ctx) error {
	if !fws.IsWebSocketUpgrade(c) {
		return fiber.ErrUpgradeRequired
	}
	origin := strings.TrimSpace(c.Get("Origin"))
	if origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Hostname() == "" {
			return c.Status(fiber.StatusForbidden).SendString("forbidden: invalid origin")
		}
		originHost := u.Hostname()
		reqHost := c.Hostname()
		if h, _, err := net.SplitHostPort(reqHost); err == nil {
			reqHost = h
		}
		isLocal := (originHost == "localhost" || originHost == "127.0.0.1") && (reqHost == "localhost" || reqHost == "127.0.0.1")
		if !isLocal && !strings.EqualFold(originHost, reqHost) {
			var panelDomain string
			if p != nil && p.DB != nil {
				panelDomain = strings.TrimSpace(p.DB.GetSetting(c.UserContext(), "panel_domain"))
				if h, _, err := net.SplitHostPort(panelDomain); err == nil {
					panelDomain = h
				}
			}
			if panelDomain == "" || !strings.EqualFold(originHost, panelDomain) {
				return c.Status(fiber.StatusForbidden).SendString("forbidden: cross-origin websocket")
			}
		}
	}
	return c.Next()
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
	app, err := p.DB.GetApp(chkCtx, appID)
	if err != nil {
		_ = c.WriteMessage(websocket.TextMessage, []byte("app not found"))
		return
	}
	if u, uOk := c.Locals(contextUserKey).(db.User); uOk {
		allowed, aerr := p.CanAccessApp(chkCtx, u.ID, u.Role, app.ID, db.CollabRoleDeveloper)
		if aerr != nil || !allowed {
			_ = c.WriteMessage(websocket.TextMessage, []byte("forbidden: developer access required for terminal"))
			return
		}
	}
	var targetContainer string
	var phpPTY *dockerapi.PTYOptions
	if p.PHP != nil {
		if site, serr := p.DB.GetPHPSite(chkCtx, app.ID); serr == nil {
			if site.SiteType == "static" {
				_ = c.WriteMessage(websocket.TextMessage, []byte("static sites have no PHP runtime to open a shell in"))
				return
			}
			targetContainer = p.PHP.RuntimeName(site)
			phpPTY = &dockerapi.PTYOptions{
				User:       fmt.Sprintf("%d:%d", site.UID, site.UID),
				WorkingDir: p.PHP.DocRootFor(site),
				Env: append(phphosting.SiteEnv(site),
					"USER=site", "LOGNAME=site",
					`PS1=\[\033[01;32m\]site@`+app.Name+`\[\033[00m\]:\[\033[01;34m\]\w\[\033[00m\]\$ `),
			}
		}
	}
	if targetContainer == "" && container != "" && container != "all" {
		if p.ContainerBelongsToApp(chkCtx, app.ID, container) {
			targetContainer = container
		}
	}
	if targetContainer == "" {
		candidates := p.ComposeProjectCandidates(chkCtx, app, app.ID)
		for _, proj := range candidates {
			if container != "" && container != "all" {
				if cid, rerr := dockerapi.ContainerIDForComposeService(chkCtx, proj, container); rerr == nil && cid != "" {
					targetContainer = cid
					break
				}
			}
			if rows, rerr := dockerapi.ComposePS(chkCtx, proj); rerr == nil && len(rows) > 0 {
				for _, r := range rows {
					if container == "" || container == "all" || r.Service == container {
						targetContainer = r.Name
						break
					}
				}
				if targetContainer != "" {
					break
				}
			}
		}
	}
	if targetContainer == "" {
		_ = c.WriteMessage(websocket.TextMessage, []byte("no active container found for this app"))
		return
	}
	cols := parseDim(c.Query("cols"), 80)
	rows := parseDim(c.Query("rows"), 24)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var sess *dockerapi.PTYExecSession
	if phpPTY != nil {
		sess, err = dockerapi.ExecPTYWithOptions(ctx, targetContainer, []string{"/bin/bash"}, rows, cols, *phpPTY)
	} else {
		sess, err = dockerapi.ExecPTYAutoShell(ctx, targetContainer, rows, cols)
	}
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
			if len(msg) > 0 && msg[0] == '{' && json.Unmarshal(msg, &m) == nil && m.Op == "resize" && m.Cols > 0 && m.Rows > 0 {
				if rerr := sess.Resize(ctx, m.Rows, m.Cols); rerr != nil {
					log.Printf("terminal resize: %v", rerr)
				}
				continue
			}
		}
		if (mt == websocket.BinaryMessage || mt == websocket.TextMessage) && len(msg) > 0 {
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

	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "/data"
	}

	// Attempt industry-standard SSH to host first (Dokploy/Coolify approach)
	if signer, err := EnsureLocalHostSSH(dataDir); err == nil {
		if sshClient, err := DialHostSSH(signer); err == nil {
			defer sshClient.Close()
			session, err := sshClient.NewSession()
			if err == nil {
				defer session.Close()

				modes := ssh.TerminalModes{
					ssh.ECHO:          1,
					ssh.TTY_OP_ISPEED: 14400,
					ssh.TTY_OP_OSPEED: 14400,
				}
				if err := session.RequestPty("xterm-256color", int(rows), int(cols), modes); err == nil {
					stdinPipe, _ := session.StdinPipe()
					stdoutPipe, _ := session.StdoutPipe()
					stderrPipe, _ := session.StderrPipe()

					if err := session.Shell(); err == nil {
						sshDone := make(chan struct{})
						defer close(sshDone)

						var wg sync.WaitGroup
						wg.Add(1)
						go func() {
							defer wg.Done()
							buf := make([]byte, 32*1024)
							r := io.MultiReader(stdoutPipe, stderrPipe)
							for {
								n, err := r.Read(buf)
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
								break
							}
							if mt == websocket.TextMessage {
								var m termClientMsg
								if len(msg) > 0 && msg[0] == '{' && json.Unmarshal(msg, &m) == nil && m.Op == "resize" && m.Cols > 0 && m.Rows > 0 {
									_ = session.WindowChange(int(m.Rows), int(m.Cols))
									continue
								}
							}
							if (mt == websocket.BinaryMessage || mt == websocket.TextMessage) && len(msg) > 0 {
								if _, werr := stdinPipe.Write(msg); werr != nil {
									break
								}
							}
						}
						_ = session.Close()
						_ = sshClient.Close()
						wg.Wait()
						return
					}
				}
			}
		}
	}

	// High-performance fallback: use NextDeploy self-image with nsenter (Zero external image pull)
	cmd := exec.Command("docker", "run", "--rm", "-it", "--privileged", "--pid=host", "--entrypoint", "nsenter", "mrformula/nextdeploy:latest", "-t", "1", "-m", "-u", "-n", "-i", "su", "-")
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
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
			if len(msg) > 0 && msg[0] == '{' && json.Unmarshal(msg, &m) == nil && m.Op == "resize" && m.Cols > 0 && m.Rows > 0 {
				if err := pty.Setsize(ptmx, &pty.Winsize{
					Rows: uint16(m.Rows),
					Cols: uint16(m.Cols),
				}); err != nil {
					log.Printf("vps terminal resize: %v", err)
				}
				continue
			}
		}
		if (mt == websocket.BinaryMessage || mt == websocket.TextMessage) && len(msg) > 0 {
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
