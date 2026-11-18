package angie

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"panel/internal/proxy"
)

func TestValidateChecks(t *testing.T) {
	d := New()
	if err := d.Validate(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty stagedDir")
	}
	emptyDir := t.TempDir()
	if err := d.Validate(context.Background(), emptyDir); err == nil {
		t.Fatal("expected error when angie.conf is missing")
	}

	dataDir := t.TempDir()
	stagedDir := filepath.Join(dataDir, "sites.gen-1")
	if err := os.MkdirAll(stagedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagedDir, "angie.conf"), []byte("events{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var execArgs []string
	dRunning := New(
		WithDataDir(dataDir),
		WithRuntimeHooks(RuntimeHooks{
			Inspect: func(context.Context, string) (ContainerInspectResult, error) {
				return ContainerInspectResult{Exists: true, Running: true, Status: "running"}, nil
			},
			Exec: func(_ context.Context, container string, args ...string) (string, error) {
				execArgs = append([]string{container}, args...)
				return "syntax is ok", nil
			},
		}),
	)
	if err := dRunning.Validate(context.Background(), stagedDir); err != nil {
		t.Fatalf("Validate inside running container: %v", err)
	}
	want := "nd-proxy angie -t -c /etc/angie/nd/sites.gen-1/angie.conf"
	if got := strings.Join(execArgs, " "); got != want {
		t.Fatalf("exec args = %q, want %q", got, want)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := dRunning.Validate(canceled, stagedDir); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestApplyStandaloneAndRollback(t *testing.T) {
	baseDir := t.TempDir()
	stage, err := proxy.NewStageStore(proxy.StageConfig{BaseDir: baseDir, KeepGenerations: 2})
	if err != nil {
		t.Fatal(err)
	}

	var failValidate atomic.Bool
	var failReload atomic.Bool
	var reloadCalls atomic.Int32

	d := New(
		WithDataDir(baseDir),
		WithStageStore(stage),
		WithRuntimeHooks(RuntimeHooks{
			RunValidate: func(_ context.Context, stagedDir string) error {
				if failValidate.Load() {
					return errors.New("bad directive")
				}
				return nil
			},
			Reload: func(context.Context, string) error {
				reloadCalls.Add(1)
				if failReload.Load() {
					return errors.New("reload signal failed")
				}
				return nil
			},
		}),
	)

	if _, err := d.Apply(context.Background(), nil); err == nil {
		t.Fatal("expected error for empty files")
	}

	v1 := proxy.Files{"angie.conf": "# v1\n", "00-global.conf": "send_timeout 60s;\n"}
	res1, err := d.Apply(context.Background(), v1)
	if err != nil || !res1.Reloaded || !res1.Changed {
		t.Fatalf("Apply v1 failed: res=%+v err=%v", res1, err)
	}
	link1, err := proxy.ReadDirLink(stage.LinkPath())
	if err != nil {
		t.Fatal(err)
	}

	// Validation failure must reject and keep v1 live
	failValidate.Store(true)
	vBad := proxy.Files{"angie.conf": "# bad\n"}
	if _, err := d.Apply(context.Background(), vBad); err == nil {
		t.Fatal("expected validation failure")
	}
	if got, _ := proxy.ReadDirLink(stage.LinkPath()); got != link1 {
		t.Fatalf("active link changed on validation error: got %q, want %q", got, link1)
	}
	failValidate.Store(false)

	// Reload failure must roll back symlink to v1
	failReload.Store(true)
	v2 := proxy.Files{"angie.conf": "# v2\n", "00-global.conf": "send_timeout 30s;\n"}
	res2, err := d.Apply(context.Background(), v2)
	if err == nil || !res2.RolledBack {
		t.Fatalf("expected RolledBack=true on reload failure, got res=%+v err=%v", res2, err)
	}
	if got, _ := proxy.ReadDirLink(stage.LinkPath()); got != link1 {
		t.Fatalf("active link not rolled back to %q, got %q", link1, got)
	}
}

func TestSyncerWithAngieDriverNoDoubleSwapAndHealthRollback(t *testing.T) {
	for _, tc := range []struct {
		name           string
		passStageToSnc bool
	}{
		{name: "driver_owns_staging", passStageToSnc: false},
		{name: "syncer_owns_staging", passStageToSnc: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseDir := t.TempDir()
			stage, err := proxy.NewStageStore(proxy.StageConfig{BaseDir: baseDir, KeepGenerations: 3})
			if err != nil {
				t.Fatal(err)
			}
			var reloads atomic.Int32
			var failHealth atomic.Bool
			domain := "one.example.com"

			d := New(
				WithDataDir(baseDir),
				WithStageStore(stage),
				WithRuntimeHooks(RuntimeHooks{
					RunValidate: func(_ context.Context, stagedDir string) error {
						if _, rerr := os.Stat(filepath.Join(stagedDir, "angie.conf")); rerr != nil {
							return rerr
						}
						return nil
					},
					Reload: func(context.Context, string) error {
						reloads.Add(1)
						return nil
					},
				}),
			)

			cfg := proxy.SyncerConfig{
				Driver: d,
				BuildState: func(context.Context) (*proxy.State, error) {
					return &proxy.State{
						Sites: []proxy.Site{
							{
								ID:       "app:1:dom:1",
								Hosts:    []string{domain},
								TLS:      proxy.TLSNone,
								Kind:     proxy.SiteKindAppProxy,
								Upstream: &proxy.UpstreamRef{Name: "nd_1_web_3000", Port: 3000},
							},
						},
					}, nil
				},
				HealthCheck: func(context.Context, proxy.Driver, *proxy.State, proxy.ApplyResult) error {
					if failHealth.Load() {
						return errors.New("health check 502")
					}
					return nil
				},
			}
			if tc.passStageToSnc {
				cfg.Stage = stage
			}
			s, err := proxy.NewSyncer(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()

			res1, err := s.SyncNow(context.Background(), proxy.Reason{Kind: "init"})
			if err != nil || !res1.Reloaded {
				t.Fatalf("SyncNow v1: res=%+v err=%v", res1, err)
			}
			v1Target, err := proxy.ReadDirLink(stage.LinkPath())
			if err != nil {
				t.Fatal(err)
			}
			if reloads.Load() != 1 {
				t.Fatalf("expected 1 reload, got %d", reloads.Load())
			}

			// Trigger health failure on v2 -> automatic rollback to v1Target + reload
			domain = "two.example.com"
			failHealth.Store(true)
			res2, err := s.SyncNow(context.Background(), proxy.Reason{Kind: "update"})
			if err == nil || !res2.RolledBack {
				t.Fatalf("expected health failure rollback, got res=%+v err=%v", res2, err)
			}
			afterTarget, err := proxy.ReadDirLink(stage.LinkPath())
			if err != nil {
				t.Fatal(err)
			}
			if tc.passStageToSnc {
				if afterTarget != v1Target {
					t.Fatalf("expected active symlink rolled back to %q, got %q", v1Target, afterTarget)
				}
			} else {
				_ = v1Target
				activeConf, rerr := os.ReadFile(filepath.Join(baseDir, afterTarget, "sites", SafeFileStem("app:1:dom:1")+".conf"))
				if rerr != nil {
					t.Fatal(rerr)
				}
				if !strings.Contains(string(activeConf), "one.example.com") || strings.Contains(string(activeConf), "two.example.com") {
					t.Fatalf("expected rolled back config for one.example.com, got:\n%s", string(activeConf))
				}
			}
		})
	}
}

func TestStatusLogsAndRestart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"angie": {"version":"1.12.4","generation":3,"load_time":"2026-11-18T07:02:07.875Z"},
			"connections": {"accepted":12,"dropped":0,"active":4,"idle":2},
			"http": {"server_zones": {"nd_a": {"requests":{"total":5,"processing":2,"discarded":0}}}}
		}`))
	}))
	defer srv.Close()

	var restarted atomic.Bool
	var ensuredNet atomic.Bool
	var gotTail int

	d := New(
		WithStatusURL(srv.URL),
		WithRuntimeHooks(RuntimeHooks{
			Inspect: func(context.Context, string) (ContainerInspectResult, error) {
				return ContainerInspectResult{Exists: true, Running: true, Status: "running"}, nil
			},
			Logs: func(_ context.Context, _ string, tail int) (string, error) {
				gotTail = tail
				return "line1\r\nline2\n\n", nil
			},
			Restart: func(_ context.Context, container string) error {
				if container != "nd-proxy" {
					return errors.New("unexpected container")
				}
				restarted.Store(true)
				return nil
			},
			EnsureNetwork: func(context.Context) error {
				ensuredNet.Store(true)
				return nil
			},
		}),
	)

	st, err := d.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.Running || st.Version != "1.12.4" || st.Generation != "3" || st.Connections != 4 || st.ActiveRequests != 2 {
		t.Fatalf("unexpected Status: %+v", st)
	}
	if st.LastReloadAt == nil || !st.LastReloadAt.Equal(time.Date(2026, 11, 18, 7, 2, 7, 875000000, time.UTC)) {
		t.Fatalf("unexpected LastReloadAt: %v", st.LastReloadAt)
	}
	if !strings.Contains(st.RawConfig, `"generation": 3`) {
		t.Fatalf("expected pretty RawConfig, got %q", st.RawConfig)
	}

	lines, err := d.Logs(context.Background(), 0)
	if err != nil || gotTail != 300 || len(lines) != 2 || lines[0] != "line1" || lines[1] != "line2" {
		t.Fatalf("Logs(0): tail=%d lines=%v err=%v", gotTail, lines, err)
	}
	if _, err := d.Logs(context.Background(), 99999); err != nil || gotTail != 2000 {
		t.Fatalf("Logs clamp: tail=%d err=%v", gotTail, err)
	}

	if err := d.Restart(context.Background()); err != nil || !restarted.Load() || !ensuredNet.Load() {
		t.Fatalf("Restart: err=%v restarted=%v net=%v", err, restarted.Load(), ensuredNet.Load())
	}
}
