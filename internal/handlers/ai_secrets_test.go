package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"panel/internal/db"

	"github.com/gofiber/fiber/v2"
)

func TestCopilotSecrets_CRUD(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "secrets_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := db.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open failed: %v", err)
	}
	defer store.Close()

	p := &Panel{DB: store}

	app := fiber.New()
	app.Get("/api/v1/copilot/secrets", p.CopilotGetSecrets)
	app.Post("/api/v1/copilot/secrets", p.CopilotSaveSecret)
	app.Delete("/api/v1/copilot/secrets/:name", p.CopilotDeleteSecret)

	// 1. Initially empty
	req := httptest.NewRequest("GET", "/api/v1/copilot/secrets", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("GET /api/v1/copilot/secrets failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var listResp struct {
		Secrets []CopilotSecretItem `json:"secrets"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&listResp)
	if len(listResp.Secrets) != 0 {
		t.Fatalf("expected 0 secrets, got %d", len(listResp.Secrets))
	}

	// 2. Add invalid secret name
	badPayload := bytes.NewBufferString(`{"name":"invalid-name!","value":"val"}`)
	req = httptest.NewRequest("POST", "/api/v1/copilot/secrets", badPayload)
	req.Header.Set("Content-Type", "application/json")
	resp, _ = app.Test(req)
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("expected 400 for bad name, got %d", resp.StatusCode)
	}

	// 3. Add valid secret
	payload := bytes.NewBufferString(`{"name":"DB_PASSWORD","value":"supersecretpass123"}`)
	req = httptest.NewRequest("POST", "/api/v1/copilot/secrets", payload)
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for valid secret, got %d (err: %v)", resp.StatusCode, err)
	}

	// 4. Verify list contains masked secret and value
	req = httptest.NewRequest("GET", "/api/v1/copilot/secrets", nil)
	resp, _ = app.Test(req)
	_ = json.NewDecoder(resp.Body).Decode(&listResp)
	if len(listResp.Secrets) != 1 {
		t.Fatalf("expected 1 secret, got %d", len(listResp.Secrets))
	}
	item := listResp.Secrets[0]
	if item.Name != "DB_PASSWORD" {
		t.Errorf("expected DB_PASSWORD, got %s", item.Name)
	}
	if item.Value != "supersecretpass123" {
		t.Errorf("expected plaintext value, got %s", item.Value)
	}
	if item.Length != len("supersecretpass123") {
		t.Errorf("expected length %d, got %d", len("supersecretpass123"), item.Length)
	}
	if item.Masked == item.Value || item.Masked == "" {
		t.Errorf("expected masked representation, got %s", item.Masked)
	}

	// 5. Delete secret
	req = httptest.NewRequest("DELETE", "/api/v1/copilot/secrets/DB_PASSWORD", nil)
	resp, _ = app.Test(req)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 on delete, got %d", resp.StatusCode)
	}

	// 6. Verify removed
	if v := store.GetSetting(context.Background(), "ai_secret_DB_PASSWORD"); v != "" {
		t.Errorf("expected setting to be deleted from db, got %s", v)
	}
}
