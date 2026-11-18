package ferron

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

	"panel/internal/dockerapi"
	"panel/internal/proxy"
)

func TestApply_HappyPathAtomicSymlinksAndReload(t *testing.T) {
	dataDir := t.TempDir()
	var activeGen atomic.Uint64
	activeGen.Store(1)
	var reloadCalls atomic.Int32

	d := New(
		WithDataDir(dataDir),
		WithRuntimeHooks(RuntimeHooks{
			ValidateFunc: func(_ context.Context, stagedDir string) error {
				if _, err := os.Stat(filepath.Join(stagedDir, "ferron.conf")); err != nil {
					return err
				}
				return nil
			},
			AdminRequestFunc: func(_ context.Context, method, path string) (int, []byte, error) {
				switch method + " " + path {
				case "GET /reload":
					return http.StatusOK, []byte(`{"last_reload_time":"2026-11-18T12:00:00Z","last_reload_error":null,"active_generation":` +
						strconvFormatUint(activeGen.Load()) + `}`), nil
				case "POST /reload":
					reloadCalls.Add(1)
					activeGen.Add(1)
					return http.StatusOK, []byte(`{"status":"reload_initiated","error":null}`), nil
				default:
					return http.StatusNotFound, nil, nil
				}
			},
		}),
	)

	res, err := d.Apply(context.Background(), proxy.Files{
		"ferron.conf":     "{ strict_hosts }\ninclude \"sites/*.conf\"\n",
		"sites/00-a.conf": "a.example.com { root \"/srv/a\" }\n",
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !res.Changed || !res.Reloaded || res.RolledBack || res.Driver != "ferron" {
		t.Fatalf("unexpected ApplyResult: %+v", res)
	}
	if reloadCalls.Load() != 1 || activeGen.Load() != 2 {
		t.Fatalf("expected 1 reload and gen 2, got calls=%d gen=%d", reloadCalls.Load(), activeGen.Load())
	}

	curTarget, err := proxy.ReadDirLink(filepath.Join(dataDir, "sites"))
	if err != nil || filepath.IsAbs(curTarget) || !strings.HasPrefix(curTarget, "sites.gen-") {
		t.Fatalf("expected relative sites symlink to sites.gen-*, got %q (err=%v)", curTarget, err)
	}
	data, err := os.ReadFile(filepath.Join(dataDir, curTarget, "ferron.conf"))
	if err != nil || !strings.Contains(string(data), "strict_hosts") {
		t.Fatalf("reading ferron.conf through generation dir %q failed: %v", curTarget, err)
	}
}

func TestApply_ValidateFailureAbortsBeforeSwap(t *testing.T) {
	dataDir := t.TempDir()
	var failValidate atomic.Bool
	var activeGen atomic.Uint64
	activeGen.Store(1)

	d := New(
		WithDataDir(dataDir),
		WithRuntimeHooks(RuntimeHooks{
			ValidateFunc: func(_ context.Context, _ string) error {
				if failValidate.Load() {
					return errors.New("syntax error on line 3")
				}
				return nil
			},
			AdminRequestFunc: func(_ context.Context, method, path string) (int, []byte, error) {
				if method == http.MethodPost && path == "/reload" {
					activeGen.Add(1)
					return http.StatusOK, []byte(`{"status":"reload_initiated","error":null}`), nil
				}
				return http.StatusOK, []byte(`{"last_reload_time":"2026-11-18T12:00:00Z","last_reload_error":null,"active_generation":` +
					strconvFormatUint(activeGen.Load()) + `}`), nil
			},
		}),
	)

	v1 := proxy.Files{"ferron.conf": "# v1\n", "sites/a.conf": "a.example.com { root \"/srv/v1\" }\n"}
	if _, err := d.Apply(context.Background(), v1); err != nil {
		t.Fatalf("initial Apply: %v", err)
	}
	v1Link, _ := proxy.ReadDirLink(filepath.Join(dataDir, "sites"))

	failValidate.Store(true)
	v2 := proxy.Files{"ferron.conf": "# v2\n", "sites/a.conf": "bad {"}
	res, err := d.Apply(context.Background(), v2)
	if err == nil || res.Reloaded || res.RolledBack {
		t.Fatalf("expected validate failure with Reloaded=false RolledBack=false, got res=%+v err=%v", res, err)
	}
	afterLink, _ := proxy.ReadDirLink(filepath.Join(dataDir, "sites"))
	if afterLink != v1Link {
		t.Fatalf("sites symlink changed on validate failure: %q -> %q", v1Link, afterLink)
	}
}

func TestApply_ReloadFailureAndErrorRollsBackAndReloadsPrev(t *testing.T) {
	for _, tc := range []struct {
		name      string
		postCode  int
		postBody  string
		getErrStr string
		bumpGen   bool
	}{
		{
			name:     "post_reload_500",
			postCode: http.StatusInternalServerError,
			postBody: `{"status":"reload_failed","error":"bind failed"}`,
		},
		{
			name:      "get_reload_last_reload_error",
			postCode:  http.StatusOK,
			postBody:  `{"status":"reload_initiated","error":null}`,
			getErrStr: `"invalid upstream URL"`,
			bumpGen:   false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			var activeGen atomic.Uint64
			activeGen.Store(1)
			var failNext atomic.Bool
			var postCalls atomic.Int32
			lastErrJSON := "null"

			d := New(
				WithDataDir(dataDir),
				WithRuntimeHooks(RuntimeHooks{
					ValidateFunc: func(_ context.Context, _ string) error { return nil },
					AdminRequestFunc: func(_ context.Context, method, path string) (int, []byte, error) {
						if method == http.MethodGet && path == "/reload" {
							return http.StatusOK, []byte(`{"last_reload_time":"2026-11-18T12:00:00Z","last_reload_error":` +
								lastErrJSON + `,"active_generation":` + strconvFormatUint(activeGen.Load()) + `}`), nil
						}
						if method == http.MethodPost && path == "/reload" {
							postCalls.Add(1)
							if failNext.Load() {
								failNext.Store(false)
								if tc.getErrStr != "" {
									lastErrJSON = tc.getErrStr
								}
								if tc.bumpGen {
									activeGen.Add(1)
								}
								return tc.postCode, []byte(tc.postBody), nil
							}
							lastErrJSON = "null"
							activeGen.Add(1)
							return http.StatusOK, []byte(`{"status":"reload_initiated","error":null}`), nil
						}
						return http.StatusNotFound, nil, nil
					},
				}),
			)

			if _, err := d.Apply(context.Background(), proxy.Files{"ferron.conf": "# v1\n"}); err != nil {
				t.Fatalf("v1 Apply: %v", err)
			}
			v1Link, _ := proxy.ReadDirLink(filepath.Join(dataDir, "sites"))

			failNext.Store(true)
			res, err := d.Apply(context.Background(), proxy.Files{"ferron.conf": "# v2\n"})
			if err == nil || res.Reloaded || !res.RolledBack {
				t.Fatalf("expected rollback failure result, got res=%+v err=%v", res, err)
			}
			curLink, _ := proxy.ReadDirLink(filepath.Join(dataDir, "sites"))
			if curLink != v1Link {
				t.Fatalf("expected symlink rolled back to %q, got %q", v1Link, curLink)
			}
			if postCalls.Load() != 3 {
				t.Fatalf("expected 3 POST /reload calls (v1, failed v2, rollback v1), got %d", postCalls.Load())
			}
		})
	}
}

func TestStatusLogsRestartAndValidateJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" && r.Header.Get("Authorization") != "Bearer tok123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte("OK"))
		case "/status":
			_, _ = w.Write([]byte(`{"uptime_sec":42,"connections_active":7,"requests_total":100,"reloads":2,"config_drift":false}`))
		case "/reload":
			_, _ = w.Write([]byte(`{"last_reload_time":"2026-11-18T12:00:00Z","last_reload_error":null,"active_generation":3}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	var restarted bool
	d := New(
		WithAdminURL(srv.URL),
		WithAdminToken("tok123"),
		WithRuntimeHooks(RuntimeHooks{
			InspectContainerFunc: func(_ context.Context, _ string) (dockerapi.ContainerReadinessInfo, error) {
				return dockerapi.ContainerReadinessInfo{Running: true}, nil
			},
			FetchLogsFunc: func(_ context.Context, _ string, lines int) (string, error) {
				if lines != 200 {
					t.Fatalf("expected clamped default lines=200, got %d", lines)
				}
				return "line1\r\nline2\n\nline3\n", nil
			},
			RestartFunc: func(_ context.Context, _ string) error {
				restarted = true
				return nil
			},
		}),
	)

	st, err := d.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.Running || !st.Experimental || st.Connections != 7 || st.Generation != "3" || st.Version != "3.0.0-rc.11" || st.LastReloadAt == nil {
		t.Fatalf("unexpected Status: %+v", st)
	}

	logs, err := d.Logs(context.Background(), 0)
	if err != nil || len(logs) != 3 || logs[0] != "line1" {
		t.Fatalf("unexpected Logs: %v, err=%v", logs, err)
	}
	if err := d.Restart(context.Background()); err != nil || !restarted {
		t.Fatalf("Restart failed: restarted=%v err=%v", restarted, err)
	}

	rawDiag := []byte(`{"valid":false,"diagnostics":[{"kind":"Invalid configuration","message":"Bad directive","span":{"line":5,"column":3,"file":"/etc/ferron/nd/ferron.conf"}}]}`)
	verr := parseValidateOutput(rawDiag, errors.New("exit status 1"))
	if verr == nil || !strings.Contains(verr.Error(), "Bad directive") || !strings.Contains(verr.Error(), ":5:3)") {
		t.Fatalf("expected formatted validate error, got %v", verr)
	}
	if okErr := parseValidateOutput([]byte(`{"valid":true,"diagnostics":[]}`), nil); okErr != nil {
		t.Fatalf("expected valid config to succeed, got %v", okErr)
	}
}

func strconvFormatUint(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
