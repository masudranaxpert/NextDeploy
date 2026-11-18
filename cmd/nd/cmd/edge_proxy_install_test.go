package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nd/internal/edge"
)

type fakeEdgeAngieRunner struct {
	validateErr error
	validateOut string
	reloadErr   error
	aliveErr    error
	version     string
	versionErr  error
	validated   []string
	reloaded    int
	aliveChecks int
}

func (f *fakeEdgeAngieRunner) Validate(_ context.Context, mainConfig string) (string, error) {
	f.validated = append(f.validated, mainConfig)
	if f.validateErr != nil {
		return f.validateOut, f.validateErr
	}
	return "angie: configuration file syntax is ok", nil
}

func (f *fakeEdgeAngieRunner) Reload(_ context.Context) (time.Duration, error) {
	f.reloaded++
	return 5 * time.Millisecond, f.reloadErr
}

func (f *fakeEdgeAngieRunner) CheckAlive(_ context.Context) error {
	f.aliveChecks++
	return f.aliveErr
}

func (f *fakeEdgeAngieRunner) Version(_ context.Context) (string, error) {
	if f.versionErr != nil {
		return "", f.versionErr
	}
	if f.version != "" {
		return f.version, nil
	}
	return "1.12.4", nil
}

type fakeSwitchSystemdRunner struct {
	units     map[string]bool
	caddyUser string
}

func newFakeSwitchSystemdRunner() *fakeSwitchSystemdRunner {
	return &fakeSwitchSystemdRunner{
		units: map[string]bool{
			"caddy.service":   true,
			"angie.service":   false,
			"nd-edge.service": false,
		},
		caddyUser: "caddy",
	}
}

func (f *fakeSwitchSystemdRunner) IsActive(_ context.Context, unit string) (bool, string, error) {
	active := f.units[unit]
	if active {
		return true, "active", nil
	}
	return false, "inactive", errors.New("inactive")
}

func (f *fakeSwitchSystemdRunner) Start(_ context.Context, unit string) error {
	f.units[unit] = true
	return nil
}

func (f *fakeSwitchSystemdRunner) CaddyUser(_ context.Context) string {
	if f.caddyUser != "" {
		return f.caddyUser
	}
	return "caddy"
}

func (f *fakeSwitchSystemdRunner) JournalCaddyErrors(_ context.Context, _ string) ([]byte, error) {
	return nil, nil
}

func TestEdgeInstallProxyFlagParsing(t *testing.T) {
	// Default omits --proxy -> caddy.
	opts, err := parseEdgeInstallArgs([]string{"--panel-url", "https://panel.example.com", "--token-stdin"})
	if err != nil {
		t.Fatalf("parseEdgeInstallArgs default: %v", err)
	}
	eng, err := validateProxyInstallChoice(opts.proxy, opts.experimental, opts.adoptExisting)
	if err != nil || eng != edge.EngineCaddy {
		t.Fatalf("expected default proxy caddy, got %q err=%v", eng, err)
	}

	// --proxy angie and --proxy=angie
	for _, argv := range [][]string{
		{"--proxy", "angie"},
		{"--proxy=angie"},
	} {
		opts, err = parseEdgeInstallArgs(argv)
		if err != nil {
			t.Fatalf("parseEdgeInstallArgs(%v): %v", argv, err)
		}
		eng, err = validateProxyInstallChoice(opts.proxy, opts.experimental, opts.adoptExisting)
		if err != nil || eng != edge.EngineAngie {
			t.Fatalf("expected angie for %v, got %q err=%v", argv, eng, err)
		}
	}

	// --proxy angie with --adopt-existing must fail
	if _, err := validateProxyInstallChoice("angie", false, true); err == nil || !strings.Contains(err.Error(), "--adopt-existing") {
		t.Fatalf("expected --adopt-existing rejection for angie, got %v", err)
	}

	// --proxy ferron requires --experimental, and accepts ferron when --experimental is passed.
	if _, err := validateProxyInstallChoice("ferron", false, false); err == nil || !strings.Contains(err.Error(), "--experimental") {
		t.Fatalf("expected --experimental gate for ferron, got %v", err)
	}
	if eng, err := validateProxyInstallChoice("ferron", true, false); err != nil || eng != edge.EngineFerron {
		t.Fatalf("expected ferron with --experimental, got %q err=%v", eng, err)
	}
	if _, err := validateProxyInstallChoice("ferron", true, true); err == nil || !strings.Contains(err.Error(), "--adopt-existing") {
		t.Fatalf("expected --adopt-existing rejection for ferron, got %v", err)
	}

	// Invalid engine rejected
	if _, err := validateProxyInstallChoice("nginx", false, false); err == nil {
		t.Fatalf("expected invalid engine error")
	}
}

