package db

import (
	"context"
	"testing"
)

func TestPortAllocationsRegistry(t *testing.T) {
	ctx := context.Background()
	store, err := Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer store.Close()

	// Create test apps first to satisfy foreign key
	uid, err := store.CreateUser(ctx, "testuser", "hash", RoleAdmin)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	if err := store.CreateApp(ctx, "app1", "App One", uid); err != nil {
		t.Fatalf("failed to create app1: %v", err)
	}
	if err := store.CreateApp(ctx, "app2", "App Two", uid); err != nil {
		t.Fatalf("failed to create app2: %v", err)
	}

	// Allocate port 4013 for app1
	err = store.AllocatePort(ctx, 4013, "0.0.0.0", "app1", "web", "tcp")
	if err != nil {
		t.Fatalf("failed to allocate port 4013: %v", err)
	}

	// Verify it can be retrieved
	alloc, err := store.GetPortAllocation(ctx, 4013)
	if err != nil {
		t.Fatalf("failed to get port allocation: %v", err)
	}
	if alloc == nil || alloc.AppID != "app1" || alloc.Port != 4013 {
		t.Fatalf("unexpected allocation: %+v", alloc)
	}

	// Same app re-allocating (e.g. updating service or bind IP) should succeed
	err = store.AllocatePort(ctx, 4013, "127.0.0.1", "app1", "web-api", "tcp")
	if err != nil {
		t.Fatalf("failed to update port allocation by same app: %v", err)
	}
	alloc, _ = store.GetPortAllocation(ctx, 4013)
	if alloc.BindIP != "127.0.0.1" || alloc.Service != "web-api" {
		t.Fatalf("expected updated bind_ip and service: %+v", alloc)
	}

	// Different app trying to allocate port 4013 should FAIL
	err = store.AllocatePort(ctx, 4013, "0.0.0.0", "app2", "web", "tcp")
	if err == nil {
		t.Fatalf("expected port collision error for app2, but got nil")
	}

	// Release app1 ports
	err = store.ReleaseAppPorts(ctx, "app1")
	if err != nil {
		t.Fatalf("failed to release ports: %v", err)
	}

	// Now app2 can allocate port 4013
	err = store.AllocatePort(ctx, 4013, "0.0.0.0", "app2", "web", "tcp")
	if err != nil {
		t.Fatalf("app2 should be able to allocate freed port 4013: %v", err)
	}
}
