package cmd

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestPosixQuote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "''"},
		{"abc", "abc"},
		{"my-app_1.0:test", "my-app_1.0:test"},
		{"hello world", "'hello world'"},
		{"print(x)", "'print(x)'"},
		{"foo'bar", `'foo'\''bar'`},
		{"$VAR", "'$VAR'"},
		{"a; b", "'a; b'"},
	}

	for _, tc := range tests {
		got := posixQuote(tc.input)
		if got != tc.want {
			t.Errorf("posixQuote(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestPosixJoin(t *testing.T) {
	args := []string{"python", "-c", "print('hello')"}
	got := posixJoin(args)
	want := "python -c 'print('\\''hello'\\'')'"
	if got != want {
		t.Errorf("posixJoin(%v) = %q, want %q", args, got, want)
	}
}

func TestIsSafeTarPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"main.go", true},
		{"src/sub/app.js", true},
		{".env.example", true},
		{"", false},
		{"../evil", false},
		{"/root/evil", false},
		{"a/../../b", false},
		{"..", false},
	}

	for _, tc := range tests {
		got := isSafeTarPath(tc.path)
		if got != tc.want {
			t.Errorf("isSafeTarPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestHasHelpFlag(t *testing.T) {
	if !hasHelpFlag([]string{"-h"}) {
		t.Errorf("expected -h to be detected as help")
	}
	if !hasHelpFlag([]string{"foo", "--help"}) {
		t.Errorf("expected --help to be detected as help")
	}
	if hasHelpFlag([]string{"foo", "bar"}) {
		t.Errorf("expected no help flag")
	}
}

func TestExtractWorkspaceArchive(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "nd_extract_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test tar in memory
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// 1. Regular file (mode 0644)
	docContent := []byte("hello world")
	_ = tw.WriteHeader(&tar.Header{
		Name:     "README.md",
		Mode:     0644,
		Size:     int64(len(docContent)),
		Typeflag: tar.TypeReg,
	})
	_, _ = tw.Write(docContent)

	// 2. Executable script (mode 0755)
	scriptContent := []byte("#!/bin/sh\necho test\n")
	_ = tw.WriteHeader(&tar.Header{
		Name:     "bin/entrypoint.sh",
		Mode:     0755,
		Size:     int64(len(scriptContent)),
		Typeflag: tar.TypeReg,
	})
	_, _ = tw.Write(scriptContent)

	// 3. Safe relative symlink (pointing inside root)
	_ = tw.WriteHeader(&tar.Header{
		Name:     "bin/run.sh",
		Linkname: "entrypoint.sh",
		Typeflag: tar.TypeSymlink,
	})

	// 4. Unsafe symlink (pointing outside root)
	_ = tw.WriteHeader(&tar.Header{
		Name:     "bin/escape.sh",
		Linkname: "../../etc/passwd",
		Typeflag: tar.TypeSymlink,
	})

	// 5. Protected .env file (must be ignored)
	envContent := []byte("SECRET=123\n")
	_ = tw.WriteHeader(&tar.Header{
		Name:     ".env",
		Mode:     0600,
		Size:     int64(len(envContent)),
		Typeflag: tar.TypeReg,
	})
	_, _ = tw.Write(envContent)

	// 6. Path traversal file (must be ignored)
	evilContent := []byte("evil")
	_ = tw.WriteHeader(&tar.Header{
		Name:     "../outside.txt",
		Mode:     0644,
		Size:     int64(len(evilContent)),
		Typeflag: tar.TypeReg,
	})
	_, _ = tw.Write(evilContent)

	_ = tw.Close()

	extracted, skipped, _, err := extractWorkspaceArchive(&buf, tmpDir)
	if err != nil {
		t.Fatalf("extractWorkspaceArchive failed: %v", err)
	}

	// README.md, bin/entrypoint.sh, and bin/run.sh should be extracted (3 files/links)
	if extracted < 2 {
		t.Errorf("expected at least 2 regular extracted files, got %d", extracted)
	}
	// escape.sh and outside.txt should be skipped
	if skipped == 0 {
		t.Errorf("expected skipped count > 0 for unsafe entries, got %d", skipped)
	}

	// Verify README.md exists and has non-exec mode
	readmePath := filepath.Join(tmpDir, "README.md")
	readmeInfo, err := os.Stat(readmePath)
	if err != nil {
		t.Fatalf("README.md not extracted: %v", err)
	}
	if readmeInfo.Mode().Perm()&0111 != 0 {
		t.Errorf("README.md should not be executable, got perm: %v", readmeInfo.Mode().Perm())
	}

	// Verify bin/entrypoint.sh exists and preserved executable bit
	scriptPath := filepath.Join(tmpDir, "bin", "entrypoint.sh")
	scriptInfo, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("entrypoint.sh not extracted: %v", err)
	}
	if scriptInfo.Mode().Perm()&0100 == 0 {
		t.Errorf("entrypoint.sh must retain executable bit, got perm: %v", scriptInfo.Mode().Perm())
	}

	// Verify .env was NOT created
	envPath := filepath.Join(tmpDir, ".env")
	if _, err := os.Stat(envPath); !os.IsNotExist(err) {
		t.Errorf(".env file must not be extracted from archive")
	}

	// Verify unsafe symlink was NOT created
	escapePath := filepath.Join(tmpDir, "bin", "escape.sh")
	if _, err := os.Lstat(escapePath); !os.IsNotExist(err) {
		t.Errorf("escaping symlink must not be created")
	}
}
