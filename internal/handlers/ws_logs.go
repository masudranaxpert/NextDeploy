package handlers

import (
	"context"
	"io"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"panel/internal/db"
	"panel/internal/dockerapi"
	"panel/internal/logview"

	"github.com/fasthttp/websocket"
	fws "github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
)

// AppLogWebSocket streams live container logs over the Docker Engine API (follow + stdcopy demux),
// avoiding docker CLI / compose subprocess overhead (similar goal to Dokploy's direct docker logs --follow).
func (p *Panel) AppLogWebSocket(c *fws.Conn) {
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

	tail := strings.TrimSpace(c.Query("tail"))
	if tail == "" {
		tail = "200"
	}
	since := strings.TrimSpace(c.Query("since"))

	var logRef string
	// 1. If container is explicitly provided and belongs to the app:
	if container != "" && container != "all" {
		if p.ContainerBelongsToApp(chkCtx, app.ID, container) {
			logRef = container
		}
	}

	// 2. Search project candidates (active slot project first, then canonical)
	if logRef == "" {
		candidates := p.ComposeProjectCandidates(chkCtx, app, app.ID)
		for _, proj := range candidates {
			if container != "" && container != "all" {
				if cid, rerr := dockerapi.ContainerIDForComposeService(chkCtx, proj, container); rerr == nil && cid != "" {
					logRef = cid
					break
				}
			}
			// If looking for any/default container
			if rows, rerr := dockerapi.ComposePS(chkCtx, proj); rerr == nil && len(rows) > 0 {
				for _, r := range rows {
					if container == "" || container == "all" || r.Service == container {
						logRef = r.Name
						break
					}
				}
				if logRef != "" {
					break
				}
			}
		}
	}

	// 3. Fallback to compose rows probe
	if logRef == "" {
		project, composeRows, composeRes := p.ComposeProjectAndPS(chkCtx, app, app.ID)
		if composeRes.OK && len(composeRows) > 0 {
			for _, r := range composeRows {
				if container == "" || container == "all" || r.Service == container {
					if cid, rerr := dockerapi.ContainerIDForComposeService(chkCtx, project, r.Service); rerr == nil && cid != "" {
						logRef = cid
					} else {
						logRef = r.Name
					}
					break
				}
			}
		}
	}

	if logRef == "" {
		_ = c.WriteMessage(websocket.TextMessage, []byte("no running container found for app"))
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logReader, err := dockerapi.FollowContainerLogsDemuxed(ctx, logRef, tail, since)
	if err != nil {
		_ = c.WriteMessage(websocket.TextMessage, []byte("docker logs: "+err.Error()))
		return
	}
	defer logReader.Close()

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

	var writeMu sync.Mutex
	writeChunk := func(payload []byte) error {
		if len(payload) == 0 {
			return nil
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		return c.WriteMessage(websocket.TextMessage, payload)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var ansiAccum []byte
		buf := make([]byte, 32*1024)
		for {
			n, rerr := logReader.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				cleaned, newAccum := logview.StripDockerLogChunk(ansiAccum, chunk)
				ansiAccum = newAccum
				if len(cleaned) > 0 {
					if werr := writeChunk(cleaned); werr != nil {
						cancel()
						return
					}
				}
			}
			if rerr != nil {
				if rerr != io.EOF && ctx.Err() == nil {
					log.Printf("log websocket read error: %v", rerr)
				}
				if flush := logview.FlushDockerLogAccum(ansiAccum); len(flush) > 0 {
					_ = writeChunk(flush)
				}
				return
			}
		}
	}()

	for {
		if _, _, err := c.ReadMessage(); err != nil {
			cancel()
			break
		}
	}
	_ = logReader.Close()
	wg.Wait()
}

// AppLogsREST returns formatted recent log entries for an app or container, supporting ?since=15m|1h|24h&tail=200.
func (p *Panel) AppLogsREST(c *fiber.Ctx) error {
	appID := strings.TrimSpace(c.Params("id"))
	if appID == "" {
		return c.Status(400).JSON(fiber.Map{"ok": false, "error": "app id required"})
	}
	chkCtx, chkCancel := context.WithTimeout(c.UserContext(), 15*time.Second)
	defer chkCancel()
	app, err := p.DB.GetApp(chkCtx, appID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"ok": false, "error": "app not found"})
	}

	container := strings.TrimPrefix(strings.TrimSpace(c.Query("container")), "/")
	tailStr := strings.TrimSpace(c.Query("tail"))
	tailLines := 200
	if n, err := strconv.Atoi(tailStr); err == nil && n > 0 {
		tailLines = n
	}
	since := strings.TrimSpace(c.Query("since"))

	var logRef string
	if container != "" && container != "all" {
		if p.ContainerBelongsToApp(chkCtx, app.ID, container) {
			logRef = container
		}
	}
	if logRef == "" {
		candidates := p.ComposeProjectCandidates(chkCtx, app, app.ID)
		for _, proj := range candidates {
			if rows, rerr := dockerapi.ComposePS(chkCtx, proj); rerr == nil && len(rows) > 0 {
				for _, r := range rows {
					if container == "" || container == "all" || r.Service == container {
						logRef = r.Name
						break
					}
				}
				if logRef != "" {
					break
				}
			}
		}
	}

	if logRef == "" {
		return c.JSON(fiber.Map{"ok": true, "logs": []string{}})
	}

	text, err := dockerapi.FetchContainerLogsText(chkCtx, logRef, tailLines, since)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}

	lines := strings.Split(text, "\n")
	res := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			res = append(res, l)
		}
	}
	return c.JSON(fiber.Map{"ok": true, "logs": res, "container": logRef})
}
