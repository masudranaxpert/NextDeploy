//go:build integration

package ferron

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panel/internal/db"
	"panel/internal/proxy"
	"panel/internal/proxy/state"
)

func requireDockerAndFerronImage(t *testing.T) {
	t.Helper()
	if out, err := exec.Command("docker", "version", "--format", "{{.Server.Version}}").CombinedOutput(); err != nil {
		t.Skipf("docker daemon unavailable: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	if err := exec.Command("docker", "image", "inspect", FerronImage).Run(); err != nil {
		if out, perr := exec.Command("docker", "pull", FerronImage).CombinedOutput(); perr != nil {
			t.Fatalf("failed to pull %s: %v\n%s", FerronImage, perr, string(out))
		}
	}
}

func stageFilesForValidate(t *testing.T, files proxy.Files) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", full, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	return dir
}

func TestIntegration_ValidateAllRenderedConfigsInDocker(t *testing.T) {
	requireDockerAndFerronImage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	drv := New(
		WithOnDemandAskURL("http://127.0.0.1:3000/api/internal/on-demand-ask"),
		WithImportedCerts(map[string]ManualCert{
			"imported.example.com": {CertPath: "/etc/nd-pki/imported/imported.crt", KeyPath: "/etc/nd-pki/imported/imported.key"},
		}),
	)

	// 1. Validate combined full multi-feature IR state.
	richFiles, err := drv.Render(buildRichTestState(t))
	if err != nil {
		t.Fatalf("Render(rich): %v", err)
	}
	if err := drv.Validate(ctx, stageFilesForValidate(t, richFiles)); err != nil {
		t.Fatalf("ferron validate failed on combined rich config: %v", err)
	}

	// 2. Validate individual feature states independently + state.Build IR.
	builtState, err := state.Build(state.BuildInput{
		PanelDomain:      "panel.nd.test",
		PanelEnableHTTPS: true,
		ACMEEmail:        "admin@nd.test",
		Apps:             []db.App{{ID: "a1", Name: "web", ActiveSlot: "blue"}},
		AppDomains:       []db.AppDomain{{ID: 1, AppID: "a1", Domain: "nd.test", Service: "web", Port: 3000, EnableHTTPS: true, EnableWWW: true}},
		AppTargets:       map[string]state.AppTarget{"a1": {ServiceContainers: map[string]string{"web": "nd-a1-blue"}}},
		ProxyRoutes: []db.ProxyRoute{
			{ID: 1, Kind: "redirect", Domain: "old.nd.test", Target: "https://nd.test", RedirectCode: 308, KeepPath: true, EnableHTTPS: true, Enabled: true},
			{ID: 2, Kind: "proxy", Domain: "ext.nd.test", Target: "https://partner.internal:8443", EnableHTTPS: true, SkipTLSVerify: true, Enabled: true},
		},
	})
	if err != nil {
		t.Fatalf("state.Build: %v", err)
	}

	subStates := []struct {
		name string
		st   *proxy.State
	}{
		{name: "state_build_end_to_end", st: builtState},
		{name: "imported_cert_override", st: &proxy.State{
			Sites: []proxy.Site{{
				ID:       "app:imp:dom:1",
				Hosts:    []string{"imported.example.com"},
				TLS:      proxy.TLSAuto,
				Kind:     proxy.SiteKindAppProxy,
				Upstream: &proxy.UpstreamRef{Name: "u1", Container: "imp-web", Port: 3000},
			}},
		}},
		{name: "wildcard_http_redirect_and_gateway", st: &proxy.State{
			Sites: []proxy.Site{{
				ID:       "route:w1",
				Hosts:    []string{"old.local"},
				TLS:      proxy.TLSNone,
				Kind:     proxy.SiteKindRedirect,
				Redirect: &proxy.RedirectSpec{Code: 301, Target: "https://new.local", KeepPath: false},
			}},
			Gateways: []proxy.GatewayPort{{
				Port: 8888, BindIP: "127.0.0.1", Protocol: "tcp",
				Upstream: &proxy.UpstreamRef{Container: "gw-app", Port: 8080},
			}},
		}},
	}
	for _, tc := range subStates {
		t.Run(tc.name, func(t *testing.T) {
			files, err := drv.Render(tc.st)
			if err != nil {
				t.Fatalf("Render(%s): %v", tc.name, err)
			}
			if err := drv.Validate(ctx, stageFilesForValidate(t, files)); err != nil {
				t.Fatalf("Validate(%s): %v", tc.name, err)
			}
		})
	}

	// 3. Negative checks: invalid directive args and syntax errors must fail Validate with diagnostics.
	badStatusDir := stageFilesForValidate(t, proxy.Files{"ferron.conf": "broken.example.com {\n    status 999\n}\n"})
	if err := drv.Validate(ctx, badStatusDir); err == nil {
		t.Fatalf("expected ferron validate to fail on invalid status 999")
	}
	badSyntaxDir := stageFilesForValidate(t, proxy.Files{"ferron.conf": "broken.example.com {\n    root \"/srv\"\n"})
	if err := drv.Validate(ctx, badSyntaxDir); err == nil {
		t.Fatalf("expected ferron validate to fail on unclosed block")
	}

	// 4. Verify rich config has zero "Unknown directive" diagnostics in raw JSON report.
	richDir := stageFilesForValidate(t, richFiles)
	absRich, _ := filepath.Abs(richDir)
	rawOut, rawErr := exec.CommandContext(ctx, "docker", "run", "--rm",
		"-v", absRich+":"+containerConfigRoot+"/sites:ro",
		FerronImage,
		"/usr/local/bin/ferron", "validate", "-c", containerLiveConfig, "--json", "--config-adapter", "ferronconf",
	).CombinedOutput()
	if rawErr != nil || strings.Contains(string(rawOut), "Unknown directive") {
		t.Fatalf("unexpected validation diagnostic in rich config (err=%v):\n%s", rawErr, string(rawOut))
	}
}