func TestBuildAngieRepoPlan(t *testing.T) {
	dir := t.TempDir()

	// 1. Debian 12
	debPath := filepath.Join(dir, "debian")
	_ = os.WriteFile(debPath, []byte("ID=debian\nVERSION_ID=\"12\"\nVERSION_CODENAME=bookworm\n"), 0o644)
	info, err := parseOSReleaseInfo(debPath)
	if err != nil {
		t.Fatalf("parse debian: %v", err)
	}
	plan, err := buildAngieRepoPlan(info)
	if err != nil {
		t.Fatalf("buildAngieRepoPlan debian: %v", err)
	}
	if !strings.Contains(plan.KeyURL, "https://angie.software/keys/angie-signing.gpg") ||
		!strings.Contains(plan.RepoContent, "https://download.angie.software/angie/debian/12 bookworm main") {
		t.Fatalf("unexpected debian repo plan: %+v", plan)
	}

	// 2. Ubuntu 22.04 and 24.04
	for _, tc := range []struct {
		ver      string
		codename string
	}{
		{"22.04", "jammy"},
		{"24.04", "noble"},
	} {
		ubuPath := filepath.Join(dir, "ubuntu-"+tc.ver)
		_ = os.WriteFile(ubuPath, []byte("ID=ubuntu\nID_LIKE=debian\nVERSION_ID=\""+tc.ver+"\"\nVERSION_CODENAME="+tc.codename+"\n"), 0o644)
		info, err = parseOSReleaseInfo(ubuPath)
		if err != nil {
			t.Fatalf("parse ubuntu %s: %v", tc.ver, err)
		}
		plan, err = buildAngieRepoPlan(info)
		if err != nil {
			t.Fatalf("buildAngieRepoPlan ubuntu %s: %v", tc.ver, err)
		}
		wantRepo := "https://download.angie.software/angie/ubuntu/" + tc.ver + " " + tc.codename + " main"
		if !strings.Contains(plan.RepoContent, wantRepo) {
			t.Fatalf("ubuntu %s missing repo %q in:\n%s", tc.ver, wantRepo, plan.RepoContent)
		}
	}

	// 3. Alpine
	alpPath := filepath.Join(dir, "alpine")
	_ = os.WriteFile(alpPath, []byte("ID=alpine\nVERSION_ID=3.21.2\n"), 0o644)
	info, err = parseOSReleaseInfo(alpPath)
	if err != nil {
		t.Fatalf("parse alpine: %v", err)
	}
	plan, err = buildAngieRepoPlan(info)
	if err != nil {
		t.Fatalf("buildAngieRepoPlan alpine: %v", err)
	}
	if !strings.Contains(plan.KeyURL, "angie-signing.rsa") ||
		plan.KeyDestPath != "/etc/apk/keys/angie-signing.rsa" ||
		!strings.Contains(plan.RepoContent, "https://download.angie.software/angie/alpine/v3.21/main") {
		t.Fatalf("unexpected alpine repo plan: %+v", plan)
	}

	// 4. RHEL / Alma / Rocky / Fedora
	for _, tc := range []struct {
		id       string
		idLike   string
		wantSlug string
	}{
		{"almalinux", "rhel fedora", "almalinux"},
		{"rhel", "fedora", "almalinux"},
		{"rocky", "rhel fedora", "rocky"},
		{"fedora", "", "fedora"},
	} {
		rhelPath := filepath.Join(dir, tc.id)
		_ = os.WriteFile(rhelPath, []byte("ID="+tc.id+"\nID_LIKE=\""+tc.idLike+"\"\nVERSION_ID=\"9\"\n"), 0o644)
		info, err = parseOSReleaseInfo(rhelPath)
		if err != nil {
			t.Fatalf("parse %s: %v", tc.id, err)
		}
		plan, err = buildAngieRepoPlan(info)
		if err != nil {
			t.Fatalf("buildAngieRepoPlan %s: %v", tc.id, err)
		}
		wantURL := "https://download.angie.software/angie/" + tc.wantSlug + "/$releasever/"
		if !strings.Contains(plan.RepoContent, wantURL) ||
			!strings.Contains(plan.RepoContent, "gpgkey=https://angie.software/keys/angie-signing.gpg.asc") {
			t.Fatalf("%s missing expected baseurl %q:\n%s", tc.id, wantURL, plan.RepoContent)
		}
	}

	// 5. Unsupported OS family
	unkPath := filepath.Join(dir, "arch")
	_ = os.WriteFile(unkPath, []byte("ID=arch\n"), 0o644)
	if _, err := parseOSReleaseInfo(unkPath); err == nil {
		t.Fatalf("expected unsupported OS error")
	}
}

