package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nd/internal/client"
	"nd/internal/config"
	"nd/internal/edge"
)

type fakeEdgeCaddyRunner struct {
	validateErr error
	validateOut string
	reloadErr   error
	adminErr    error
	validated   []string
	reloaded    int
}

func (f *fakeEdgeCaddyRunner) Validate(_ context.Context, configPath string) (string, error) {
	f.validated = append(f.validated, configPath)
	if f.validateErr != nil {
		return f.validateOut, f.validateErr
	}
	return "Valid configuration", nil
}

func (f *fakeEdgeCaddyRunner) Reload(_ context.Context, _, _ string) (time.Duration, error) {
	f.reloaded++
	return 5 * time.Millisecond, f.reloadErr
}

func (f *fakeEdgeCaddyRunner) CheckAdmin(_ context.Context, _ string) error {
	return f.adminErr
}

type fakeEdgeSystemdRunner struct {
	caddyActive  bool
	ndEdgeActive bool
	caddyUser    string
}

func (f *fakeEdgeSystemdRunner) IsActive(_ context.Context, unit string) (bool, string, error) {
	if unit == "caddy.service" {
		if f.caddyActive {
			return true, "active", nil
		}
		return false, "inactive", errors.New("inactive")
	}
	if unit == "nd-edge.service" {
		if f.ndEdgeActive {
			return true, "active", nil
		}
		return false, "inactive", errors.New("inactive")
	}
	return false, "unknown", nil
}

func (f *fakeEdgeSystemdRunner) Start(_ context.Context, _ string) error { return nil }
func (f *fakeEdgeSystemdRunner) CaddyUser(_ context.Context) string {
	if f.caddyUser != "" {
		return f.caddyUser
	}
	return "caddy"
}
func (f *fakeEdgeSystemdRunner) JournalCaddyErrors(_ context.Context, _ string) ([]byte, error) {
	return nil, nil
}

func newTestEdgeOps(t *testing.T) (*edgeOps, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	root := t.TempDir()
	paths := edge.NewPaths(root)
	osRel := filepath.Join(root, "os-release")
	_ = os.WriteFile(osRel, []byte("ID=ubuntu\nID_LIKE=debian\n"), 0o644)

	var stdout, stderr bytes.Buffer
	ops := &edgeOps{
		paths:         paths,
		unitPath:      filepath.Join(root, "etc", "systemd", "system", "nd-edge.service"),
		binaryPath:    filepath.Join(root, "usr", "local", "bin", "nd"),
		osReleasePath: osRel,
		stdin:         strings.NewReader(""),
		stdout:        &stdout,
		stderr:        &stderr,
		caddy:         &fakeEdgeCaddyRunner{},
		systemd:       &fakeEdgeSystemdRunner{caddyActive: true, ndEdgeActive: true, caddyUser: "caddy"},
		httpClient:    &http.Client{Timeout: 5 * time.Second},
		now:           func() time.Time { return time.Date(2026, 11, 11, 6, 0, 0, 0, time.UTC) },
		getEUID:       func() int { return 0 },
		goos:          "linux",
		goarch:        "amd64",
		hasSystemd:    func() bool { return true },
		diskFreeBytes: func(string) (uint64, error) { return 10 << 30, nil },
		runCmd: func(_ context.Context, name string, args ...string) (string, error) {
			if name == "caddy" && len(args) > 0 && args[0] == "version" {
				return "v2.8.4 h1:abc\n", nil
			}
			if name == "ss" {
				return "State Recv-Q Send-Q Local Address:Port Peer Address:Port Process\n", nil
			}
			if name == "ufw" {
				return "Status: inactive\n", nil
			}
			if name == "firewall-cmd" {
				return "", errors.New("not running")
			}
			return "", nil
		},
		lookPath:       func(string) (string, error) { return "/usr/bin/caddy", nil },
		chownGroup:     func(string, string, bool) error { return nil },
		getenforce:     func() string { return "Disabled" },
		signalReload:   func(int) error { return nil },
		hostname:       func() (string, error) { return "shield-sg", nil },
		localPublicIPs: func() ([]string, []string) { return []string{"198.51.100.10"}, nil },
	}
	return ops, &stdout, &stderr
}

