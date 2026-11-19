package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nd/internal/edge"
)

type fakeEdgeRunner struct {
	validateErr error
	reloadErr   error
	validated   []string
	reloaded    int
}

func (f *fakeEdgeRunner) Validate(_ context.Context, mainConfig string) (string, error) {
	f.validated = append(f.validated, mainConfig)
	if f.validateErr != nil {
		return "validation failed", f.validateErr
	}
	return "syntax is ok", nil
}

func (f *fakeEdgeRunner) Reload(_ context.Context) (time.Duration, error) {
	f.reloaded++
	return time.Millisecond, f.reloadErr
}

func (f *fakeEdgeRunner) CheckAlive(_ context.Context) error { return nil }
func (f *fakeEdgeRunner) Version(_ context.Context) (string, error) {
	return "1.12.4", nil
}

type fakeEdgeSystemdMulti struct {
	fakeEdgeSystemdRunner
	activeUnits map[string]bool
}

func (f *fakeEdgeSystemdMulti) IsActive(ctx context.Context, unit string) (bool, string, error) {
	if f.activeUnits != nil {
		if a, ok := f.activeUnits[unit]; ok {
			if a {
				return true, "active", nil
			}
			return false, "inactive", errors.New("inactive")
		}
	}
	return f.fakeEdgeSystemdRunner.IsActive(ctx, unit)
}

func TestEdgeDomainDocsCoverEverySubcommand(t *testing.T) {
	var top bytes.Buffer
	printEdgeCommandHelp(&top, "domain")
	for _, sub := range edgeDomainOrder {
		doc, ok := edgeDomainDocs[sub]
		if !ok || doc.Summary == "" || doc.Usage == "" {
			t.Fatalf("missing help doc for domain subcommand %q", sub)
		}
		if !strings.Contains(top.String(), sub) {
			t.Errorf("nd edge domain --help does not list %q", sub)
		}
		var b bytes.Buffer
		printEdgeCommandHelp(&b, "domain "+sub)
		if !strings.Contains(b.String(), "Usage: nd edge domain "+sub) {
			t.Errorf("help for domain %q lacks Usage line:\n%s", sub, b.String())
		}
		for _, fl := range doc.Flags {
			flagName := strings.Fields(fl[0])[0]
			if !strings.Contains(doc.Usage, flagName) {
				t.Errorf("domain %s: flag %s documented in Flags but missing from Usage", sub, flagName)
			}
		}
	}
	if len(edgeDomainDocs) != len(edgeDomainOrder) {
		t.Errorf("%d domain subcommands documented vs %d in edgeDomainOrder", len(edgeDomainDocs), len(edgeDomainOrder))
	}
}

