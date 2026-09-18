package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFormatFileSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}

	for _, tc := range tests {
		got := formatFileSize(tc.bytes)
		if got != tc.want {
			t.Errorf("formatFileSize(%d) = %q, want %q", tc.bytes, got, tc.want)
		}
	}
}

func TestResolveFileApp_ExplicitFlag(t *testing.T) {
	appID, rest, err := resolveFileApp([]string{"--app", "my-app", "src/main.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if appID != "my-app" {
		t.Errorf("expected appID 'my-app', got %q", appID)
	}
	if len(rest) != 1 || rest[0] != "src/main.go" {
		t.Errorf("expected rest ['src/main.go'], got %v", rest)
	}
}

func TestResolveFileApp_PositionalWhenNotLinked(t *testing.T) {
	// In a temp unlinked dir
	tmpDir, err := os.MkdirTemp("", "nd-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	origWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	appID, rest, err := resolveFileApp([]string{"some-app", "file.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if appID != "some-app" {
		t.Errorf("expected appID 'some-app', got %q", appID)
	}
	if len(rest) != 1 || rest[0] != "file.txt" {
		t.Errorf("expected rest ['file.txt'], got %v", rest)
	}
}

func TestResolveFileApp_LinkedProject(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "nd-linked-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create .nd/project.json
	ndDir := filepath.Join(tmpDir, ".nd")
	_ = os.MkdirAll(ndDir, 0750)
	_ = os.WriteFile(filepath.Join(ndDir, "project.json"), []byte(`{"app_id":"linked-app"}`), 0640)

	origWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	appID, rest, err := resolveFileApp([]string{"index.html"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if appID != "linked-app" {
		t.Errorf("expected linked app 'linked-app', got %q", appID)
	}
	if len(rest) != 1 || rest[0] != "index.html" {
		t.Errorf("expected rest ['index.html'], got %v", rest)
	}
}
