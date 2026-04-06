package handlers

import (
	"context"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync"

	"github.com/fasthttp/websocket"
	fws "github.com/gofiber/contrib/websocket"
)

// AppLogWebSocket streams `docker logs -f` output to the browser.
// The initial history is still loaded by the normal partial; this stream only appends new output.
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
	if _, err := p.DB.GetApp(context.Background(), appID); err != nil {
		_ = c.WriteMessage(websocket.TextMessage, []byte("app not found"))
		return
	}
	if container == "" || !p.containerBelongsToApp(appID, container) {
		_ = c.WriteMessage(websocket.TextMessage, []byte("invalid container for this app"))
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "logs", "-f", "--tail", "0", container)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = c.WriteMessage(websocket.TextMessage, []byte("stdout pipe error: "+err.Error()))
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = c.WriteMessage(websocket.TextMessage, []byte("stderr pipe error: "+err.Error()))
		return
	}
	if err := cmd.Start(); err != nil {
		_ = c.WriteMessage(websocket.TextMessage, []byte("could not start docker logs: "+err.Error()))
		return
	}
	defer func() {
		cancel()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
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
	streamReader := func(r io.Reader) {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				if werr := writeChunk(chunk); werr != nil {
					cancel()
					return
				}
			}
			if err != nil {
				if err != io.EOF && ctx.Err() == nil {
					log.Printf("log websocket read error: %v", err)
				}
				return
			}
		}
	}

	wg.Add(2)
	go streamReader(stdout)
	go streamReader(stderr)

	// Read/discard messages so the connection closes promptly when the client goes away.
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			cancel()
			break
		}
	}
	wg.Wait()
}