func seedBasicEdgeConfig(t *testing.T, ops *edgeOps) *edge.Config {
	t.Helper()
	cfg := &edge.Config{
		EdgeID:         "edge-domain-test",
		ProxyEngine:    edge.EngineCaddy,
		ACMEEmail:      "ops@example.com",
		DNSGateOff:     true,
		ShieldHostname: "shield.example.net",
		ShieldIPs:      []string{"198.51.100.10"},
	}
	if err := edge.SaveConfig(ops.paths.ConfigFile, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	return cfg
}

func TestEdgeDomainAddRollbackDryRunAndConflicts(t *testing.T) {
	ops, stdout, _ := newTestEdgeOps(t)
	seedBasicEdgeConfig(t, ops)
	caddyRunner := ops.caddy.(*fakeEdgeCaddyRunner)

	t.Run("dry_run_changes_nothing", func(t *testing.T) {
		stdout.Reset()
		if err := runEdgeWithOps(ops, []string{"domain", "add", "dry.example.com", "10.0.0.5:8080", "--dry-run"}); err != nil {
			t.Fatalf("add --dry-run: %v", err)
		}
		if !strings.Contains(stdout.String(), "dry.example.com") {
			t.Fatalf("expected dry-run config output, got:\n%s", stdout.String())
		}
		if _, err := os.Stat(ops.paths.DomainsFile); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("dry-run must not create domains.json, stat err=%v", err)
		}
		if caddyRunner.reloaded != 0 {
			t.Fatalf("dry-run must not reload proxy, reloads=%d", caddyRunner.reloaded)
		}
	})

	t.Run("apply_failure_leaves_domains_json_untouched", func(t *testing.T) {
		caddyRunner.validateErr = errors.New("bad caddy directive")
		err := runEdgeWithOps(ops, []string{"domain", "add", "fail.example.com", "10.0.0.5:8080"})
		caddyRunner.validateErr = nil
		if err == nil {
			t.Fatal("expected error when proxy validation fails")
		}
		if _, statErr := os.Stat(ops.paths.DomainsFile); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("domains.json must not be saved when apply fails")
		}
	})

	t.Run("add_persists_and_applies", func(t *testing.T) {
		stdout.Reset()
		if err := runEdgeWithOps(ops, []string{"domain", "add", "shop.example.com", "10.0.0.5:8080", "--www", "--max-body", "100m", "--note", "legacy shop"}); err != nil {
			t.Fatalf("add failed: %v", err)
		}
		if caddyRunner.reloaded == 0 {
			t.Fatal("expected Caddy reload after successful add")
		}
		mf, err := edge.LoadManualDomains(ops.paths.DomainsFile)
		if err != nil || len(mf.Domains) != 1 || mf.Domains[0].Domain != "shop.example.com" || !mf.Domains[0].WWW {
			t.Fatalf("unexpected domains.json content: %+v err=%v", mf, err)
		}
		activePanelsDir, err := edge.ReadDirLink(ops.paths.PanelsLink)
		if err != nil {
			t.Fatalf("ReadDirLink: %v", err)
		}
		b, err := os.ReadFile(filepath.Join(activePanelsDir, edge.ManualCaddyFileName))
		if err != nil || !strings.Contains(string(b), "shop.example.com") || !strings.Contains(string(b), "www.shop.example.com") {
			t.Fatalf("zz-manual.caddy missing expected hosts (err=%v):\n%s", err, string(b))
		}
	})

	t.Run("conflicts_with_panel_and_user_file", func(t *testing.T) {
		now := ops.now()
		panelsDir, _ := edge.ReadDirLink(ops.paths.PanelsLink)
		claims, _ := edge.LoadClaims(ops.paths.ClaimsFile, panelsDir, now)
		claims.Claims["panel-host.example.com"] = edge.Claim{PanelID: "panel-alpha", ClaimedAt: now}
		if err := edge.SaveClaims(ops.paths.ClaimsFile, claims); err != nil {
			t.Fatal(err)
		}
		if err := runEdgeWithOps(ops, []string{"domain", "add", "panel-host.example.com", "10.0.0.2:80"}); err == nil || !strings.Contains(err.Error(), "panel-alpha") {
			t.Fatalf("expected panel conflict naming panel-alpha, got %v", err)
		}

		_ = os.MkdirAll(ops.paths.UserCaddyDir, 0o755)
		_ = os.WriteFile(filepath.Join(ops.paths.UserCaddyDir, "custom.caddy"), []byte("user-host.example.com {\n\treverse_proxy 127.0.0.1:9000\n}\n"), 0o644)
		if err := runEdgeWithOps(ops, []string{"domain", "add", "user-host.example.com", "10.0.0.2:80"}); err == nil || !strings.Contains(err.Error(), "custom.caddy") {
			t.Fatalf("expected user file conflict naming custom.caddy, got %v", err)
		}
	})
}

