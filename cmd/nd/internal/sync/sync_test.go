package sync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDiff_ExcludesEnvFiles(t *testing.T) {
	// Server manifest containing .env and uploads/logo.png
	serverManifest := manifestResponse{
		Files: []struct {
			Path   string `json:"path"`
			IsDir  bool   `json:"is_dir"`
			SHA256 string `json:"sha256"`
		}{
			{Path: "main.go", IsDir: false, SHA256: "hash1"},
			{Path: ".env", IsDir: false, SHA256: "secret-env-hash"},
			{Path: ".env.production", IsDir: false, SHA256: "secret-prod-hash"},
			{Path: "uploads/logo.png", IsDir: false, SHA256: "logo-hash"},
		},
	}
	manifestBytes, err := json.Marshal(serverManifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	// Local files without .env
	local := map[string]string{
		"main.go": "hash1",
	}

	toUpload, toDelete, err := Diff(manifestBytes, local)
	if err != nil {
		t.Fatalf("Diff error: %v", err)
	}

	if len(toUpload) != 0 {
		t.Errorf("expected 0 toUpload, got %d: %v", len(toUpload), toUpload)
	}

	// .env and .env.* MUST NOT be scheduled for deletion
	for _, del := range toDelete {
		if del == ".env" || del == ".env.production" {
			t.Fatalf("CRITICAL BUG: %s was scheduled for deletion on server!", del)
		}
	}

	if len(toDelete) != 1 || toDelete[0] != "uploads/logo.png" {
		t.Errorf("expected toDelete [uploads/logo.png], got %v", toDelete)
	}
}

func TestLocalHashes_WithGitIgnore(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nd-sync-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test files
	_ = os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, ".env"), []byte("SECRET=123"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, ".env.local"), []byte("SECRET=local"), 0644)
	_ = os.MkdirAll(filepath.Join(tempDir, "dist"), 0755)
	_ = os.WriteFile(filepath.Join(tempDir, "dist", "bundle.js"), []byte("code"), 0644)
	_ = os.MkdirAll(filepath.Join(tempDir, "secret_dir"), 0755)
	_ = os.WriteFile(filepath.Join(tempDir, "secret_dir", "keys.txt"), []byte("keys"), 0644)

	// .gitignore file
	gitignoreContent := "dist/\nsecret_dir/**\n*.tmp\n"
	_ = os.WriteFile(filepath.Join(tempDir, ".gitignore"), []byte(gitignoreContent), 0644)

	hashes, err := LocalHashes(tempDir)
	if err != nil {
		t.Fatalf("LocalHashes error: %v", err)
	}

	// main.go should be present
	if _, ok := hashes["main.go"]; !ok {
		t.Errorf("expected main.go in hashes, got %v", hashes)
	}

	// .env and .env.local MUST NOT be present
	if _, ok := hashes[".env"]; ok {
		t.Errorf("expected .env to be ignored by LocalHashes")
	}
	if _, ok := hashes[".env.local"]; ok {
		t.Errorf("expected .env.local to be ignored by LocalHashes")
	}

	// Ignored directories must not be present
	for path := range hashes {
		if filepath.HasPrefix(path, "dist") || filepath.HasPrefix(path, "secret_dir") {
			t.Errorf("expected %s to be ignored by gitignore", path)
		}
	}
}
