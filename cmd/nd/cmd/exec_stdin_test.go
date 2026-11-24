package cmd

import (
	"os"
	"testing"
	"time"
)

func TestReadExecStdin_DoesNotHangOnOpenPipeWithoutFlag(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer pr.Close()
	defer pw.Close()

	// Pipe stays open (simulates Windows Git Bash/Mintty/agent subprocess stdin).
	done := make(chan string, 1)
	go func() {
		done <- readExecStdin(false, pr)
	}()

	select {
	case got := <-done:
		if got != "" {
			t.Fatalf("expected empty stdin without -i, got %q", got)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("readExecStdin blocked on open pipe even though -i/--stdin was not set")
	}
}

func TestReadExecStdin_ReadsPipeWithFlagAndRedirectedFile(t *testing.T) {
	// 1. Explicit -i/--stdin reads piped input.
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	go func() {
		_, _ = pw.WriteString("hello from pipe\n")
		_ = pw.Close()
	}()
	got := readExecStdin(true, pr)
	_ = pr.Close()
	if got != "hello from pipe\n" {
		t.Fatalf("expected piped stdin with -i, got %q", got)
	}

	// 2. Non-empty redirected regular file works without -i and without blocking.
	tmp, err := os.CreateTemp(t.TempDir(), "stdin-*.txt")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer tmp.Close()
	if _, err := tmp.WriteString("select 1;\n"); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	if _, err := tmp.Seek(0, 0); err != nil {
		t.Fatalf("seek tmp: %v", err)
	}
	if got := readExecStdin(false, tmp); got != "select 1;\n" {
		t.Fatalf("expected redirected file stdin, got %q", got)
	}
}
