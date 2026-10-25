package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"panel/internal/db"
	"panel/internal/workspace"

	"github.com/gofiber/fiber/v2"
)

func TestStorageOversellingSettingsAndStrictMode(t *testing.T) {
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	adminID, err := store.CreateUser(ctx, "admin", "hash", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	user1ID, err := store.CreateUser(ctx, "user1", "hash", db.RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	user2ID, err := store.CreateUser(ctx, "user2", "hash", db.RoleUser)
	if err != nil {
		t.Fatal(err)
	}

	p := &Panel{DB: store, Store: workspace.NewStore(dir), WorkspacesRoot: dir, cgroupChecked: true, cgroupModeVal: ""}

	// Default: Overselling should be enabled (thin provisioning)
	if !p.IsStorageOversellingEnabled(ctx) {
		t.Fatal("expected storage overselling to be enabled by default")
	}

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		// Mock current user as admin
		c.Locals(contextUserKey, db.User{ID: adminID, Username: "admin", Role: db.RoleAdmin})
		return c.Next()
	})
	app.Post("/settings", p.SettingsSave)
	app.Post("/users/:id/limits", p.UserChangeLimits)

	// Set user1 to 20 GB (20480 MB) and user2 to 15 GB (15360 MB)
	_ = store.UpdateUserLimits(ctx, user1ID, 10, 1024, 1.0, 20480, 0, true)
	_ = store.UpdateUserLimits(ctx, user2ID, 10, 1024, 1.0, 15360, 0, true)
	// Total user storage = 35840 MB (35 GB)

	// Case 1: Try to disable overselling when host capacity is 30 GB (30720 MB).
	// Total assigned (35 GB) > host disk (30 GB), so toggle must be REJECTED with 400 and overcommitted: true.
	body, _ := json.Marshal(map[string]interface{}{
		"storage_overselling_enabled": false,
	})
	req := httptest.NewRequest(http.MethodPost, "/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatal(err)
	}

	// We expect 400 Bad Request if host capacity < 35 GB
	hostMB := p.HostDiskCapacityMB(ctx)
	if 35840 > hostMB {
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request when overcommitted, got %d", resp.StatusCode)
		}
		var res map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&res)
		if res["overcommitted"] != true {
			t.Fatalf("expected overcommitted: true in response, got %v", res)
		}
	}

	// Case 2: Reduce user quotas so total is well within host capacity (e.g. 500 MB each)
	_ = store.UpdateUserLimits(ctx, user1ID, 10, 1024, 1.0, 500, 0, true)
	_ = store.UpdateUserLimits(ctx, user2ID, 10, 1024, 1.0, 500, 0, true)

	// Now turning overselling OFF should succeed
	req2 := httptest.NewRequest(http.MethodPost, "/settings", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Accept", "application/json")
	resp2, err := app.Test(req2, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK when turning off overselling within capacity, got %d", resp2.StatusCode)
	}
	if p.IsStorageOversellingEnabled(ctx) {
		t.Fatal("expected storage overselling to be disabled")
	}

	// Case 3: In Strict Mode, assigning 0 (unlimited) must be rejected
	limitBody0, _ := json.Marshal(map[string]interface{}{
		"max_apps":       10,
		"max_memory_mb":  512,
		"max_cpus":       1.0,
		"max_storage_mb": 0, // unlimited
	})
	reqLimit0 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/users/%d/limits", user1ID), bytes.NewReader(limitBody0))
	reqLimit0.Header.Set("Content-Type", "application/json")
	reqLimit0.Header.Set("Accept", "application/json")
	respLimit0, err := app.Test(reqLimit0, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if respLimit0.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for unlimited storage in strict mode, got %d", respLimit0.StatusCode)
	}

	// Case 4: In Strict Mode, assigning a quota that exceeds host disk capacity must be rejected
	limitBodyHuge, _ := json.Marshal(map[string]interface{}{
		"max_apps":       10,
		"max_memory_mb":  512,
		"max_cpus":       1.0,
		"max_storage_mb": hostMB + 1000,
	})
	reqLimitHuge := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/users/%d/limits", user1ID), bytes.NewReader(limitBodyHuge))
	reqLimitHuge.Header.Set("Content-Type", "application/json")
	reqLimitHuge.Header.Set("Accept", "application/json")
	respLimitHuge, err := app.Test(reqLimitHuge, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if respLimitHuge.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request when quota exceeds host capacity in strict mode, got %d", respLimitHuge.StatusCode)
	}
}