func TestEdgeArgParsingAndTokenResolution(t *testing.T) {
	opts, err := parseEdgeInstallArgs([]string{
		"--panel-url", "https://panel.example.com/",
		"--token-stdin",
		"--acme-email=ops@example.com",
		"--adopt-existing",
		"--panel-ca-sha256", "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"--insecure-http",
		"--dns-gate", "off",
		"--force",
	})
	if err != nil {
		t.Fatalf("parseEdgeInstallArgs failed: %v", err)
	}
	if opts.panelURL != "https://panel.example.com/" || !opts.tokenStdin || opts.acmeEmail != "ops@example.com" ||
		!opts.adoptExisting || !opts.insecureHTTP || !opts.dnsGateOff || !opts.force {
		t.Fatalf("unexpected parsed opts: %+v", opts)
	}

	if _, err := parseEdgeInstallArgs([]string{"--dns-gate", "maybe"}); err == nil {
		t.Fatalf("expected error on invalid --dns-gate")
	}
	if _, err := parseEdgeInstallArgs([]string{"--bogus"}); err == nil {
		t.Fatalf("expected error on unknown flag")
	}

	// Token resolution: --token-stdin
	tok, err := resolveEdgeToken("", true, strings.NewReader("  shld_secret_from_stdin\n"))
	if err != nil || tok != "shld_secret_from_stdin" {
		t.Fatalf("expected stdin token, got %q err=%v", tok, err)
	}

	// Mutual exclusion --token + --token-stdin
	if _, err := resolveEdgeToken("shld_a", true, strings.NewReader("shld_b")); err == nil {
		t.Fatalf("expected error when both --token and --token-stdin given")
	}

	// ND_EDGE_TOKEN fallback
	t.Setenv("ND_EDGE_TOKEN", "shld_from_env")
	tok, err = resolveEdgeToken("", false, strings.NewReader(""))
	if err != nil || tok != "shld_from_env" {
		t.Fatalf("expected env token, got %q err=%v", tok, err)
	}

	// URL validation: http:// requires --insecure-http
	var warnBuf bytes.Buffer
	if _, err := validatePanelURL("http://panel.local", false, &warnBuf); err == nil {
		t.Fatalf("expected http:// without --insecure-http to fail")
	}
	clean, err := validatePanelURL("http://panel.local/", true, &warnBuf)
	if err != nil || clean != "http://panel.local" {
		t.Fatalf("expected http:// with --insecure-http to succeed, got %q err=%v", clean, err)
	}
	if !strings.Contains(warnBuf.String(), "WARNING") {
		t.Fatalf("expected loud warning on --insecure-http, got %q", warnBuf.String())
	}
}

func TestEdgeSystemdUnitContent(t *testing.T) {
	unit := renderSystemdUnit("/usr/local/bin/nd")
	for _, want := range []string{
		"Description=NextDeploy edge sync",
		"After=network-online.target caddy.service",
		"Wants=network-online.target caddy.service",
		"ExecStart=/usr/local/bin/nd edge run",
		"ExecReload=/bin/kill -HUP $MAINPID",
		"Restart=always",
		"LimitNOFILE=65536",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit missing %q:\n%s", want, unit)
		}
	}
	if strings.Contains(unit, "Requires=caddy") {
		t.Errorf("unit must NOT use Requires=caddy:\n%s", unit)
	}
}

