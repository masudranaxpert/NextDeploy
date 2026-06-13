package handlers

import (
	"errors"
	"time"

	"panel/internal/db"

	"github.com/fasthttp/websocket"
	fws "github.com/gofiber/contrib/websocket"
)

func (p *Panel) MonitorWebSocket(c *fws.Conn) {
	u, uOk := c.Locals(contextUserKey).(db.User)
	if !uOk || !p.monitorAuthOK(u) {
		_ = c.Close()
		return
	}

	dataTicker := time.NewTicker(2 * time.Second)
	authTicker := time.NewTicker(monitorAuthRecheck)
	defer dataTicker.Stop()
	defer authTicker.Stop()

	sendCached := func() error {
		body := p.monitorCacheBody()
		if len(body) == 0 {
			p.refreshMonitorSnapshot()
			body = p.monitorCacheBody()
		}
		if len(body) == 0 {
			return errors.New("monitor snapshot unavailable")
		}
		return c.WriteMessage(websocket.TextMessage, body)
	}

	if err := sendCached(); err != nil {
		return
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-done:
			return
		case <-authTicker.C:
			if !p.monitorAuthOK(u) {
				_ = c.Close()
				return
			}
		case <-dataTicker.C:
			if err := sendCached(); err != nil {
				return
			}
		}
	}
}
