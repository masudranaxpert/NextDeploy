package workspace

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFileModeForPath(t *testing.T) {
	cases := []struct {
		rel  string
		data string
		hint os.FileMode
		want os.FileMode
	}{
		{"src/app.py", "print('hi')\n", 0600, 0644},
		{"Dockerfile", "FROM python:3.12\n", 0640, 0644},
		{".env.example", "FOO=bar\n", 0600, 0644},
		{".env", "SECRET=1\n", 0644, 0600},
		{"sub/.env.production", "SECRET=1\n", 0644, 0600},
		{"scripts/start.sh", "echo ok\n", 0644, 0755},
		{"bin/run", "#!/bin/sh\necho ok\n", 0644, 0755},
		{"bin/tool", "binary", 0755, 0755},
	}
	for _, tc := range cases {
		if got := FileModeForPath(tc.rel, []byte(tc.data), tc.hint); got != tc.want {
			t.Errorf("FileModeForPath(%q, hint=%#o) = %#o, want %#o", tc.rel, tc.hint, got, tc.want)
		}
	}
}

func TestExtractTarGzSetsReadableModesAndRepairsExisting(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	appID := "app-perms"
	base := store.Path(appID)

	// Pre-create a restrictive subdir and file (simulating older panel 0750/0600 state).
	subDir := filepath.Join(base, "app")
	if err := os.MkdirAll(subDir, 0750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := filepath.Join(subDir, "main.py")
	if err := os.WriteFile(existing, []byte("old"), 0600); err != nil {
		t.Fatalf("write existing: %v", err)
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(subDir, 0750)
		_ = os.Chmod(existing, 0600)
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	addFile := func(name, body string, mode int64) {
		t.Helper()
		hdr := &tar.Header{Name: name, Mode: mode, Size: int64(len(body)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("header %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	addFile("app/main.py", "print('new')\n", 0600)
	addFile("app/entrypoint.sh", "echo start\n", 0600)
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gz: %v", err)
	}

	n, _, err := store.ExtractArchiveDir(appID, base, &buf, "workspace.tar.gz")
	if err != nil {
		t.Fatalf("ExtractArchiveDir: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 extracted files, got %d", n)
	}

	gotBytes, err := os.ReadFile(existing)
	if err != nil || string(gotBytes) != "print('new')\n" {
		t.Fatalf("unexpected main.py content: %q (err=%v)", string(gotBytes), err)
	}

	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(subDir); err != nil || fi.Mode().Perm()&0755 != 0755 {
			t.Fatalf("expected app/ dir to be 0755, got %#o (err=%v)", fi.Mode().Perm(), err)
		}
		if fi, err := os.Stat(existing); err != nil || fi.Mode().Perm() != 0644 {
			t.Fatalf("expected main.py to be repaired to 0644, got %#o (err=%v)", fi.Mode().Perm(), err)
		}
		if fi, err := os.Stat(filepath.Join(subDir, "entrypoint.sh")); err != nil || fi.Mode().Perm() != 0755 {
			t.Fatalf("expected entrypoint.sh to be 0755, got %#o (err=%v)", fi.Mode().Perm(), err)
		}
	}
}
