package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"strconv"
	"strings"
	"sync"

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

// TerminalWSUpgrade must run before the websocket route so only WebSocket clients hit the handler.
func (p *Panel) TerminalWSUpgrade(c *fiber.Ctx) error {
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
	appID := c.Params("id")
	container := strings.TrimPrefix(strings.TrimSpace(c.Query("container")), "/")
	if _, err := p.DB.GetApp(context.Background(), appID); err != nil {
		_ = c.WriteMessage(websocket.TextMessage, []byte("app not found"))
		return
	}
	if container == "" || !p.containerBelongsToApp(appID, container) {
		_ = c.WriteMessage(websocket.TextMessage, []byte("invalid container for this app"))
		return
	}
	cols := parseDim(c.Query("cols"), 80)
	rows := parseDim(c.Query("rows"), 24)

	ctx := context.Background()
	sess, err := dockerapi.ExecPTY(ctx, container, nil, rows, cols)
	if err != nil {
		log.Printf("terminal exec: %v", err)
		_ = c.WriteMessage(websocket.TextMessage, []byte("could not start shell: "+err.Error()))
		return
	}
	defer sess.Close()

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
				}
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
			if json.Unmarshal(msg, &m) == nil && m.Op == "resize" && m.Cols > 0 && m.Rows > 0 {
				if rerr := sess.Resize(ctx, m.Rows, m.Cols); rerr != nil {
					log.Printf("terminal resize: %v", rerr)
				}
				continue
			}
		}
		if mt == websocket.BinaryMessage && len(msg) > 0 {
			if _, werr := hij.Conn.Write(msg); werr != nil {
				break
			}
		}
	}
	_ = hij.CloseWrite()
	wg.Wait()
}
