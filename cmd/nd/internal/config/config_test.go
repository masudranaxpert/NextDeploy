package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyMigration(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	dotNd := filepath.Join(tempHome, ".nd")
	if err := os.MkdirAll(dotNd, 0700); err != nil {
		t.Fatalf("failed to create temp .nd dir: %v", err)
	}

	legacyJSON := `{
  "server_url": "https://legacy.example.com",
  "token": "tok_legacy_123",
  "device_id": "nd_legacy_dev"
}`
	if err := os.WriteFile(filepath.Join(dotNd, "config.json"), []byte(legacyJSON), 0600); err != nil {
		t.Fatalf("failed to write legacy config: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.CurrentAccount != "default" {
		t.Errorf("expected CurrentAccount 'default', got %q", cfg.CurrentAccount)
	}
	if len(cfg.Accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(cfg.Accounts))
	}
	acc, ok := cfg.Accounts["default"]
	if !ok {
		t.Fatalf("expected 'default' account in Accounts map")
	}
	if acc.ServerURL != "https://legacy.example.com" {
		t.Errorf("expected server url 'https://legacy.example.com', got %q", acc.ServerURL)
	}
	if acc.Token != "tok_legacy_123" {
		t.Errorf("expected token 'tok_legacy_123', got %q", acc.Token)
	}
	if acc.DeviceID != "nd_legacy_dev" {
		t.Errorf("expected device id 'nd_legacy_dev', got %q", acc.DeviceID)
	}

	// Verify top-level mirror
	if cfg.ServerURL != "https://legacy.example.com" || cfg.Token != "tok_legacy_123" {
		t.Errorf("top-level mirror fields not populated properly: %+v", cfg)
	}
}

func TestAccountOperations(t *testing.T) {
	var cfg Config

	// 1. Add first account
	cfg.SetAccount("prod", AccountConfig{
		ServerURL: "https://prod.example.com",
		Token:     "tok_prod",
		Username:  "admin",
	}, true)

	if cfg.CurrentAccount != "prod" {
		t.Errorf("expected current account 'prod', got %q", cfg.CurrentAccount)
	}
	if cfg.ServerURL != "https://prod.example.com" || cfg.Token != "tok_prod" {
		t.Errorf("top-level mirror mismatch for prod: %+v", cfg)
	}

	// 2. Add second account without making active
	cfg.SetAccount("staging", AccountConfig{
		ServerURL: "https://staging.example.com",
		Token:     "tok_staging",
		Username:  "tester",
	}, false)

	if cfg.CurrentAccount != "prod" {
		t.Errorf("current account should still be 'prod', got %q", cfg.CurrentAccount)
	}

	// 3. Switch account
	if err := cfg.SwitchAccount("staging"); err != nil {
		t.Fatalf("SwitchAccount failed: %v", err)
	}
	if cfg.CurrentAccount != "staging" {
		t.Errorf("expected current account 'staging', got %q", cfg.CurrentAccount)
	}
	if cfg.ServerURL != "https://staging.example.com" || cfg.Token != "tok_staging" {
		t.Errorf("top-level mirror not updated after switch: %+v", cfg)
	}

	// Switch to non-existent
	if err := cfg.SwitchAccount("nonexistent"); err == nil {
		t.Errorf("expected error switching to non-existent account")
	}

	// 4. Rename account
	if err := cfg.RenameAccount("staging", "stage-v2"); err != nil {
		t.Fatalf("RenameAccount failed: %v", err)
	}
	if cfg.CurrentAccount != "stage-v2" {
		t.Errorf("expected current account 'stage-v2' after rename, got %q", cfg.CurrentAccount)
	}
	if _, ok := cfg.Accounts["staging"]; ok {
		t.Errorf("old account name 'staging' should be deleted")
	}
	if _, ok := cfg.Accounts["stage-v2"]; !ok {
		t.Errorf("new account name 'stage-v2' should exist")
	}

	// 5. Delete active account - should auto-switch to remaining account (prod)
	newActive, err := cfg.DeleteAccount("stage-v2")
	if err != nil {
		t.Fatalf("DeleteAccount failed: %v", err)
	}
	if newActive != "prod" || cfg.CurrentAccount != "prod" {
		t.Errorf("expected auto-switch to 'prod', got %q", cfg.CurrentAccount)
	}
	if cfg.ServerURL != "https://prod.example.com" {
		t.Errorf("expected server url to be prod, got %q", cfg.ServerURL)
	}

	// 6. Delete last account
	lastActive, err := cfg.DeleteAccount("prod")
	if err != nil {
		t.Fatalf("DeleteAccount failed: %v", err)
	}
	if lastActive != "" || cfg.CurrentAccount != "" {
		t.Errorf("expected empty current account after deleting all, got %q", cfg.CurrentAccount)
	}
	if cfg.ServerURL != "" || cfg.Token != "" {
		t.Errorf("expected empty top-level credentials after deleting all, got %+v", cfg)
	}
}

func TestSaveAndLoadRoundtrip(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	var initial Config
	initial.SetAccount("work", AccountConfig{
		ServerURL: "https://work.example.com",
		Token:     "tok_work_999",
		Username:  "masud",
		DeviceID:  "nd_test_dev_1",
	}, true)

	initial.SetAccount("personal", AccountConfig{
		ServerURL: "https://personal.example.com",
		Token:     "tok_personal_888",
		Username:  "masud-home",
		DeviceID:  "nd_test_dev_2",
	}, false)

	if err := Save(initial); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Read raw JSON from disk to ensure top-level mirrors are present for older tools
	p, err := path()
	if err != nil {
		t.Fatalf("path() error: %v", err)
	}
	rawBytes, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}

	var rawMap map[string]interface{}
	if err := json.Unmarshal(rawBytes, &rawMap); err != nil {
		t.Fatalf("json parse error: %v", err)
	}
	if rawMap["server_url"] != "https://work.example.com" {
		t.Errorf("raw JSON missing top-level server_url: %v", rawMap["server_url"])
	}
	if rawMap["current_account"] != "work" {
		t.Errorf("raw JSON current_account mismatch: %v", rawMap["current_account"])
	}

	// Load back
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.CurrentAccount != "work" {
		t.Errorf("expected CurrentAccount 'work', got %q", loaded.CurrentAccount)
	}
	if len(loaded.Accounts) != 2 {
		t.Errorf("expected 2 accounts, got %d", len(loaded.Accounts))
	}

	// Test ND_ACCOUNT env override
	t.Setenv("ND_ACCOUNT", "personal")
	overridden, err := Load()
	if err != nil {
		t.Fatalf("Load with ND_ACCOUNT failed: %v", err)
	}
	if overridden.CurrentAccount != "personal" {
		t.Errorf("expected ND_ACCOUNT override to select 'personal', got %q", overridden.CurrentAccount)
	}
	if overridden.ServerURL != "https://personal.example.com" {
		t.Errorf("expected server url to be personal, got %q", overridden.ServerURL)
	}
}
