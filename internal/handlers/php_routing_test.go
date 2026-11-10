package handlers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"panel/internal/db"

	"github.com/gofiber/fiber/v2"
)

func TestPHPRoutingRBACAndHtaccessScan(t *testing.T) {
	tmp := t.TempDir()
	store, err := db.Open(filepath.Join(tmp, "test_php_routing.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	ownerID, err := store.CreateUser(ctx, "alice", "hash1", db.RoleUser)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	owner, _ := store.GetUserByID(ctx, ownerID)
	viewerID, err := store.CreateUser(ctx, "bob", "hash2", db.RoleUser)
	if err != nil {
		t.Fatalf("create viewer: %v", err)
	}
	viewer, _ := store.GetUserByID(ctx, viewerID)
	outsiderID, err := store.CreateUser(ctx, "eve", "hash3", db.RoleUser)
	if err != nil {
		t.Fatalf("create outsider: %v", err)
	}
	outsider, _ := store.GetUserByID(ctx, outsiderID)

	appID := "site-alice"
	site := db.PHPSite{AppID: appID, OwnerID: owner.ID, UID: 10000, SiteType: "php", PHPVersion: "8.3"}
	if err := store.CreateAppAtomic(ctx, appID, "site-alice", owner.ID, 10, &site); err != nil {
		t.Fatalf("create php site: %v", err)
	}
	if err := store.AddCollaborator(ctx, appID, viewer.ID, db.CollabRoleViewer); err != nil {
		t.Fatalf("add collaborator: %v", err)
	}

	wsRoot := filepath.Join(tmp, "workspaces")
	htdocs := filepath.Join(wsRoot, appID, "htdocs")
	if err := os.MkdirAll(htdocs, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(htdocs, ".htaccess"), []byte("RewriteRule ^profile/([0-9]+)$ /profile.php?id=$1 [L]\n"), 0644); err != nil {
		t.Fatal(err)
	}

	p := &Panel{DB: store, WorkspacesRoot: wsRoot}
	var current db.User
	fiberApp := fiber.New()
	fiberApp.Use(func(c *fiber.Ctx) error {
		c.Locals(contextUserKey, current)
		return c.Next()
	})
	fiberApp.Get("/api/v1/php/sites/:id/routing", p.PHPRoutingGet)
	fiberApp.Post("/api/v1/php/sites/:id/routing/convert-htaccess", p.PHPRoutingConvertHtaccess)
	fiberApp.Post("/api/v1/php/sites/:id/routing/scan-htaccess", p.PHPRoutingScanHtaccess)
	fiberApp.Post("/api/v1/php/sites/:id/stop", p.PHPSiteStop)

	// 1. Outsider cannot view or scan
	current = outsider
	resp, _ := fiberApp.Test(httptest.NewRequest("GET", "/api/v1/php/sites/"+appID+"/routing", nil))
	if resp.StatusCode != 403 {
		t.Fatalf("outsider GET routing = %d, want 403", resp.StatusCode)
	}

	// 2. Viewer can view routing, but cannot mutate/scan/stop
	current = viewer
	resp, _ = fiberApp.Test(httptest.NewRequest("GET", "/api/v1/php/sites/"+appID+"/routing", nil))
	if resp.StatusCode != 200 {
		t.Fatalf("viewer GET routing = %d, want 200", resp.StatusCode)
	}
	resp, _ = fiberApp.Test(httptest.NewRequest("POST", "/api/v1/php/sites/"+appID+"/routing/scan-htaccess", strings.NewReader(`{"apply":false}`)))
	if resp.StatusCode != 403 {
		t.Fatalf("viewer POST scan-htaccess = %d, want 403", resp.StatusCode)
	}
	resp, _ = fiberApp.Test(httptest.NewRequest("POST", "/api/v1/php/sites/"+appID+"/stop", nil))
	if resp.StatusCode != 403 {
		t.Fatalf("viewer POST stop = %d, want 403", resp.StatusCode)
	}

	// 3. Owner can scan htdocs & convert pasted .htaccess
	current = owner
	req := httptest.NewRequest("POST", "/api/v1/php/sites/"+appID+"/routing/scan-htaccess", strings.NewReader(`{"apply":false}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err = fiberApp.Test(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("owner POST scan-htaccess = %v (status %d)", err, resp.StatusCode)
	}
	var scanOut struct {
		OK     bool `json:"ok"`
		Result struct {
			FilesScanned int `json:"files_scanned"`
			Rules        []struct {
				Pattern string `json:"pattern"`
				Target  string `json:"target"`
			} `json:"rules"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&scanOut); err != nil {
		t.Fatal(err)
	}
	if !scanOut.OK || scanOut.Result.FilesScanned != 1 || len(scanOut.Result.Rules) != 1 || scanOut.Result.Rules[0].Target != "/profile.php?id=$1" {
		t.Fatalf("unexpected scan response: %+v", scanOut)
	}

	req = httptest.NewRequest("POST", "/api/v1/php/sites/"+appID+"/routing/convert-htaccess", strings.NewReader(`{"content":"Redirect 301 /old /new"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err = fiberApp.Test(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("owner convert-htaccess = %v (status %d)", err, resp.StatusCode)
	}
}
