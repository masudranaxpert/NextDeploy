package handlers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"panel/internal/db"
	"panel/internal/dockerapi"
	"panel/internal/workspace"
)

func TestReconcileAllAppSlotComposeAndRootCaddySync(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := db.Open(filepath.Join(tmpDir, "panel.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()

	wsRoot := filepath.Join(tmpDir, "workspaces")
	wsStore := workspace.NewStore(wsRoot)

	stackComposePath := filepath.Join(tmpDir, "docker-compose.yml")
	baseStackYAML := `services:
  caddy:
    image: lucaslorentz/caddy-docker-proxy:ci-alpine
    ports:
      - "80:80"
      - "443:443"
      - "443:443/udp"
  panel:
    image: masudranaxpert/nextdeploy:latest
`
	if err := os.WriteFile(stackComposePath, []byte(baseStackYAML), 0640); err != nil {
		t.Fatalf("write root compose: %v", err)
	}
	t.Setenv("PANEL_STACK_COMPOSE_FILE", stackComposePath)

	uid, err := store.CreateUser(ctx, "admin", "hash", db.RoleAdmin)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// App A (stopped): alphabetically/ID first, tries to claim 12002
	if err := store.CreateApp(ctx, "app-a-stopped", "App Stopped", uid); err != nil {
		t.Fatalf("create stopped app: %v", err)
	}
	dirA := wsStore.Path("app-a-stopped")
	if err := os.MkdirAll(dirA, 0750); err != nil {
		t.Fatalf("ensure dirA: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirA, "docker-compose.yml"), []byte(`services:
  web:
    image: nginx:alpine
    ports:
      - "12002:80"
`), 0640); err != nil {
		t.Fatalf("write composeA: %v", err)
	}
	// Seed pre-upgrade stolen allocation held by stopped app-a-stopped
	if err := store.AllocatePort(ctx, 12002, "0.0.0.0", "app-a-stopped", "web", "tcp"); err != nil {
		t.Fatalf("seed stolen port: %v", err)
	}

	// App B (running): uses .env APP_PORT=18080 (default would hit reserved 8080), 12002 web, and 54325 postgres
	if err := store.CreateApp(ctx, "app-b-running", "App Running", uid); err != nil {
		t.Fatalf("create running app: %v", err)
	}
	dirB := wsStore.Path("app-b-running")
	if err := os.MkdirAll(dirB, 0750); err != nil {
		t.Fatalf("ensure dirB: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirB, ".env"), []byte("APP_PORT=18080\n"), 0640); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirB, "docker-compose.yml"), []byte(`services:
  web:
    image: nginx:alpine
    ports:
      - "12002:80"
      - "${APP_PORT:-8080}:8080"
  db:
    image: postgres:16
    ports:
      - "54325:5432"
`), 0640); err != nil {
		t.Fatalf("write composeB: %v", err)
	}

	var applied [][]string
	// Phase 1: Legacy state — app-b-running holds 12002 directly on host (not via Caddy yet)
	phase1Live := &dockerapi.LivePortState{
		DirectHostPorts:   map[int]bool{12002: true, 18080: true, 54325: true},
		CaddyGatewayPorts: map[int]bool{},
		CaddyBoundPorts:   map[int]bool{},
		RunningProjects:   map[string]bool{"app-b-running": true},
	}

	p := &Panel{
		DB:             store,
		Store:          wsStore,
		WorkspacesRoot: wsRoot,
		applyRootStackComposeFn: func(composeFile string, services ...string) {
			applied = append(applied, append([]string{composeFile}, services...))
		},
		inspectLivePortStateFn: func(ctx context.Context) (*dockerapi.LivePortState, error) {
			return phase1Live, nil
		},
	}

	p.ReconcileAllAppSlotCompose(ctx)

	// Running app-b-running must reclaim 12002 + 18080 (HTTP) + 54325 (Direct)
	alloc12002, err := store.GetPortAllocation(ctx, 12002)
	if err != nil || alloc12002 == nil || alloc12002.AppID != "app-b-running" || alloc12002.Direct {
		t.Fatalf("expected running app-b-running to reclaim 12002 (direct=false), got %+v (err=%v)", alloc12002, err)
	}
	alloc18080, err := store.GetPortAllocation(ctx, 18080)
	if err != nil || alloc18080 == nil || alloc18080.AppID != "app-b-running" || alloc18080.Direct {
		t.Fatalf("expected .env APP_PORT=18080 allocated to app-b-running, got %+v (err=%v)", alloc18080, err)
	}
	alloc54325, err := store.GetPortAllocation(ctx, 54325)
	if err != nil || alloc54325 == nil || alloc54325.AppID != "app-b-running" || !alloc54325.Direct {
		t.Fatalf("expected postgres port 54325 marked direct=true for app-b-running, got %+v (err=%v)", alloc54325, err)
	}

	// Phase 1 must NOT tell Caddy to bind 12002 while legacy container binds it directly!
	rootBytes, _ := os.ReadFile(stackComposePath)
	if strings.Contains(string(rootBytes), "12002:12002") || strings.Contains(string(rootBytes), "54325:54325") {
		t.Fatalf("caddy compose must not bind legacy-occupied or direct ports in phase 1:\n%s", string(rootBytes))
	}

	// Phase 2: Container redeployed as slot stack advertising Caddy labels (:12002, :18080),
	// while live Caddy container only binds 80/443 right after an update.
	phase1Live = &dockerapi.LivePortState{
		DirectHostPorts:   map[int]bool{54325: true},
		CaddyGatewayPorts: map[int]bool{12002: true, 18080: true},
		CaddyBoundPorts:   map[int]bool{},
		RunningProjects:   map[string]bool{"app-b-running-blue": true},
	}

	p.syncAndApplyRootCaddyIfNeeded(ctx)

	rootBytes, _ = os.ReadFile(stackComposePath)
	rootText := string(rootBytes)
	if !strings.Contains(rootText, "12002:12002") || !strings.Contains(rootText, "18080:18080") {
		t.Fatalf("expected Caddy root compose to publish 12002 and 18080:\n%s", rootText)
	}
	if strings.Contains(rootText, "54325:54325") {
		t.Fatalf("direct postgres port 54325 must not be published on Caddy:\n%s", rootText)
	}
	if len(applied) == 0 || applied[len(applied)-1][1] != "caddy" {
		t.Fatalf("expected caddy apply trigger when live Caddy bindings differ, got %+v", applied)
	}
}