func newMockInstallPanelServer(t *testing.T, token string, onRegister func(edge.RegisterRequest), onUnregister func()) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", time.Date(2026, 11, 11, 6, 0, 10, 0, time.UTC).Format(http.TimeFormat))
		w.Header().Set("X-ND-Edge-API", "1")
		if r.Method != http.MethodHead && r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodHead && r.URL.Path == "/":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/edge/register":
			var req edge.RegisterRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if onRegister != nil {
				onRegister(req)
			}
			_ = json.NewEncoder(w).Encode(edge.RegisterResponse{
				PanelID:         "panel-angie-1",
				PanelName:       "angie-panel",
				ShieldNodeID:    7,
				PollIntervalSec: 30,
				APIVersion:      1,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/edge/routes":
			_ = json.NewEncoder(w).Encode(edge.RoutesResponse{
				APIVersion: 1,
				PanelID:    "panel-angie-1",
				ETag:       "etag-a1",
				Origin: edge.OriginInfo{
					IPs:      []string{"203.0.113.70"},
					Port:     443,
					CAStatus: edge.CAStatusMissing,
				},
				Routes: []edge.EdgeRoute{
					{ID: "dom_1", Kind: edge.KindAppDomain, Hosts: []string{"angie.example.com"}},
				},
				Node: edge.NodeInfo{ID: 7, IP: "198.51.100.10"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/edge/unregister":
			if onUnregister != nil {
				onUnregister()
			}
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestEdgeInstallAngiePreflightRollbackAndHappyPath(t *testing.T) {
	const token = "shld_angie_secret"
	var regReq edge.RegisterRequest
	var unregistered bool
	panelSrv := newMockInstallPanelServer(t, token, func(r edge.RegisterRequest) {
		regReq = r
	}, func() {
		unregistered = true
	})

	// 1. If angie -t fails while Caddy holds port 80/443, Caddy MUST NOT be stopped.
	opsValFail, _, _ := newTestEdgeOps(t)
	opsValFail.angie = &fakeEdgeAngieRunner{validateErr: errors.New("bad directive")}
	var stoppedCaddyOnValFail bool
	opsValFail.runCmd = func(_ context.Context, name string, args ...string) (string, error) {
		if name == "ss" {
			return `LISTEN 0 511 0.0.0.0:443 0.0.0.0:* users:(("caddy",pid=101,fd=8))`, nil
		}
		if name == "systemctl" && len(args) >= 2 && args[0] == "stop" && args[1] == "caddy" {
			stoppedCaddyOnValFail = true
		}
		return "", nil
	}
	opsValFail.stdin = strings.NewReader(token + "\n")
	if err := runEdgeWithOps(opsValFail, []string{
		"install", "--proxy", "angie", "--panel-url", panelSrv.URL, "--token-stdin", "--insecure-http",
	}); err == nil || !strings.Contains(err.Error(), "angie -t failed") {
		t.Fatalf("expected angie -t failure, got %v", err)
	}
	if stoppedCaddyOnValFail {
		t.Fatalf("caddy was stopped before angie -t succeeded")
	}

	// 2. If systemctl enable --now angie fails after stopping conflicting Caddy, Caddy is restarted.
	opsStartFail, _, _ := newTestEdgeOps(t)
	opsStartFail.angie = &fakeEdgeAngieRunner{}
	var startFailCmds []string
	opsStartFail.runCmd = func(_ context.Context, name string, args ...string) (string, error) {
		cmd := name + " " + strings.Join(args, " ")
		startFailCmds = append(startFailCmds, cmd)
		if name == "ss" {
			return `LISTEN 0 511 0.0.0.0:80 0.0.0.0:* users:(("caddy",pid=101,fd=8))`, nil
		}
		if cmd == "systemctl enable --now angie" {
			return "start error", errors.New("exit 1")
		}
		return "", nil
	}
	opsStartFail.stdin = strings.NewReader(token + "\n")
	if err := runEdgeWithOps(opsStartFail, []string{
		"install", "--proxy", "angie", "--panel-url", panelSrv.URL, "--token-stdin", "--insecure-http",
	}); err == nil || !strings.Contains(err.Error(), "systemctl enable --now angie") {
		t.Fatalf("expected angie start failure, got %v", err)
	}
	joinedStartFail := strings.Join(startFailCmds, "\n")
	if !strings.Contains(joinedStartFail, "systemctl stop caddy") || !strings.Contains(joinedStartFail, "systemctl start caddy") {
		t.Fatalf("expected caddy stop + rollback start, got:\n%s", joinedStartFail)
	}

	// 3. Full happy path: angie not yet installed -> package install + marker + layout + panel sync + doctor + uninstall.
	ops, stdout, stderr := newTestEdgeOps(t)
	angieRunner := &fakeEdgeAngieRunner{version: "Angie/1.12.4"}
	ops.angie = angieRunner
	sysd := newFakeSwitchSystemdRunner()
	ops.systemd = sysd
	_ = os.WriteFile(ops.osReleasePath, []byte("ID=debian\nVERSION_ID=\"12\"\nVERSION_CODENAME=bookworm\n"), 0o644)
	ops.lookPath = func(bin string) (string, error) {
		if bin == "angie" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + bin, nil
	}

	var cmds []string
	ops.runCmd = func(_ context.Context, name string, args ...string) (string, error) {
		cmd := name + " " + strings.Join(args, " ")
		cmds = append(cmds, cmd)
		switch cmd {
		case "ss -ltnp":
			return `LISTEN 0 511 0.0.0.0:443 0.0.0.0:* users:(("caddy",pid=101,fd=8))`, nil
		case "systemctl stop caddy":
			sysd.units["caddy.service"] = false
		case "systemctl enable --now angie":
			sysd.units["angie.service"] = true
		case "systemctl disable --now angie":
			sysd.units["angie.service"] = false
		case "systemctl enable --now nd-edge":
			sysd.units["nd-edge.service"] = true
		case "systemctl disable --now nd-edge":
			sysd.units["nd-edge.service"] = false
		}
		return "", nil
	}

	ops.stdin = strings.NewReader(token + "\n")
	if err := runEdgeWithOps(ops, []string{
		"install",
		"--proxy", "angie",
		"--panel-url", panelSrv.URL,
		"--token-stdin",
		"--insecure-http",
		"--acme-email", "ops@example.com",
		"--dns-gate", "off",
	}); err != nil {
		t.Fatalf("install --proxy angie failed: %v\nstderr=%s", err, stderr.String())
	}

	if regReq.CaddyVer != "Angie/1.12.4" {
		t.Fatalf("expected Angie version reported during register, got %q", regReq.CaddyVer)
	}
	if _, err := os.Stat(edgeAngieInstalledMarker(ops.paths)); err != nil {
		t.Fatalf("expected angie-installed-by-nd marker: %v", err)
	}

	ep := ops.paths.EnginePathsFor(edge.EngineAngie)
	for _, p := range []string{
		ep.MainConfig,
		ep.GlobalFile,
		ep.CommonFile,
		filepath.Join(ep.UserDir, edge.PlaceholderAngieFileName),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected rendered Angie file %s: %v", p, err)
		}
	}

	panelsDir, err := edge.ReadDirLink(ep.PanelsLink)
	if err != nil {
		t.Fatalf("ReadDirLink(%s): %v", ep.PanelsLink, err)
	}
	panelConf := filepath.Join(panelsDir, "panel-angie-1.conf")
	panelBytes, err := os.ReadFile(panelConf)
	if err != nil {
		t.Fatalf("expected initial reconcile panel conf %s: %v", panelConf, err)
	}
	if !strings.Contains(string(panelBytes), "angie.example.com") {
		t.Fatalf("expected angie.example.com in %s:\n%s", panelConf, string(panelBytes))
	}

	cfg, err := edge.LoadConfig(ops.paths.ConfigFile)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.ProxyEngine != edge.EngineAngie {
		t.Fatalf("expected config proxy_engine=angie, got %q", cfg.ProxyEngine)
	}

	unitBytes, err := os.ReadFile(ops.unitPath)
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	if !strings.Contains(string(unitBytes), "After=network-online.target angie.service") ||
		!strings.Contains(string(unitBytes), "Wants=network-online.target angie.service") {
		t.Fatalf("expected nd-edge.service to depend on angie.service:\n%s", string(unitBytes))
	}

	joinedCmds := strings.Join(cmds, "\n")
	if !strings.Contains(joinedCmds, "bash -c ") || !strings.Contains(joinedCmds, "https://download.angie.software/angie/debian/12") {
		t.Fatalf("expected Angie apt install command, got:\n%s", joinedCmds)
	}
	if !strings.Contains(joinedCmds, "systemctl disable caddy") {
		t.Fatalf("expected conflicting caddy service to be disabled, got:\n%s", joinedCmds)
	}

	// Status JSON reports proxy_engine=angie & running=true
	stdout.Reset()
	if err := runEdgeWithOps(ops, []string{"status", "--json"}); err != nil {
		t.Fatalf("status --json: %v", err)
	}
	var statusPayload map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &statusPayload); err != nil {
		t.Fatalf("parse status json: %v\nraw=%s", err, stdout.String())
	}
	if statusPayload["proxy_engine"] != edge.EngineAngie || statusPayload["caddy_running"] != true {
		t.Fatalf("unexpected status payload: %v", statusPayload)
	}

	// Doctor passes Angie checks without any FAIL lines
	stdout.Reset()
	if err := runEdgeWithOps(ops, []string{"doctor"}); err != nil {
		t.Fatalf("doctor: %v\noutput:\n%s", err, stdout.String())
	}
	docOut := stdout.String()
	for _, want := range []string{"angie-binary", "angie.service", "angie-alive", "angie-config"} {
		if !strings.Contains(docOut, want) {
			t.Fatalf("expected %q in doctor output:\n%s", want, docOut)
		}
	}
	if strings.Contains(docOut, "[FAIL]") {
		t.Fatalf("unexpected FAIL in doctor output:\n%s", docOut)
	}

	// Uninstall --all removes Angie layout, marker, config, and unregisters.
	if err := runEdgeWithOps(ops, []string{"uninstall", "--all"}); err != nil {
		t.Fatalf("uninstall --all: %v", err)
	}
	if !unregistered {
		t.Fatalf("expected panel unregister on uninstall")
	}
	if _, err := os.Stat(ep.MainConfig); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected angie.conf removed on uninstall, got err=%v", err)
	}
	if _, err := os.Stat(ep.ManagedDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected %s removed on uninstall, got err=%v", ep.ManagedDir, err)
	}
	if _, err := os.Stat(ep.CertsDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected %s removed on uninstall --all, got err=%v", ep.CertsDir, err)
	}
}

func TestEdgeInstallFerronExperimentalLifecycle(t *testing.T) {
	const token = "shld_ferron_secret"
	var regReq edge.RegisterRequest
	var unregistered bool
	panelSrv := newMockInstallPanelServer(t, token, func(r edge.RegisterRequest) {
		regReq = r
	}, func() {
		unregistered = true
	})

	// 1. Omitting --experimental rejects --proxy ferron
	opsGate, _, _ := newTestEdgeOps(t)
	opsGate.stdin = strings.NewReader(token + "\n")
	if err := runEdgeWithOps(opsGate, []string{
		"install", "--proxy", "ferron", "--panel-url", panelSrv.URL, "--token-stdin", "--insecure-http",
	}); err == nil || !strings.Contains(err.Error(), "--experimental") {
		t.Fatalf("expected --experimental gate error, got %v", err)
	}

	// 2. Full install --proxy ferron --experimental -> marker, systemd unit+dropin, token, layout, doctor, uninstall
	ops, stdout, stderr := newTestEdgeOps(t)
	ferronRunner := &fakeEdgeAngieRunner{version: "3.0.0-rc.11"}
	ops.ferron = ferronRunner
	sysd := newFakeSwitchSystemdRunner()
	ops.systemd = sysd
	_ = os.WriteFile(ops.osReleasePath, []byte("ID=ubuntu\nID_LIKE=debian\nVERSION_ID=\"24.04\"\nVERSION_CODENAME=noble\n"), 0o644)
	ops.lookPath = func(bin string) (string, error) {
		if bin == "ferron" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + bin, nil
	}

	var cmds []string
	ops.runCmd = func(_ context.Context, name string, args ...string) (string, error) {
		cmd := name + " " + strings.Join(args, " ")
		cmds = append(cmds, cmd)
		switch cmd {
		case "ss -ltnp":
			return "State Recv-Q Send-Q Local Address:Port Peer Address:Port Process\n", nil
		case "systemctl enable --now ferron":
			sysd.units["ferron.service"] = true
		case "systemctl disable --now ferron":
			sysd.units["ferron.service"] = false
		case "systemctl enable --now nd-edge":
			sysd.units["nd-edge.service"] = true
		case "systemctl disable --now nd-edge":
			sysd.units["nd-edge.service"] = false
		}
		return "", nil
	}

	ops.stdin = strings.NewReader(token + "\n")
	if err := runEdgeWithOps(ops, []string{
		"install",
		"--proxy", "ferron",
		"--experimental",
		"--panel-url", panelSrv.URL,
		"--token-stdin",
		"--insecure-http",
		"--acme-email", "ops@example.com",
		"--dns-gate", "off",
	}); err != nil {
		t.Fatalf("install --proxy ferron --experimental failed: %v\nstderr=%s", err, stderr.String())
	}

	if regReq.CaddyVer != "3.0.0-rc.11" {
		t.Fatalf("expected ferron version reported on register, got %q", regReq.CaddyVer)
	}
	if _, err := os.Stat(edgeFerronInstalledMarker(ops.paths)); err != nil {
		t.Fatalf("expected ferron-installed-by-nd marker: %v", err)
	}
	if _, err := os.Stat(ferronAdminTokenPath(ops.paths)); err != nil {
		t.Fatalf("expected ferron-admin-token: %v", err)
	}

	ep := ops.paths.EnginePathsFor(edge.EngineFerron)
	unitDir := filepath.Dir(ops.unitPath)
	for _, p := range []string{
		ep.MainConfig,
		ep.GlobalFile,
		ep.CommonFile,
		ferronRootConfigPath(ep),
		filepath.Join(ep.UserDir, edge.PlaceholderFerronFileName),
		filepath.Join(unitDir, "ferron.service"),
		filepath.Join(unitDir, "ferron.service.d", "nd-edge.conf"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected rendered Ferron file %s: %v", p, err)
		}
	}

	ndUnit, _ := os.ReadFile(ops.unitPath)
	if !strings.Contains(string(ndUnit), "After=network-online.target ferron.service") {
		t.Fatalf("expected nd-edge.service dependency on ferron.service:\n%s", string(ndUnit))
	}

	cfg, _ := edge.LoadConfig(ops.paths.ConfigFile)
	if cfg == nil || cfg.EngineName() != edge.EngineFerron {
		t.Fatalf("expected proxy_engine=ferron in config, got %+v", cfg)
	}

	joinedCmds := strings.Join(cmds, "\n")
	if !strings.Contains(joinedCmds, "https://deb.ferron.sh/signing.pgp") || !strings.Contains(joinedCmds, "ferron3") {
		t.Fatalf("expected official Ferron Debian/Ubuntu package install command:\n%s", joinedCmds)
	}

	// Doctor checks pass for Ferron
	stdout.Reset()
	if err := runEdgeWithOps(ops, []string{"doctor"}); err != nil {
		t.Fatalf("doctor: %v\noutput:\n%s", err, stdout.String())
	}
	docOut := stdout.String()
	for _, want := range []string{"ferron-binary", "ferron.service", "ferron-alive", "ferron-config"} {
		if !strings.Contains(docOut, want) {
			t.Fatalf("expected %q in Ferron doctor output:\n%s", want, docOut)
		}
	}

	// Uninstall --all removes Ferron layout, shim, unit, and unregisters
	if err := runEdgeWithOps(ops, []string{"uninstall", "--all"}); err != nil {
		t.Fatalf("uninstall --all: %v", err)
	}
	if !unregistered {
		t.Fatalf("expected panel unregister on uninstall")
	}
	for _, gone := range []string{ep.MainConfig, ep.ManagedDir, ep.CertsDir, ferronRootConfigPath(ep), filepath.Join(unitDir, "ferron.service")} {
		if _, err := os.Stat(gone); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected %s removed on uninstall --all, got err=%v", gone, err)
		}
	}
}
