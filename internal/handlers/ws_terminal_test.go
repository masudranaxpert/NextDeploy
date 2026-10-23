package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestWSUpgrade_OriginValidation(t *testing.T) {
	app := fiber.New()
	p := &Panel{}

	app.Use("/ws", p.WSUpgrade)
	app.Get("/ws", func(c *fiber.Ctx) error {
		return c.SendString("upgraded")
	})

	tests := []struct {
		name       string
		headers    map[string]string
		wantStatus int
	}{
		{
			name: "non-websocket request returns upgrade required",
			headers: map[string]string{
				"Host": "panel.example.com",
			},
			wantStatus: http.StatusUpgradeRequired,
		},
		{
			name: "same-origin websocket is allowed",
			headers: map[string]string{
				"Host":       "panel.example.com",
				"Connection": "Upgrade",
				"Upgrade":    "websocket",
				"Origin":     "https://panel.example.com",
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "cross-origin websocket is rejected",
			headers: map[string]string{
				"Host":       "panel.example.com",
				"Connection": "Upgrade",
				"Upgrade":    "websocket",
				"Origin":     "https://evil.com",
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name: "no-origin websocket from cli is allowed",
			headers: map[string]string{
				"Host":       "panel.example.com",
				"Connection": "Upgrade",
				"Upgrade":    "websocket",
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "invalid origin format is rejected",
			headers: map[string]string{
				"Host":       "panel.example.com",
				"Connection": "Upgrade",
				"Upgrade":    "websocket",
				"Origin":     "://invalid-url",
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name: "local host to 127.0.0.1 is allowed",
			headers: map[string]string{
				"Host":       "localhost:3000",
				"Connection": "Upgrade",
				"Upgrade":    "websocket",
				"Origin":     "http://127.0.0.1:3000",
			},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/ws", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			if h, ok := tt.headers["Host"]; ok {
				req.Host = h
			}
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			if resp.StatusCode != tt.wantStatus {
				t.Errorf("got status %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}
