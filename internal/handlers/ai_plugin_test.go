package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"panel/internal/agent"
	"panel/internal/db"

	"github.com/gofiber/fiber/v2"
)

func TestCopilotRoutesAnswerClearlyWhileAIIsOff(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_ = store.SetSetting(context.Background(), agent.SettingAIEnabled, "0")
	p := &Panel{DB: store, Copilot: agent.NewManager(store, nil, t.TempDir())}
	p.Copilot.SetDataDir(t.TempDir())

	app := fiber.New()
	app.Get("/api/v1/copilot/status", p.RequireAIMiddleware, p.CopilotStatus)
	app.Get("/api/v1/ai/plugin", p.AIPluginStatus)

	resp, err := app.Test(httptest.NewRequest("GET", "/api/v1/copilot/status", nil))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	var out struct {
		Error       string `json:"error"`
		AIInstalled bool   `json:"ai_installed"`
	}
	_ = json.Unmarshal(body, &out)
	if resp.StatusCode != fiber.StatusConflict || out.AIInstalled || out.Error != agent.ErrAINotInstalled.Error() {
		t.Fatalf("status %d body %s", resp.StatusCode, body)
	}

	resp, _ = app.Test(httptest.NewRequest("GET", "/api/v1/ai/plugin", nil))
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("plugin status must work while AI is off, got %d", resp.StatusCode)
	}
}
