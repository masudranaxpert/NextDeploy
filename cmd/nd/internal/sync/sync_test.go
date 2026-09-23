package sync

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"io"
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

func TestLocalHashes_SingleFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nd-sync-single-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tempDir)

	filePath := filepath.Join(tempDir, "script.py")
	if err := os.WriteFile(filePath, []byte("print(42)\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	hashes, err := LocalHashes(filePath)
	if err != nil {
		t.Fatalf("LocalHashes on file returned error: %v", err)
	}

	if len(hashes) != 1 {
		t.Fatalf("expected 1 file in hashes, got %d", len(hashes))
	}

	if _, ok := hashes["script.py"]; !ok {
		t.Errorf("expected key 'script.py' in hashes, got %v", hashes)
	}
}

func TestLocalHashes_Cache(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nd-sync-cache-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tempDir)

	filePath := filepath.Join(tempDir, "app.txt")
	_ = os.WriteFile(filePath, []byte("version1"), 0644)

	// First run creates cache
	hashes1, err := LocalHashes(tempDir)
	if err != nil {
		t.Fatalf("LocalHashes 1: %v", err)
	}

	cacheFile := filepath.Join(tempDir, ".nd", "hash_cache.json")
	if _, err := os.Stat(cacheFile); err != nil {
		t.Fatalf("expected hash_cache.json to exist at %s", cacheFile)
	}

	// Second run should return identical hash
	hashes2, err := LocalHashes(tempDir)
	if err != nil {
		t.Fatalf("LocalHashes 2: %v", err)
	}

	if hashes1["app.txt"] != hashes2["app.txt"] {
		t.Errorf("hash mismatch between cached runs: %q vs %q", hashes1["app.txt"], hashes2["app.txt"])
	}
}

func TestPackTarGz_PreservesExecutableMode(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nd-sync-pack-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tempDir)

	normalFile := filepath.Join(tempDir, "file.txt")
	scriptFile := filepath.Join(tempDir, "run.sh")

	if err := os.WriteFile(normalFile, []byte("hello"), 0644); err != nil {
		t.Fatalf("WriteFile normal: %v", err)
	}
	if err := os.WriteFile(scriptFile, []byte("#!/bin/sh\necho hi"), 0755); err != nil {
		t.Fatalf("WriteFile script: %v", err)
	}

	reader, _, err := PackTarGz(tempDir, []string{"file.txt", "run.sh"})
	if err != nil {
		t.Fatalf("PackTarGz: %v", err)
	}

	gzReader, err := gzip.NewReader(reader)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	modes := make(map[string]int64)

	for {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar.Next: %v", err)
		}
		modes[hdr.Name] = hdr.Mode
	}

	if modes["file.txt"] != 0644 {
		t.Errorf("expected file.txt mode 0644, got %o", modes["file.txt"])
	}
	if modes["run.sh"] != 0755 {
		t.Errorf("expected run.sh mode 0755, got %o", modes["run.sh"])
	}
}

