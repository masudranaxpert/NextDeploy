package handlers

import (
	"context"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"panel/internal/dockerapi"
	"panel/internal/logview"

	"github.com/fasthttp/websocket"
	fws "github.com/gofiber/contrib/websocket"
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
	byService := p.composeServiceBelongsToApp(chkCtx, appID, container)
	if container == "" || (!byService && !p.containerBelongsToApp(chkCtx, appID, container)) {
		_ = c.WriteMessage(websocket.TextMessage, []byte("invalid container for this app"))
		return
	}

	var logRef string
	if byService {
		project := p.activeComposeProjectName(chkCtx, app, appID)
		cid, rerr := dockerapi.ContainerIDForComposeService(chkCtx, project, container)
		if rerr != nil {
			_ = c.WriteMessage(websocket.TextMessage, []byte("compose logs: "+rerr.Error()))
			return
		}
		logRef = cid
	} else {
		logRef = container
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logReader, err := dockerapi.FollowContainerLogsDemuxed(ctx, logRef, "0")
	if err != nil {
		_ = c.WriteMessage(websocket.TextMessage, []byte("docker logs: "+err.Error()))
		return
	}
	defer logReader.Close()

	go func() {
		tick := time.NewTicker(45 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
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
	wg.Wait()
}
