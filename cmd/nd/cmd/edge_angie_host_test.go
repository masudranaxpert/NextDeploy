package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"nd/internal/edge"
)

func init() {
	// Tests that write the Angie drop-in must never touch the host's logrotate config.
	angieLogrotatePath = filepath.Join(os.TempDir(), "nd-test-absent", "logrotate-angie")
}

func TestAngieOpenSSLWarning(t *testing.T) {
	cases := []struct {
		name, out string
		warn      bool
	}{
		{"focal package", "Angie version: Angie/1.10.3\nbuilt with OpenSSL 1.1.1f  31 Mar 2020\n", true},
		{"openssl 3.0", "built with OpenSSL 3.0.13 30 Jan 2024\n", true},
		{"docker image", "Angie version: Angie/1.12.4\nbuilt with OpenSSL 3.5.9 1 Jul 2026\n", false},
		{"running newer than built", "built with OpenSSL 3.0.2 15 Mar 2022 (running with OpenSSL 3.5.1 1 Jul 2025)\n", false},
		{"running older than built", "built with OpenSSL 3.5.0 8 Apr 2025 (running with OpenSSL 3.4.0 22 Oct 2024)\n", true},
		{"openssl 4", "built with OpenSSL 4.0.0\n", false},
		{"no openssl line", "Angie version: Angie/1.12.4\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := angieOpenSSLWarning(tc.out)
			if (got != "") != tc.warn {
				t.Fatalf("warning = %q, want warn=%v", got, tc.warn)
			}
		})
	}
}

func TestFixAngieLogrotatePID(t *testing.T) {
	old := angieLogrotatePath
	t.Cleanup(func() { angieLogrotatePath = old })
	angieLogrotatePath = filepath.Join(t.TempDir(), "angie")

	if err := fixAngieLogrotatePID(); err != nil {
		t.Fatalf("missing file must be ignored: %v", err)
	}

	pkg := "/var/log/angie/*.log {\n\tpostrotate\n\t\tif [ -f /run/angie.pid ]; then\n\t\t\tkill -USR1 $(cat /run/angie.pid)\n\t\tfi\n\tendscript\n}\n"
	if err := os.WriteFile(angieLogrotatePath, []byte(pkg), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // idempotent
		if err := fixAngieLogrotatePID(); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := os.ReadFile(angieLogrotatePath)
	if strings.Contains(string(got), "/run/angie.pid") || strings.Count(string(got), edge.DefaultAngiePIDFile) != 2 {
		t.Fatalf("logrotate not pointed at %s:\n%s", edge.DefaultAngiePIDFile, got)
	}
}

func TestProbeSwitchHostReadyRetriesRefused(t *testing.T) {
	oldWait := switchProbeWait
	t.Cleanup(func() { switchProbeWait = oldWait })
	switchProbeWait = 5 * time.Second

	calls := 0
	ops := &edgeOps{probeSwitchHost: func(context.Context, string) error {
		calls++
		if calls < 3 {
			return fmt.Errorf("dial tcp 127.0.0.1:443: %w", syscall.ECONNREFUSED)
		}
		return nil
	}}
	if err := probeSwitchHostReady(context.Background(), ops, "example.com"); err != nil || calls != 3 {
		t.Fatalf("err=%v calls=%d, want success on the 3rd attempt", err, calls)
	}

	calls = 0
	other := errors.New("x509: certificate signed by unknown authority")
	ops.probeSwitchHost = func(context.Context, string) error { calls++; return other }
	if err := probeSwitchHostReady(context.Background(), ops, "example.com"); !errors.Is(err, other) || calls != 1 {
		t.Fatalf("err=%v calls=%d, want the non-refused error at once", err, calls)
	}
}