func TestEdgeCaddyfileLegacyStripAndAdoptExisting(t *testing.T) {
	paths := edge.NewPaths(t.TempDir())

	// 1. Empty Caddyfile -> writes nd template
	plan, err := prepareInstallCaddyfile("", paths, "ops@example.com", false)
	if err != nil {
		t.Fatalf("empty Caddyfile failed: %v", err)
	}
	if !strings.Contains(plan.Content, edge.ManagedStartMarker) || !strings.Contains(plan.Content, "email ops@example.com") {
		t.Fatalf("unexpected empty template:\n%s", plan.Content)
	}

	// 2. Legacy catch-all + on_demand_tls -> stripped cleanly without requiring --adopt-existing
	legacy := `{
    # Global On-Demand TLS with NextDeploy verification
    on_demand_tls {
        ask https://panel.example.com/api/v1/edge/verify-domain?token=shld_old
    }
}

# === NextDeploy Edge Shield Catch-all ===
https:// {
    tls {
        on_demand
    }
    reverse_proxy https://203.0.113.10:443 {
        header_up Host {host}
    }
}
http:// {
    redir https://{host}{uri} permanent
}
# === End NextDeploy Edge Shield Catch-all ===
`
	plan, err = prepareInstallCaddyfile(legacy, paths, "", false)
	if err != nil {
		t.Fatalf("legacy strip failed: %v", err)
	}
	if !plan.LegacyStripped || plan.HasUserSites {
		t.Fatalf("expected LegacyStripped=true HasUserSites=false, got %+v", plan)
	}
	if strings.Contains(plan.Content, "on_demand_tls") || strings.Contains(plan.Content, "Catch-all") {
		t.Fatalf("legacy content remained:\n%s", plan.Content)
	}

	// 3. Existing user sites without --adopt-existing -> error (E-INST-2)
	existingUser := `{
	admin 127.0.0.1:2020
	servers {
		trusted_proxies static 10.0.0.0/8
	}
}

blog.example.com {
	respond "hello" 200
}
`
	if _, err := prepareInstallCaddyfile(existingUser, paths, "", false); err == nil {
		t.Fatalf("expected error when existing user sites present without --adopt-existing")
	}

	// 4. Existing user sites WITH --adopt-existing -> preserved + custom admin + trusted_proxies warning
	plan, err = prepareInstallCaddyfile(existingUser, paths, "", true)
	if err != nil {
		t.Fatalf("adopt-existing failed: %v", err)
	}
	if !plan.HasUserSites || plan.AdminAddr != "127.0.0.1:2020" || !plan.SkipTrustedProxies {
		t.Fatalf("expected HasUserSites=true AdminAddr=127.0.0.1:2020 SkipTrustedProxies=true, got %+v", plan)
	}
	if len(plan.Warnings) == 0 {
		t.Fatalf("expected trusted_proxies warning")
	}
	if !strings.Contains(plan.Content, "blog.example.com") || !strings.Contains(plan.Content, "admin 127.0.0.1:2020") {
		t.Fatalf("adopted Caddyfile missing user site or custom admin:\n%s", plan.Content)
	}

	// 5. admin off -> abort
	if _, err := prepareInstallCaddyfile("{\n\tadmin off\n}\n", paths, "", true); err == nil {
		t.Fatalf("expected error for admin off")
	}
}