func TestIntegration_LiveContainerAdminReloadAndStatus(t *testing.T) {
	requireDockerAndFerronImage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	tmpBase := t.TempDir()
	stage, err := proxy.NewStageStore(proxy.StageConfig{
		BaseDir:         tmpBase,
		LinkName:        "sites",
		GenPrefix:       "sites.gen-",
		KeepGenerations: 3,
	})
	if err != nil {
		t.Fatalf("NewStageStore: %v", err)
	}

	renderDrv := New(WithAdminTokenEnv("ND_FERRON_ADMIN_TOKEN"))
	filesV1, err := renderDrv.Render(&proxy.State{
		Sites: []proxy.Site{{
			ID:       "route:live:1",
			Hosts:    []string{"v1.local"},
			TLS:      proxy.TLSNone,
			Kind:     proxy.SiteKindRedirect,
			Redirect: &proxy.RedirectSpec{Code: 302, Target: "https://v1.example.com", KeepPath: true},
		}},
	})
	if err != nil {
		t.Fatalf("Render v1: %v", err)
	}
	filesV2, err := renderDrv.Render(&proxy.State{
		Sites: []proxy.Site{
			{
				ID:       "route:live:1",
				Hosts:    []string{"v1.local"},
				TLS:      proxy.TLSNone,
				Kind:     proxy.SiteKindRedirect,
				Redirect: &proxy.RedirectSpec{Code: 302, Target: "https://v1.example.com", KeepPath: true},
			},
			{
				ID:       "route:live:2",
				Hosts:    []string{"v2.local"},
				TLS:      proxy.TLSNone,
				Kind:     proxy.SiteKindRedirect,
				Redirect: &proxy.RedirectSpec{Code: 301, Target: "https://v2.example.com", KeepPath: false},
			},
		},
	})
	if err != nil {
		t.Fatalf("Render v2: %v", err)
	}

	if _, target1, err := stage.Stage("v1", filesV1); err != nil {
		t.Fatalf("Stage v1: %v", err)
	} else if _, err := stage.Activate(target1); err != nil {
		t.Fatalf("Activate v1: %v", err)
	} else {
		_, readlinkErr := os.Readlink(stage.LinkPath())
		hostSymlinksSupported := readlinkErr == nil

		absBase, err := filepath.Abs(tmpBase)
		if err != nil {
			t.Fatal(err)
		}
		name := "nd-ferron-it-" + strings.ReplaceAll(time.Now().Format("150405.000"), ".", "")
		t.Cleanup(func() {
			_ = exec.Command("docker", "rm", "-f", name).Run()
		})

		token := "nd-it-admin-secret"
		if hostSymlinksSupported {
			cmd := exec.Command("docker", "run", "-d", "--name", name,
				"-e", "ND_FERRON_ADMIN_TOKEN="+token,
				"-v", absBase+":"+containerConfigRoot+":ro",
				FerronImage,
				"/usr/local/bin/ferron", "run", "-c", containerLiveConfig, "--config-adapter", "ferronconf",
			)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("start ferron container: %v\n%s", err, string(out))
			}
		} else {
			_, target2, err := stage.Stage("v2", filesV2)
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("docker", "run", "-d", "--name", name,
				"-e", "ND_FERRON_ADMIN_TOKEN="+token,
				"-v", absBase+":/mnt/host-ferron:ro",
				FerronImage,
				"/bin/sh", "-c",
				"mkdir -p "+containerConfigRoot+" && cp -r /mnt/host-ferron/"+target1+" /mnt/host-ferron/"+target2+" "+containerConfigRoot+"/ && ln -sfn "+target1+" "+containerConfigRoot+"/sites && exec /usr/local/bin/ferron run -c "+containerLiveConfig+" --config-adapter ferronconf",
			)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("start ferron container: %v\n%s", err, string(out))
			}
		}

		liveDrv := New(WithContainerName(name), WithDataDir(tmpBase), WithStageStore(stage))
		var st1 proxy.Status
		for i := 0; i < 30; i++ {
			st1, err = liveDrv.Status(ctx)
			if err == nil && st1.Running && st1.Version != "" {
				break
			}
			time.Sleep(150 * time.Millisecond)
		}
		if err != nil || !st1.Running {
			logs, _ := liveDrv.Logs(ctx, 50)
			t.Fatalf("ferron failed readiness check: st=%+v err=%v logs=%v", st1, err, logs)
		}

		// Verify Admin API enforces Bearer auth on /status.
		unauthOut, _ := exec.Command("docker", "exec", name, "/bin/sh", "-c",
			`printf 'GET /status HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n\r\n' | nc 127.0.0.1 8081`).Output()
		if !strings.Contains(string(unauthOut), "401") {
			t.Fatalf("expected 401 on unauthenticated /status, got: %s", string(unauthOut))
		}

		beforeReload, err := liveDrv.getReloadStatus(ctx)
		if err != nil {
			t.Fatalf("GET /reload before swap: %v", err)
		}

		if hostSymlinksSupported {
			res, err := liveDrv.Apply(ctx, filesV2)
			if err != nil || !res.Reloaded {
				t.Fatalf("live Apply v2 failed: res=%+v err=%v", res, err)
			}
		} else {
			if out, err := exec.Command("docker", "exec", name, "/bin/sh", "-c",
				"t2=$(ls -d "+containerConfigRoot+"/sites.gen-* | sort | tail -n1) && ln -sfn $(basename \"$t2\") "+containerConfigRoot+"/sites",
			).CombinedOutput(); err != nil {
				t.Fatalf("container symlink swap failed: %v\n%s", err, string(out))
			}
			if err := liveDrv.reloadAndVerify(ctx, true, beforeReload.ActiveGeneration); err != nil {
				t.Fatalf("reloadAndVerify after symlink swap failed: %v", err)
			}
		}

		afterReload, err := liveDrv.getReloadStatus(ctx)
		if err != nil || afterReload.ActiveGeneration <= beforeReload.ActiveGeneration || afterReload.LastReloadError != nil {
			t.Fatalf("unexpected GET /reload after reload: before=%+v after=%+v err=%v", beforeReload, afterReload, err)
		}
		code, statusBody, err := liveDrv.doContainerExecAdminRequest(ctx, http.MethodGet, "/status")
		if err != nil || code != http.StatusOK || !strings.Contains(string(statusBody), `"reloads"`) {
			t.Fatalf("GET /status check failed: code=%d err=%v body=%s", code, err, string(statusBody))
		}
	}
}