func TestEdgeDomainLifecycleSetRmRestoreExportImportStatusDoctor(t *testing.T) {
	ops, stdout, _ := newTestEdgeOps(t)
	cfg := seedBasicEdgeConfig(t, ops)

	// Add two domains including one with --tls manual cert copy.
	now := ops.now()
	srcCertDir := t.TempDir()
	srcCert := filepath.Join(srcCertDir, "wild.crt")
	srcKey := filepath.Join(srcCertDir, "wild.key")
	cPEM, kPEM := generateSwitchTestCertPair(t, []string{"*.wild.example.com"}, now.Add(-time.Hour), now.Add(60*24*time.Hour))
	_ = os.WriteFile(srcCert, cPEM, 0o644)
	_ = os.WriteFile(srcKey, kPEM, 0o600)

	if err := runEdgeWithOps(ops, []string{"domain", "add", "*.wild.example.com", "10.0.0.9:9000", "--tls", "manual", "--cert", srcCert, "--key", srcKey}); err != nil {
		t.Fatalf("add manual cert domain: %v", err)
	}
	if err := runEdgeWithOps(ops, []string{"domain", "add", "app.example.com", "10.0.0.5:8080", "--note", "v1"}); err != nil {
		t.Fatalf("add app.example.com: %v", err)
	}

	// Set, disable, enable, show --config, list.
	if err := runEdgeWithOps(ops, []string{"domain", "set", "app.example.com", "--note", "v2", "--no-websocket", "--keepalive", "0"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := runEdgeWithOps(ops, []string{"domain", "disable", "app.example.com"}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	stdout.Reset()
	if err := runEdgeWithOps(ops, []string{"domain", "list"}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(stdout.String(), "disabled") || !strings.Contains(stdout.String(), "v2") {
		t.Fatalf("unexpected list output:\n%s", stdout.String())
	}
	if err := runEdgeWithOps(ops, []string{"domain", "enable", "app.example.com"}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	stdout.Reset()
	if err := runEdgeWithOps(ops, []string{"domain", "show", "app.example.com", "--config"}); err != nil {
		t.Fatalf("show --config: %v", err)
	}
	if !strings.Contains(stdout.String(), "keepalive off") {
		t.Fatalf("expected rendered snippet from show --config:\n%s", stdout.String())
	}

	// Status includes manual source column & JSON source field. Also add a panel route for SOURCE comparison.
	p1 := edge.PanelConfig{PanelID: "p1", PanelName: "panel-one", URL: "https://p.example.com", Token: "t", RegisteredAt: now}
	cfg.Panels = []edge.PanelConfig{p1}
	_ = edge.SaveConfig(ops.paths.ConfigFile, cfg)
	st1 := edge.NewPanelState("p1", "panel-one")
	st1.HostStates["panel-site.example.com"] = edge.HostStatus{Host: "panel-site.example.com", State: edge.StateActive}
	_ = edge.SavePanelState(ops.paths.StateFile("p1"), st1)

	stdout.Reset()
	if err := runEdgeWithOps(ops, []string{"status", "--json"}); err != nil {
		t.Fatalf("status --json: %v", err)
	}
	var statusDoc struct {
		Hosts []struct {
			Host   string `json:"host"`
			Source string `json:"source"`
			State  string `json:"state"`
		} `json:"hosts"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &statusDoc); err != nil {
		t.Fatalf("parse status json: %v\n%s", err, stdout.String())
	}
	seenManual, seenPanel := false, false
	for _, h := range statusDoc.Hosts {
		if h.Host == "app.example.com" && h.Source == "manual" {
			seenManual = true
		}
		if h.Host == "panel-site.example.com" && h.Source == "panel-one" {
			seenPanel = true
		}
	}
	if !seenManual || !seenPanel {
		t.Fatalf("expected both manual and panel sources in status json: %+v", statusDoc.Hosts)
	}

	// Doctor checks manual-domains.
	_ = edge.WriteFileAtomic(ops.unitPath, []byte("[Service]\nExecStart=/usr/local/bin/nd edge run\n"), 0o644)
	_ = edge.WriteFileAtomic(filepath.Join(filepath.Dir(ops.unitPath), "caddy.service"), []byte(""), 0o644)
	stdout.Reset()
	_ = runEdgeWithOps(ops, []string{"doctor"})
	if !strings.Contains(stdout.String(), "manual-domains") {
		t.Fatalf("expected manual-domains in doctor output:\n%s", stdout.String())
	}

	// Export -> rm -> restore --list -> restore -> import round trip.
	exportPath := filepath.Join(t.TempDir(), "export.json")
	if err := runEdgeWithOps(ops, []string{"domain", "export", "--file", exportPath}); err != nil {
		t.Fatalf("export: %v", err)
	}
	if err := runEdgeWithOps(ops, []string{"domain", "rm", "app.example.com", "--yes"}); err != nil {
		t.Fatalf("rm --yes: %v", err)
	}
	stdout.Reset()
	if err := runEdgeWithOps(ops, []string{"domain", "restore", "--list"}); err != nil {
		t.Fatalf("restore --list: %v", err)
	}
	if !strings.Contains(stdout.String(), "app.example.com") {
		t.Fatalf("expected app.example.com in trash list:\n%s", stdout.String())
	}
	if err := runEdgeWithOps(ops, []string{"domain", "restore", "app.example.com"}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	mfAfterRestore, _ := edge.LoadManualDomains(ops.paths.DomainsFile)
	if len(mfAfterRestore.Domains) != 2 {
		t.Fatalf("expected 2 domains after restore, got %+v", mfAfterRestore.Domains)
	}

	// Import --replace round trip.
	if err := runEdgeWithOps(ops, []string{"domain", "rm", "app.example.com", "--yes"}); err != nil {
		t.Fatal(err)
	}
	if err := runEdgeWithOps(ops, []string{"domain", "import", "--file", exportPath, "--replace", "--yes"}); err != nil {
		t.Fatalf("import --replace --yes: %v", err)
	}
	mfImported, _ := edge.LoadManualDomains(ops.paths.DomainsFile)
	if len(mfImported.Domains) != 2 {
		t.Fatalf("expected 2 domains after import --replace, got %+v", mfImported.Domains)
	}
}

func TestEdgeSwitchProxyCarriesManualDomains(t *testing.T) {
	ops, _, _ := newTestEdgeOps(t)
	seedCaddyEdgeNodeForSwitchTest(t, ops, "panel-site.example.com")

	// Add a manual domain on Caddy first.
	if err := runEdgeWithOps(ops, []string{"domain", "add", "manual-stay.example.com", "10.0.0.5:8080", "--real-ip", "cloudflare"}); err != nil {
		t.Fatalf("add manual domain before switch: %v", err)
	}

	angieFake := &fakeEdgeRunner{}
	ops.angie = angieFake
	ops.systemd = &fakeEdgeSystemdMulti{
		fakeEdgeSystemdRunner: fakeEdgeSystemdRunner{caddyActive: true, ndEdgeActive: false},
		activeUnits: map[string]bool{
			"caddy.service":  true,
			"angie.service":  true,
			"nd-edge.service": false,
		},
	}
	var probedHosts []string
	ops.probeSwitchHost = func(_ context.Context, host string) error {
		probedHosts = append(probedHosts, host)
		return nil
	}

	// Seed a cached Caddy cert so switch-proxy imports and probes cleanly without waiting on live LE.
	now := ops.now()
	certDir := filepath.Join(ops.paths.CaddyCertsDir, "certificates", "acme-v02.api.letsencrypt.org-directory", "panel-site.example.com")
	_ = os.MkdirAll(certDir, 0o755)
	c1, k1 := generateSwitchTestCertPair(t, []string{"panel-site.example.com"}, now.Add(-time.Hour), now.Add(60*24*time.Hour))
	_ = os.WriteFile(filepath.Join(certDir, "panel-site.example.com.crt"), c1, 0o644)
	_ = os.WriteFile(filepath.Join(certDir, "panel-site.example.com.key"), k1, 0o600)

	if err := runEdgeWithOps(ops, []string{"switch-proxy", "angie", "--yes"}); err != nil {
		t.Fatalf("switch-proxy angie failed: %v", err)
	}

	epAngie := ops.paths.EnginePathsFor(edge.EngineAngie)
	panelsDir, err := edge.ReadDirLink(epAngie.PanelsLink)
	if err != nil {
		t.Fatalf("ReadDirLink(angie panels): %v", err)
	}
	manualBytes, err := os.ReadFile(filepath.Join(panelsDir, edge.ManualConfFileName))
	if err != nil || !strings.Contains(string(manualBytes), "server_name manual-stay.example.com;") {
		t.Fatalf("expected manual-stay.example.com rendered for Angie target (err=%v):\n%s", err, string(manualBytes))
	}
	seenManualProbe := false
	for _, h := range probedHosts {
		if h == "manual-stay.example.com" {
			seenManualProbe = true
		}
	}
	if !seenManualProbe {
		t.Fatalf("expected switch-proxy to probe manual-stay.example.com, probed=%v", probedHosts)
	}
}

func TestEdgeDomainPruneCommandWithFakeClockAndResolver(t *testing.T) {
	ops, stdout, _ := newTestEdgeOps(t)
	curNow := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	ops.now = func() time.Time { return curNow }

	cfg := seedBasicEdgeConfig(t, ops)
	// Seed one panel domain with broken DNS to verify prune --all NEVER touches panel domains.
	p1 := edge.PanelConfig{PanelID: "p1", PanelName: "panel-one", RegisteredAt: curNow}
	cfg.Panels = []edge.PanelConfig{p1}
	_ = edge.SaveConfig(ops.paths.ConfigFile, cfg)
	st1 := edge.NewPanelState("p1", "panel-one")
	st1.LastGood = &edge.RoutesResponse{
		APIVersion: 1,
		PanelID:    "p1",
		Origin:     edge.OriginInfo{IPs: []string{"10.0.0.2"}, Port: 443, CAStatus: edge.CAStatusMissing},
		Routes:     []edge.EdgeRoute{{ID: "r1", Hosts: []string{"panel-dead.example.com"}}},
	}
	st1.HostStates["panel-dead.example.com"] = edge.HostStatus{Host: "panel-dead.example.com", State: edge.StateActive}
	_ = edge.SavePanelState(ops.paths.StateFile("p1"), st1)

	for _, dom := range []string{"dead.example.com", "proxied.example.com", "live.example.com"} {
		if err := runEdgeWithOps(ops, []string{"domain", "add", dom, "10.0.0.5:80"}); err != nil {
			t.Fatalf("add %s: %v", dom, err)
		}
	}

	ops.dnsInspectFn = func(_ context.Context, _ *edge.Config, _ []netip.Addr, host string) edge.FreshDNSInspection {
		switch host {
		case "dead.example.com", "panel-dead.example.com":
			return edge.FreshDNSInspection{Classification: "nxdomain", Outcome: edge.DNSOutcomePending, Detail: "NXDOMAIN"}
		case "proxied.example.com":
			return edge.FreshDNSInspection{Classification: "ok_proxied", Outcome: edge.DNSOutcomeOKProxied}
		default:
			return edge.FreshDNSInspection{Classification: "ok", Outcome: edge.DNSOutcomeOK}
		}
	}
	ops.originCheckFn = func(_ context.Context, _ *edge.ManualDomainEntry, _ []string, _, _ string, _ bool) edge.OriginHealthResult {
		return edge.OriginHealthResult{Status: "up", StatusCode: 200}
	}
	ops.tlsCheckFn = func(_ context.Context, _ *edge.CertProbe, _ *edge.ManualDomainEntry, _ string, _ time.Time) edge.TLSHealthResult {
		return edge.TLSHealthResult{Status: "valid (60 days)", DaysLeft: 60}
	}

	// Check #1 (day 0): must NOT qualify for prune even with --apply.
	stdout.Reset()
	if err := runEdgeWithOps(ops, []string{"domain", "prune", "--apply", "--yes"}); err != nil {
		t.Fatalf("prune check #1: %v", err)
	}
	mf, _ := edge.LoadManualDomains(ops.paths.DomainsFile)
	if len(mf.Domains) != 3 {
		t.Fatalf("single failure must not prune anything, got %d domains", len(mf.Domains))
	}

	// Advance clock 8 days (>7d default grace), run default prune (dry-run) -> reports dead.example.com and panel hint, changes nothing.
	curNow = curNow.Add(8 * 24 * time.Hour)
	stdout.Reset()
	if err := runEdgeWithOps(ops, []string{"domain", "prune", "--all"}); err != nil {
		t.Fatalf("prune dry-run: %v", err)
	}
	if !strings.Contains(stdout.String(), "dead.example.com") || !strings.Contains(stdout.String(), "panel-dead.example.com") {
		t.Fatalf("expected dead.example.com candidate and panel-dead.example.com hint:\n%s", stdout.String())
	}
	mfDry, _ := edge.LoadManualDomains(ops.paths.DomainsFile)
	if len(mfDry.Domains) != 3 {
		t.Fatalf("dry-run must not remove anything, got %d domains", len(mfDry.Domains))
	}

	// Apply prune on day 8 -> removes dead.example.com (and keeps proxied.example.com, live.example.com, panel-dead.example.com).
	stdout.Reset()
	if err := runEdgeWithOps(ops, []string{"domain", "prune", "--apply", "--yes"}); err != nil {
		t.Fatalf("prune --apply: %v", err)
	}
	mfAfter, _ := edge.LoadManualDomains(ops.paths.DomainsFile)
	if len(mfAfter.Domains) != 2 {
		t.Fatalf("expected 2 remaining manual domains after prune, got %+v", mfAfter.Domains)
	}
	for _, d := range mfAfter.Domains {
		if d.Domain == "dead.example.com" {
			t.Fatal("dead.example.com should have been pruned")
		}
	}
	stAfter, _ := edge.LoadPanelState(ops.paths.StateFile("p1"), "p1", "panel-one", curNow)
	if len(stAfter.LastGood.Routes[0].Hosts) != 1 {
		t.Fatal("panel domain must never be modified by prune")
	}

	// Verify restored from trash works cleanly!
	if err := runEdgeWithOps(ops, []string{"domain", "restore", "dead.example.com"}); err != nil {
		t.Fatalf("restore pruned domain: %v", err)
	}
	mfRestored, _ := edge.LoadManualDomains(ops.paths.DomainsFile)
	if len(mfRestored.Domains) != 3 {
		t.Fatalf("expected 3 domains after restoring dead.example.com, got %d", len(mfRestored.Domains))
	}

	// Verify --schedule daily & off writes and cleans up systemd timer/service files.
	if err := runEdgeWithOps(ops, []string{"domain", "prune", "--schedule", "daily"}); err != nil {
		t.Fatalf("prune --schedule daily: %v", err)
	}
	unitDir := filepath.Dir(ops.unitPath)
	timerBytes, err := os.ReadFile(filepath.Join(unitDir, pruneTimerFileName))
	if err != nil || !strings.Contains(string(timerBytes), "OnCalendar=daily") {
		t.Fatalf("expected timer file created, err=%v", err)
	}
	svcBytes, err := os.ReadFile(filepath.Join(unitDir, pruneServiceFileName))
	if err != nil || !strings.Contains(string(svcBytes), "--dry-run") {
		t.Fatalf("expected dry-run default inside service unit, err=%v\n%s", err, string(svcBytes))
	}
	if err := runEdgeWithOps(ops, []string{"domain", "prune", "--schedule", "off"}); err != nil {
		t.Fatalf("prune --schedule off: %v", err)
	}
	if _, err := os.Stat(filepath.Join(unitDir, pruneTimerFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected timer removed on --schedule off, err=%v", err)
	}
}

func TestEdgeDomainUserConfigListShowRmAndReplace(t *testing.T) {
	ops, stdout, _ := newTestEdgeOps(t)
	seedBasicEdgeConfig(t, ops)
	caddyRunner := ops.caddy.(*fakeEdgeCaddyRunner)

	_ = os.MkdirAll(ops.paths.UserCaddyDir, 0o755)
	_ = os.MkdirAll(filepath.Dir(ops.paths.Caddyfile), 0o755)
	managed := edge.RenderCaddyfileManagedRegion(ops.paths, edge.DefaultAdminAddr, "ops@example.com", ops.paths.PanelsLink, ops.paths.UserCaddyDir)
	caddyBody := "hand.example.com {\n\treverse_proxy 127.0.0.1:7001\n}\n\n" +
		"pair-a.example.com, pair-b.example.com {\n\treverse_proxy 127.0.0.1:7002\n}\n\n" +
		managed
	if err := os.WriteFile(ops.paths.Caddyfile, []byte(caddyBody), 0o644); err != nil {
		t.Fatal(err)
	}
	soloFile := filepath.Join(ops.paths.UserCaddyDir, "custom.caddy")
	if err := os.WriteFile(soloFile, []byte("solo-user.example.com {\n\treverse_proxy 127.0.0.1:7003\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. list shows user-config sites even with 0 domains.json entries.
	stdout.Reset()
	if err := runEdgeWithOps(ops, []string{"domain", "list"}); err != nil {
		t.Fatalf("domain list: %v", err)
	}
	outList := stdout.String()
	for _, want := range []string{"hand.example.com", "pair-a.example.com", "solo-user.example.com", "user:Caddyfile", "user:custom.caddy", "user-config"} {
		if !strings.Contains(outList, want) {
			t.Fatalf("expected %q in domain list output:\n%s", want, outList)
		}
	}

	// 2. show & show --config work on user-config domains.
	stdout.Reset()
	if err := runEdgeWithOps(ops, []string{"domain", "show", "solo-user.example.com", "--config"}); err != nil {
		t.Fatalf("domain show --config: %v", err)
	}
	if !strings.Contains(stdout.String(), "reverse_proxy 127.0.0.1:7003") {
		t.Fatalf("unexpected show --config output:\n%s", stdout.String())
	}

	// 3. status includes user-config hosts.
	stdout.Reset()
	if err := runEdgeWithOps(ops, []string{"status", "--json"}); err != nil {
		t.Fatalf("status --json: %v", err)
	}
	if !strings.Contains(stdout.String(), "hand.example.com") || !strings.Contains(stdout.String(), "user:Caddyfile") {
		t.Fatalf("expected user-config domain in status --json:\n%s", stdout.String())
	}

	// 4. check inspects user-config domains.
	ops.dnsInspectFn = func(_ context.Context, _ *edge.Config, _ []netip.Addr, _ string) edge.FreshDNSInspection {
		return edge.FreshDNSInspection{Classification: "ok", Outcome: edge.DNSOutcomeOK}
	}
	ops.originCheckFn = func(_ context.Context, _ *edge.ManualDomainEntry, urls []string, _, _ string, _ bool) edge.OriginHealthResult {
		return edge.OriginHealthResult{Status: "up", StatusCode: 200}
	}
	ops.tlsCheckFn = func(_ context.Context, _ *edge.CertProbe, _ *edge.ManualDomainEntry, _ string, _ time.Time) edge.TLSHealthResult {
		return edge.TLSHealthResult{Status: "valid (60 days)", DaysLeft: 60}
	}
	stdout.Reset()
	if err := runEdgeWithOps(ops, []string{"domain", "check", "hand.example.com"}); err != nil {
		t.Fatalf("domain check user-config host: %v", err)
	}
	if !strings.Contains(stdout.String(), "hand.example.com") || !strings.Contains(stdout.String(), "user:Caddyfile") {
		t.Fatalf("expected hand.example.com check output:\n%s", stdout.String())
	}

	// 5. add without --replace still errors with helpful hint; add --replace fails cleanly & rolls back when validation fails.
	if err := runEdgeWithOps(ops, []string{"domain", "add", "hand.example.com", "10.0.0.9:8080"}); err == nil || !strings.Contains(err.Error(), "--replace") {
		t.Fatalf("expected conflict error mentioning --replace, got %v", err)
	}
	caddyRunner.validateErr = errors.New("validate fail")
	if err := runEdgeWithOps(ops, []string{"domain", "add", "hand.example.com", "10.0.0.9:8080", "--replace"}); err == nil {
		t.Fatal("expected add --replace error on validate failure")
	}
	caddyRunner.validateErr = nil
	afterFailCaddy, _ := os.ReadFile(ops.paths.Caddyfile)
	if !strings.Contains(string(afterFailCaddy), "hand.example.com {") {
		t.Fatalf("expected Caddyfile rolled back after add --replace failure:\n%s", string(afterFailCaddy))
	}

	// 6. add --replace succeeds: removes hand.example.com from Caddyfile unmanaged block and saves to domains.json.
	stdout.Reset()
	if err := runEdgeWithOps(ops, []string{"domain", "add", "hand.example.com", "10.0.0.9:8080", "--replace"}); err != nil {
		t.Fatalf("add --replace failed: %v", err)
	}
	mf, err := edge.LoadManualDomains(ops.paths.DomainsFile)
	if err != nil || len(mf.Domains) != 1 || mf.Domains[0].Domain != "hand.example.com" {
		t.Fatalf("expected hand.example.com adopted into domains.json: %+v err=%v", mf, err)
	}
	afterReplaceCaddy, _ := os.ReadFile(ops.paths.Caddyfile)
	if strings.Contains(string(afterReplaceCaddy), "hand.example.com {") {
		t.Fatalf("expected hand.example.com removed from unmanaged Caddyfile:\n%s", string(afterReplaceCaddy))
	}

	// 7. rm rolls back on validate failure, and cleanly removes both single-host user file + multi-host member on success.
	caddyRunner.validateErr = errors.New("reload fail")
	if err := runEdgeWithOps(ops, []string{"domain", "rm", "solo-user.example.com", "--yes"}); err == nil {
		t.Fatal("expected rm to fail when validation fails")
	}
	caddyRunner.validateErr = nil
	if _, err := os.Stat(soloFile); err != nil {
		t.Fatalf("expected soloFile rolled back after failed rm: %v", err)
	}

	stdout.Reset()
	if err := runEdgeWithOps(ops, []string{"domain", "rm", "solo-user.example.com", "pair-a.example.com", "--yes"}); err != nil {
		t.Fatalf("rm user-config domains failed: %v", err)
	}
	if _, err := os.Stat(soloFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected empty custom.caddy removed after rm, err=%v", err)
	}
	finalCaddy, _ := os.ReadFile(ops.paths.Caddyfile)
	if strings.Contains(string(finalCaddy), "pair-a.example.com") || !strings.Contains(string(finalCaddy), "pair-b.example.com {") {
		t.Fatalf("expected pair-a removed and pair-b kept:\n%s", string(finalCaddy))
	}
}
