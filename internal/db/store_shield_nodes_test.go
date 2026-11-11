package db

import (
	"context"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

func TestShieldNodesMigrationsAndPanelID(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "shield_migrations.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	ctx := context.Background()
	panelID := store.GetSetting(ctx, SettingPanelID)
	uuidRe := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !uuidRe.MatchString(panelID) {
		t.Fatalf("expected valid UUIDv4 panel_id, got %q", panelID)
	}

	// Idempotent re-migration & reopen must preserve panel_id
	if err := store.migrateShieldNodes(); err != nil {
		t.Fatalf("second migrateShieldNodes failed: %v", err)
	}
	_ = store.Close()

	store2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("re-Open failed: %v", err)
	}
	defer store2.Close()

	if got := store2.GetSetting(ctx, SettingPanelID); got != panelID {
		t.Fatalf("panel_id regenerated on reopen: want %q, got %q", panelID, got)
	}
}

func TestShieldNodeTokenRotationGraceAndTombstone(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "shield_tokens.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	id, err := store.CreateShieldNode(ctx, &ShieldNode{
		Name:  "edge-1",
		IP:    "107.189.6.222",
		Token: "shld_initial_token_abc",
	})
	if err != nil {
		t.Fatalf("CreateShieldNode: %v", err)
	}

	rotated, err := store.RotateShieldNodeToken(ctx, id, 10*time.Minute)
	if err != nil {
		t.Fatalf("RotateShieldNodeToken: %v", err)
	}
	if rotated.Token == "shld_initial_token_abc" || rotated.Token == "" {
		t.Fatalf("expected new rotated token, got %q", rotated.Token)
	}

	// Both new and previous token accepted during grace window
	if n, err := store.GetShieldNodeByToken(ctx, rotated.Token); err != nil || n.ID != id {
		t.Fatalf("new token should authenticate: %v", err)
	}
	if n, err := store.GetShieldNodeByToken(ctx, "shld_initial_token_abc"); err != nil || n.ID != id {
		t.Fatalf("previous token should authenticate during grace: %v", err)
	}

	// Expire token_prev
	_, err = store.ExecRaw(ctx, `UPDATE shield_nodes SET token_prev_expires_at = ? WHERE id = ?`,
		time.Now().UTC().Add(-1*time.Minute).Format(time.RFC3339), id)
	if err != nil {
		t.Fatalf("expire token_prev: %v", err)
	}
	if _, err := store.GetShieldNodeByToken(ctx, "shld_initial_token_abc"); err == nil {
		t.Fatalf("expired token_prev should be rejected")
	}

	// Delete node records tombstone
	if err := store.DeleteShieldNode(ctx, id); err != nil {
		t.Fatalf("DeleteShieldNode: %v", err)
	}
	deleted, err := store.IsDeletedShieldToken(ctx, rotated.Token)
	if err != nil || !deleted {
		t.Fatalf("expected rotated token to be marked deleted, got deleted=%v err=%v", deleted, err)
	}
}

func TestShieldDomainStatusUpsertAndIsolation(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "shield_status.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	n1, _ := store.CreateShieldNode(ctx, &ShieldNode{Name: "s1", IP: "1.1.1.1", Token: "shld_1"})
	n2, _ := store.CreateShieldNode(ctx, &ShieldNode{Name: "s2", IP: "2.2.2.2", Token: "shld_2"})

	exp := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	err = store.UpdateShieldNodeEdgeStatus(ctx, n1, "1.0.0", "v2.8.4", "etag1", "", `{"ok":true}`, []ShieldDomainStatus{
		{Host: "a.example.com", State: "active", CertNotAfter: &exp},
		{Host: "b.example.com", State: "conflict", Detail: "vps-2"},
	})
	if err != nil {
		t.Fatalf("UpdateShieldNodeEdgeStatus n1: %v", err)
	}
	err = store.UpdateShieldNodeEdgeStatus(ctx, n2, "1.0.0", "v2.8.4", "etag2", "", `{"ok":true}`, []ShieldDomainStatus{
		{Host: "c.example.com", State: "pending_dns", Detail: "points to 9.9.9.9"},
	})
	if err != nil {
		t.Fatalf("UpdateShieldNodeEdgeStatus n2: %v", err)
	}

	s1, err := store.ListShieldDomainStatuses(ctx, n1)
	if err != nil || len(s1) != 2 {
		t.Fatalf("expected 2 statuses for n1, got %d (err=%v)", len(s1), err)
	}
	s2, err := store.ListShieldDomainStatuses(ctx, n2)
	if err != nil || len(s2) != 1 || s2[0].Host != "c.example.com" {
		t.Fatalf("expected isolated status for n2, got %+v (err=%v)", s2, err)
	}

	node1, err := store.GetShieldNode(ctx, n1)
	if err != nil {
		t.Fatalf("GetShieldNode n1: %v", err)
	}
	if node1.ConflictsCount != 1 || node1.SyncState != "ok" || node1.EdgeVersion != "1.0.0" {
		t.Fatalf("unexpected node1 fields: %+v", node1)
	}
}