func TestEdgeInstallAddRemoveDoctorUpgradeUninstall(t *testing.T) {
	var routesShouldFail bool
	var unregistered []string
	var lastAuth string

	panelSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", time.Date(2026, 11, 11, 6, 0, 10, 0, time.UTC).Format(http.TimeFormat))
		w.Header().Set("X-ND-Edge-API", "1")
		lastAuth = r.Header.Get("Authorization")
		if lastAuth != "Bearer shld_secret123" && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodHead && r.URL.Path == "/":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/edge/register":
			_ = json.NewEncoder(w).Encode(edge.RegisterResponse{
				PanelID:         "panel-1111",
				PanelName:       "vps-1",
				ShieldNodeID:    3,
				PollIntervalSec: 30,
				APIVersion:      1,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/edge/routes":
			if routesShouldFail {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(edge.RoutesResponse{
				APIVersion: 1,
				PanelID:    "panel-1111",
				ETag:       "etag-v1",
				Origin: edge.OriginInfo{
					IPs:      []string{"203.0.113.50"},
					Port:     443,
					CAStatus: edge.CAStatusMissing,
				},
				Routes: []edge.EdgeRoute{
					{ID: "dom_1", Kind: edge.KindAppDomain, Hosts: []string{"app.example.com"}},
				},
				Node: edge.NodeInfo{ID: 3, IP: "198.51.100.10"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/edge/unregister":
			unregistered = append(unregistered, r.URL.Path)
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer panelSrv.Close()

	// 1. Port 80 conflict -> abort with process name (E-INST-1)
	opsConflict, _, _ := newTestEdgeOps(t)
	opsConflict.runCmd = func(_ context.Context, name string, args ...string) (string, error) {
		if name == "ss" {
			return `LISTEN 0 511 0.0.0.0:80 0.0.0.0:* users:(("nginx",pid=421,fd=6))`, nil
		}
		return "", nil
	}
	opsConflict.stdin = strings.NewReader("shld_secret123\n")
	err := runEdgeWithOps(opsConflict, []string{"install", "--panel-url", panelSrv.URL, "--token-stdin", "--insecure-http"})
	if err == nil || !strings.Contains(err.Error(), "nginx") {
		t.Fatalf("expected nginx port conflict error, got %v", err)
	}

	// 2. Full successful install with --adopt-existing and --token-stdin
	ops, stdout, stderr := newTestEdgeOps(t)
	userCaddy := "existing.example.com {\n\trespond \"ok\" 200\n}\n"
	if err := edge.WriteFileAtomic(ops.paths.Caddyfile, []byte(userCaddy), 0o644); err != nil {
		t.Fatalf("seed Caddyfile: %v", err)
	}
	ops.stdin = strings.NewReader("shld_secret123\n")
	if err := runEdgeWithOps(ops, []string{
		"install",
		"--panel-url", panelSrv.URL,
		"--token-stdin",
		"--insecure-http",
		"--adopt-existing",
		"--dns-gate", "off",
	}); err != nil {
		t.Fatalf("install failed: %v\nstderr=%s", err, stderr.String())
	}

	// Token must never be echoed to stdout/stderr.
	if strings.Contains(stdout.String(), "shld_secret123") || strings.Contains(stderr.String(), "shld_secret123") {
		t.Fatalf("secret token was echoed in output")
	}

	cfg, err := edge.LoadConfig(ops.paths.ConfigFile)
	if err != nil {
		t.Fatalf("LoadConfig after install: %v", err)
	}
	if len(cfg.Panels) != 1 || cfg.Panels[0].PanelID != "panel-1111" || !cfg.DNSGateOff {
		t.Fatalf("unexpected config after install: %+v", cfg)
	}
	if _, err := os.Stat(ops.unitPath); err != nil {
		t.Fatalf("expected systemd unit file at %s: %v", ops.unitPath, err)
	}

	// 3. Idempotent re-run with changed --acme-email preserves edge_id (E-INST-4)
	// and MUST NOT overwrite or create a second Caddyfile.nd-backup.* containing managed markers.
	origEdgeID := cfg.EdgeID
	ops.now = func() time.Time { return time.Date(2026, 11, 11, 6, 5, 0, 0, time.UTC) }
	ops.stdin = strings.NewReader("shld_secret123\n")
	if err := runEdgeWithOps(ops, []string{
		"install",
		"--panel-url", panelSrv.URL,
		"--token-stdin",
		"--insecure-http",
		"--acme-email", "ops2@example.com",
	}); err != nil {
		t.Fatalf("idempotent re-install failed: %v", err)
	}
	cfg2, _ := edge.LoadConfig(ops.paths.ConfigFile)
	if cfg2.EdgeID != origEdgeID {
		t.Fatalf("edge_id changed on idempotent re-install: %q != %q", cfg2.EdgeID, origEdgeID)
	}
	caddyDirEntries, _ := os.ReadDir(filepath.Dir(ops.paths.Caddyfile))
	backupCount := 0
	for _, e := range caddyDirEntries {
		if strings.HasPrefix(e.Name(), "Caddyfile.nd-backup.") {
			backupCount++
		}
	}
	if backupCount != 1 {
		t.Fatalf("expected exactly 1 original Caddyfile backup across idempotent re-install, got %d", backupCount)
	}

	// 4. add-panel aborts without saving when /routes fails (§4.2)
	routesShouldFail = true
	ops.stdin = strings.NewReader("shld_secret123\n")
	if err := runEdgeWithOps(ops, []string{"add-panel", "--panel-url", panelSrv.URL, "--token-stdin", "--insecure-http"}); err == nil {
		t.Fatalf("expected add-panel to fail when /routes returns 500")
	}
	routesShouldFail = false

	// 4. set-token, set-url, sync
	ops.stdin = strings.NewReader("shld_secret123\n")
	if err := runEdgeWithOps(ops, []string{"set-token", "vps-1", "--token-stdin"}); err != nil {
		t.Fatalf("set-token failed: %v", err)
	}
	if err := runEdgeWithOps(ops, []string{"set-url", "vps-1", "--panel-url", panelSrv.URL, "--insecure-http"}); err != nil {
		t.Fatalf("set-url failed: %v", err)
	}
	if err := runEdgeWithOps(ops, []string{"sync", "--panel", "vps-1", "--force"}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	// 5. list, status, doctor
	stdout.Reset()
	if err := runEdgeWithOps(ops, []string{"list"}); err != nil {
		t.Fatalf("edge list failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "vps-1") || strings.Contains(stdout.String(), "shld_secret123") {
		t.Fatalf("unexpected list output:\n%s", stdout.String())
	}

	stdout.Reset()
	if err := runEdgeWithOps(ops, []string{"status", "--json"}); err != nil {
		t.Fatalf("edge status failed: %v", err)
	}
	if !strings.Contains(stdout.String(), `"edge_id"`) {
		t.Fatalf("expected JSON status output, got:\n%s", stdout.String())
	}

	stdout.Reset()
	if err := runEdgeWithOps(ops, []string{"doctor"}); err != nil {
		t.Fatalf("edge doctor failed: %v\nstdout=%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), "[PASS] caddy-directives") {
		t.Fatalf("expected caddy-directives PASS in doctor:\n%s", stdout.String())
	}

	// 6. uninstall refuses while panels remain unless --all (E-INST-9)
	if err := runEdgeWithOps(ops, []string{"uninstall"}); err == nil {
		t.Fatalf("expected uninstall without --all to refuse while panel remains")
	}

	// 7. remove-panel unregisters & cleans panel state
	if err := runEdgeWithOps(ops, []string{"remove-panel", "vps-1"}); err != nil {
		t.Fatalf("remove-panel failed: %v", err)
	}
	if len(unregistered) == 0 {
		t.Fatalf("expected /unregister to be called on remove-panel")
	}
	cfgAfterRemove, _ := edge.LoadConfig(ops.paths.ConfigFile)
	if len(cfgAfterRemove.Panels) != 0 {
		t.Fatalf("expected 0 panels after remove-panel, got %d", len(cfgAfterRemove.Panels))
	}

	// 8. upgrade sha256 mismatch keeps old binary (E-INST-10)
	newBinContent := []byte("#!/bin/sh\necho 'nd version 9.9.9'\n")
	sum := sha256.Sum256(newBinContent)
	goodSHA := hex.EncodeToString(sum[:])

	dlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(newBinContent)
	}))
	defer dlSrv.Close()

	if err := os.MkdirAll(filepath.Dir(ops.binaryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ops.binaryPath, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := runEdgeWithOps(ops, []string{"upgrade", "--url", dlSrv.URL, "--sha256", strings.Repeat("0", 64)}); err == nil {
		t.Fatalf("expected upgrade with bad sha256 to fail")
	}
	if got, _ := os.ReadFile(ops.binaryPath); string(got) != "old-binary" {
		t.Fatalf("old binary was overwritten on sha256 mismatch")
	}

	// Valid upgrade with passing smoke test replaces binary atomically.
	origRunCmd := ops.runCmd
	ops.runCmd = func(ctx context.Context, name string, args ...string) (string, error) {
		if strings.Contains(name, ".nd-upgrade-") && len(args) > 0 && args[0] == "version" {
			return "nd version 9.9.9 (linux/amd64)\n", nil
		}
		return origRunCmd(ctx, name, args...)
	}
	if err := runEdgeWithOps(ops, []string{"upgrade", "--url", dlSrv.URL, "--sha256", goodSHA}); err != nil {
		t.Fatalf("valid upgrade failed: %v", err)
	}
	if got, _ := os.ReadFile(ops.binaryPath); !bytes.Equal(got, newBinContent) {
		t.Fatalf("binary not updated after valid upgrade")
	}

	// 9. uninstall restores adopted Caddyfile backup and removes nd directories
	if err := runEdgeWithOps(ops, []string{"uninstall", "--all"}); err != nil {
		t.Fatalf("uninstall failed: %v", err)
	}
	restored, err := os.ReadFile(ops.paths.Caddyfile)
	if err != nil {
		t.Fatalf("expected restored Caddyfile: %v", err)
	}
	if string(restored) != userCaddy {
		t.Fatalf("restored Caddyfile mismatch:\ngot:\n%s\nwant:\n%s", string(restored), userCaddy)
	}
	if _, err := os.Stat(ops.paths.ConfigFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected config.json removed on uninstall")
	}
}

func TestPhase0_ShieldDomainAndProxyFlags(t *testing.T) {
	var gotMCPPayload map[string]interface{}
	var gotProxyPayload map[string]interface{}
	var gotAddNode map[string]interface{}
	var deletedNodePath string
	var rotatedNodePath string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/shield-nodes":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": true,
				"nodes": []map[string]interface{}{
					{"id": 7, "name": "sg-edge", "ip": "198.51.100.7", "token": "shld_tok7", "status": "connected"},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/shield-nodes":
			_ = json.NewDecoder(r.Body).Decode(&gotAddNode)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":            true,
				"node":          map[string]interface{}{"id": 8, "name": "eu-edge", "ip": "203.0.113.8"},
				"setup_command": "curl -sSL https://panel.test/api/v1/edge/setup.sh?token=shld_8 | bash",
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/shield-nodes/8":
			deletedNodePath = r.URL.Path
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/shield-nodes/7":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/shield-nodes/7/rotate-token":
			rotatedNodePath = r.URL.Path
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":                    true,
				"token":                 "shld_new7",
				"token_prev_expires_at": "2026-11-11T06:10:00Z",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/mcp":
			var rpc struct {
				Params struct {
					Arguments map[string]interface{} `json:"arguments"`
				} `json:"params"`
			}
			_ = json.NewDecoder(r.Body).Decode(&rpc)
			gotMCPPayload = rpc.Params.Arguments
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"result": map[string]interface{}{
					"isError": false,
					"content": []map[string]string{{"text": `{"ok":true,"message":"added"}`}},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/proxy/routes":
			_ = json.NewDecoder(r.Body).Decode(&gotProxyPayload)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	cl := client.New(config.Config{ServerURL: ts.URL, Token: "api_tok"}, "sess")

	// 1. nd shield add / rm / update / rotate-token / setup-script
	if err := RunShield(cl, []string{"add", "eu-edge", "203.0.113.8"}); err != nil {
		t.Fatalf("shield add failed: %v", err)
	}
	if gotAddNode["name"] != "eu-edge" || gotAddNode["ip"] != "203.0.113.8" {
		t.Fatalf("unexpected shield add payload: %+v", gotAddNode)
	}
	if err := RunShield(cl, []string{"rm", "8"}); err != nil || deletedNodePath != "/api/v1/shield-nodes/8" {
		t.Fatalf("shield rm failed: %v path=%q", err, deletedNodePath)
	}
	if err := RunShield(cl, []string{"update", "7", "--name", "sg-edge-2"}); err != nil {
		t.Fatalf("shield update failed: %v", err)
	}
	if err := RunShield(cl, []string{"rotate-token", "7"}); err != nil || rotatedNodePath == "" {
		t.Fatalf("shield rotate-token failed: %v", err)
	}
	// Capture stdout for setup-script & shield help to verify token query param and reassign help text
	origStdout := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut
	errSetup := RunShield(cl, []string{"setup-script", "7", "--adopt-existing"})
	errHelp := RunShield(cl, []string{"--help"})
	_ = wOut.Close()
	os.Stdout = origStdout
	var captured bytes.Buffer
	_, _ = captured.ReadFrom(rOut)
	_ = rOut.Close()
	if errSetup != nil || errHelp != nil {
		t.Fatalf("shield setup-script/help failed: setup=%v help=%v", errSetup, errHelp)
	}
	outText := captured.String()
	if !strings.Contains(outText, "setup.sh?") || !strings.Contains(outText, "token=shld_tok7") || !strings.Contains(outText, "adopt=1") {
		t.Fatalf("expected setup.sh URL with token and adopt=1, got:\n%s", outText)
	}
	if !strings.Contains(outText, "reassign <source_node_id> [target_node_id]") {
		t.Fatalf("expected updated reassign help text, got:\n%s", outText)
	}

	// 2. nd domain add --shield 7 and --direct
	if err := RunDomain(cl, []string{"add", "myapp", "app.example.com", "--shield", "7"}); err != nil {
		t.Fatalf("domain add --shield failed: %v", err)
	}
	if gotMCPPayload["shield_node_id"] != float64(7) {
		t.Fatalf("expected shield_node_id=7 in domain_add payload, got %+v", gotMCPPayload)
	}
	if err := RunDomain(cl, []string{"add", "myapp", "direct.example.com", "--direct"}); err != nil {
		t.Fatalf("domain add --direct failed: %v", err)
	}
	if gotMCPPayload["shield_node_id"] != float64(0) || gotMCPPayload["direct"] != true {
		t.Fatalf("expected direct=true shield_node_id=0 in domain_add payload, got %+v", gotMCPPayload)
	}
	if err := RunDomain(cl, []string{"add", "myapp", "bad.example.com", "--shield", "7", "--direct"}); err == nil {
		t.Fatalf("expected error when both --shield and --direct passed to domain add")
	}

	// 3. nd proxy add --shield 7 and --direct
	if err := RunProxy(cl, []string{"add", "p.example.com", "http://10.0.0.5:8080", "--shield", "7"}); err != nil {
		t.Fatalf("proxy add --shield failed: %v", err)
	}
	if gotProxyPayload["shield_node_id"] != float64(7) {
		t.Fatalf("expected shield_node_id=7 in proxy add payload, got %+v", gotProxyPayload)
	}
	if err := RunProxy(cl, []string{"add", "d.example.com", "http://10.0.0.5:8080", "--direct"}); err != nil {
		t.Fatalf("proxy add --direct failed: %v", err)
	}
	if _, hasShield := gotProxyPayload["shield_node_id"]; !hasShield || gotProxyPayload["shield_node_id"] != nil {
		t.Fatalf("expected explicit nil shield_node_id for --direct proxy add, got %+v", gotProxyPayload)
	}
	if err := RunProxy(cl, []string{"add", "bad.example.com", "http://10.0.0.5:8080", "--shield", "7", "--direct"}); err == nil {
		t.Fatalf("expected error when both --shield and --direct passed to proxy add")
	}
}

func TestParseCaddyVersionOutputSkipsJSONLogLines(t *testing.T) {
	raw := "{\"level\":\"warn\",\"ts\":1794380479.562721,\"msg\":\"unable to determine directory for user configuration\"}\nv2.11.2 h1:abcdef\n"
	if got := parseCaddyVersionOutput(raw); got != "v2.11.2" {
		t.Fatalf("expected v2.11.2, got %q", got)
	}
}
