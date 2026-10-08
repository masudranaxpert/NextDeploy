package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"nd/internal/config"
)

func setupTestConfig(t *testing.T) string {
	t.Helper()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	dotNd := filepath.Join(tempHome, ".nd")
	if err := os.MkdirAll(dotNd, 0700); err != nil {
		t.Fatalf("failed to create temp .nd: %v", err)
	}

	var cfg config.Config
	cfg.SetAccount("prod", config.AccountConfig{
		ServerURL: "https://prod.example.com",
		Token:     "tok_prod_123",
		Username:  "admin",
		DeviceID:  "dev_prod_1",
	}, true)

	cfg.SetAccount("staging", config.AccountConfig{
		ServerURL: "https://staging.example.com",
		Token:     "tok_stage_456",
		Username:  "developer",
		DeviceID:  "dev_stage_2",
	}, false)

	if err := config.Save(cfg); err != nil {
		t.Fatalf("failed to save initial config: %v", err)
	}
	return tempHome
}

func captureOutput(f func() error) (string, error) {
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := f()

	_ = w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String(), err
}

func TestRunAccountList(t *testing.T) {
	setupTestConfig(t)

	out, err := captureOutput(func() error {
		return RunAccount([]string{"list"})
	})
	if err != nil {
		t.Fatalf("RunAccount list failed: %v", err)
	}

	if !bytes.Contains([]byte(out), []byte("prod")) {
		t.Errorf("expected 'prod' in output, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("staging")) {
		t.Errorf("expected 'staging' in output, got: %s", out)
	}

	// Test JSON output
	jsonOut, err := captureOutput(func() error {
		return RunAccount([]string{"list", "--json"})
	})
	if err != nil {
		t.Fatalf("RunAccount list --json failed: %v", err)
	}
	if !bytes.Contains([]byte(jsonOut), []byte(`"name": "prod"`)) {
		t.Errorf("expected json output with prod, got: %s", jsonOut)
	}
}

func TestRunAccountSwitch(t *testing.T) {
	setupTestConfig(t)

	if err := RunAccount([]string{"switch", "staging"}); err != nil {
		t.Fatalf("RunAccount switch failed: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load failed: %v", err)
	}
	if cfg.CurrentAccount != "staging" {
		t.Errorf("expected current account 'staging', got %q", cfg.CurrentAccount)
	}
	if cfg.ServerURL != "https://staging.example.com" {
		t.Errorf("expected server url to mirror staging, got %q", cfg.ServerURL)
	}
}

func TestRunAccountRename(t *testing.T) {
	setupTestConfig(t)

	if err := RunAccount([]string{"rename", "staging", "qa"}); err != nil {
		t.Fatalf("RunAccount rename failed: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load failed: %v", err)
	}
	if _, ok := cfg.Accounts["qa"]; !ok {
		t.Errorf("expected account 'qa' to exist")
	}
	if _, ok := cfg.Accounts["staging"]; ok {
		t.Errorf("account 'staging' should not exist after rename")
	}
}

func TestRunAccountDeleteWithForce(t *testing.T) {
	setupTestConfig(t)

	// Delete currently active account "prod" with -f
	if err := RunAccount([]string{"delete", "prod", "-f"}); err != nil {
		t.Fatalf("RunAccount delete failed: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load failed: %v", err)
	}
	if cfg.CurrentAccount != "staging" {
		t.Errorf("expected auto-switch to 'staging', got %q", cfg.CurrentAccount)
	}
	if _, ok := cfg.Accounts["prod"]; ok {
		t.Errorf("account 'prod' should be deleted")
	}
}

func TestRunAccountCurrent(t *testing.T) {
	setupTestConfig(t)

	out, err := captureOutput(func() error {
		return RunAccount([]string{"current"})
	})
	if err != nil {
		t.Fatalf("RunAccount current failed: %v", err)
	}
	if !bytes.Contains([]byte(out), []byte("prod")) {
		t.Errorf("expected current account 'prod' in output, got: %s", out)
	}
}

func TestRunLogoutSpecificAndAll(t *testing.T) {
	setupTestConfig(t)

	// Logout specific account "staging"
	if err := RunLogout([]string{"staging"}); err != nil {
		t.Fatalf("RunLogout staging failed: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load failed: %v", err)
	}
	if len(cfg.Accounts) != 1 {
		t.Fatalf("expected 1 remaining account, got %d", len(cfg.Accounts))
	}
	if cfg.CurrentAccount != "prod" {
		t.Errorf("current account should still be 'prod', got %q", cfg.CurrentAccount)
	}

	// Logout --all
	if err := RunLogout([]string{"--all"}); err != nil {
		t.Fatalf("RunLogout --all failed: %v", err)
	}

	cfgAll, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load failed: %v", err)
	}
	if len(cfgAll.Accounts) != 0 {
		t.Errorf("expected 0 accounts after logout --all, got %d", len(cfgAll.Accounts))
	}
	if cfgAll.CurrentAccount != "" {
		t.Errorf("expected empty current account, got %q", cfgAll.CurrentAccount)
	}
}
