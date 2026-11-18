package angie

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

// A master started from a config without the same `pid` directive (e.g. an older bootstrap
// config) cannot be signalled; reload must fall back to restarting the container.
func TestReloadFallsBackToRestartOnPIDError(t *testing.T) {
	var restarts atomic.Int32
	d := New(WithRuntimeHooks(RuntimeHooks{
		Exec: func(context.Context, string, ...string) (string, error) {
			return `angie: [error] invalid PID number "" in "/run/angie/angie.pid"`, errors.New("exit status 1")
		},
		Restart:       func(context.Context, string) error { restarts.Add(1); return nil },
		EnsureNetwork: func(context.Context) error { return nil },
	}))
	if err := d.reloadContainer(context.Background()); err != nil {
		t.Fatalf("reloadContainer: %v", err)
	}
	if restarts.Load() != 1 {
		t.Fatalf("restarts = %d, want 1", restarts.Load())
	}
}

func TestReloadConfigErrorIsNotMaskedByRestart(t *testing.T) {
	var restarts atomic.Int32
	d := New(WithRuntimeHooks(RuntimeHooks{
		Exec: func(context.Context, string, ...string) (string, error) {
			return `angie: [emerg] unknown directive "bogus" in /etc/angie/nd/sites/angie.conf:3`, errors.New("exit status 1")
		},
		Restart:       func(context.Context, string) error { restarts.Add(1); return nil },
		EnsureNetwork: func(context.Context) error { return nil },
	}))
	if err := d.reloadContainer(context.Background()); err == nil {
		t.Fatal("expected config error to be returned")
	}
	if restarts.Load() != 0 {
		t.Fatalf("config errors must not trigger a restart (restarts=%d)", restarts.Load())
	}
}

// When the shared nd-proxy container still runs another engine (switch Ferron -> Angie), the
// in-container `angie -t` fails with a missing binary; that must trigger the throwaway-container
// fallback instead of failing the preflight.
func TestIsMissingBinaryError(t *testing.T) {
	yes := []string{
		`OCI runtime exec failed: exec failed: unable to start container process: exec: "angie": executable file not found in $PATH: unknown`,
		`kiri: cannot start container: angie: executable file not found in $PATH (exit status 127)`,
	}
	for _, s := range yes {
		if !isMissingBinaryError(s) {
			t.Errorf("expected missing-binary for %q", s)
		}
	}
	if isMissingBinaryError(`angie: [emerg] unknown directive "bogus" exit status 1`) {
		t.Error("config error misclassified as missing binary")
	}
}
