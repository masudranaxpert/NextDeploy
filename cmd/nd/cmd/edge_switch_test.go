package cmd

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"nd/internal/edge"
)

func generateSwitchTestCertPair(t *testing.T, hosts []string, notBefore, notAfter time.Time) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ecdsa key: %v", err)
	}
	cn := "localhost"
	if len(hosts) > 0 {
		cn = hosts[0]
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     hosts,
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

func seedCaddyEdgeNodeForSwitchTest(t *testing.T, ops *edgeOps, host string) {
	t.Helper()
	now := ops.now()
	panel := edge.PanelConfig{
		PanelID:         "panel-1",
		PanelName:       "main-panel",
		ShieldNodeID:    5,
		URL:             "https://panel.example.com",
		Token:           "tok_123",
		PollIntervalSec: 30,
		RegisteredAt:    now,
	}
	cfg := &edge.Config{
		EdgeID:         "edge-switch-01",
		ProxyEngine:    edge.EngineCaddy,
		ACMEEmail:      "ops@example.com",
		ShieldHostname: "shield.example.net",
		Panels:         []edge.PanelConfig{panel},
	}
	if err := edge.SaveConfig(ops.paths.ConfigFile, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	snap := &edge.RoutesResponse{
		APIVersion: 1,
		PanelID:    panel.PanelID,
		ETag:       "etag-sw1",
		Origin: edge.OriginInfo{
			IPs:      []string{"203.0.113.42"},
			Port:     443,
			CAStatus: edge.CAStatusMissing,
		},
		Routes: []edge.EdgeRoute{
			{ID: "dom_1", Kind: edge.KindAppDomain, Hosts: []string{host}},
		},
	}
	if err := saveInitialPanelState(ops.paths, panel, snap, now); err != nil {
		t.Fatalf("saveInitialPanelState: %v", err)
	}
	if err := edge.WriteFileAtomic(ops.unitPath, []byte(renderSystemdUnitForEngine(ops.binaryPath, "caddy.service")), 0o644); err != nil {
		t.Fatalf("write unitPath: %v", err)
	}
	if err := edge.WriteFileAtomic(ops.paths.Caddyfile, []byte(edge.ManagedStartMarker+"\n"+edge.ManagedEndMarker+"\n"), 0o644); err != nil {
		t.Fatalf("write Caddyfile: %v", err)
	}

	certPEM, keyPEM := generateSwitchTestCertPair(t, []string{host}, now.Add(-24*time.Hour), now.Add(60*24*time.Hour))
	caddyHostCertDir := filepath.Join(ops.paths.CaddyCertsDir, "certificates", "acme-v02.api.letsencrypt.org-directory", host)
	if err := os.MkdirAll(caddyHostCertDir, 0o755); err != nil {
		t.Fatalf("mkdir caddy cert dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(caddyHostCertDir, host+".crt"), certPEM, 0o644); err != nil {
		t.Fatalf("write crt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(caddyHostCertDir, host+".key"), keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
}

func TestParseSwitchProxyArgs(t *testing.T) {
	opts, err := parseSwitchProxyArgs([]string{"angie", "--yes"})
	if err != nil || opts.targetEngine != edge.EngineAngie || !opts.yes {
		t.Fatalf("expected angie --yes, got %+v err=%v", opts, err)
	}

	opts, err = parseSwitchProxyArgs([]string{"caddy", "-y"})
	if err != nil || opts.targetEngine != edge.EngineCaddy || !opts.yes {
		t.Fatalf("expected caddy -y, got %+v err=%v", opts, err)
	}

	if _, err := parseSwitchProxyArgs([]string{"ferron"}); err == nil || !strings.Contains(err.Error(), "--experimental") {
		t.Fatalf("expected --experimental gate error, got %v", err)
	}
	optsFerron, err := parseSwitchProxyArgs([]string{"ferron", "--experimental", "--yes"})
	if err != nil || optsFerron.targetEngine != edge.EngineFerron || !optsFerron.experimental || !optsFerron.yes {
		t.Fatalf("expected ferron --experimental --yes, got %+v err=%v", optsFerron, err)
	}
	if _, err := parseSwitchProxyArgs(nil); err == nil {
		t.Fatalf("expected error on missing target engine")
	}
	if _, err := parseSwitchProxyArgs([]string{"angie", "--bogus"}); err == nil {
		t.Fatalf("expected error on unknown flag")
	}
	if _, err := parseSwitchProxyArgs([]string{"angie", "caddy"}); err == nil {
		t.Fatalf("expected error on extra argument")
	}
}

func TestEdgeSwitchProxyValidationFailureLeavesSourceUntouched(t *testing.T) {
	ops, _, _ := newTestEdgeOps(t)
	seedCaddyEdgeNodeForSwitchTest(t, ops, "app.example.com")

	sysd := newFakeSwitchSystemdRunner()
	sysd.units["nd-edge.service"] = true
	ops.systemd = sysd
	ops.angie = &fakeEdgeAngieRunner{validateErr: errors.New("syntax error in server block")}

	var cmds []string
	ops.runCmd = func(_ context.Context, name string, args ...string) (string, error) {
		cmd := name + " " + strings.Join(args, " ")
		cmds = append(cmds, cmd)
		return "", nil
	}

	err := runEdgeWithOps(ops, []string{"switch-proxy", "angie", "--yes"})
	if err == nil || !strings.Contains(err.Error(), "preflight validation failed") {
		t.Fatalf("expected preflight validation failure error, got %v", err)
	}

	joined := strings.Join(cmds, "\n")
	if strings.Contains(joined, "systemctl stop caddy.service") {
		t.Fatalf("source caddy.service must NOT be stopped when preflight validation fails; cmds:\n%s", joined)
	}
	if !strings.Contains(joined, "systemctl start nd-edge") {
		t.Fatalf("expected paused nd-edge daemon to resume after preflight abort; cmds:\n%s", joined)
	}

	cfg, loadErr := edge.LoadConfig(ops.paths.ConfigFile)
	if loadErr != nil || cfg.EngineName() != edge.EngineCaddy {
		t.Fatalf("expected config engine to stay caddy, got %+v err=%v", cfg, loadErr)
	}
	unitBytes, _ := os.ReadFile(ops.unitPath)
	if !strings.Contains(string(unitBytes), "caddy.service") {
		t.Fatalf("expected nd-edge.service unit to still reference caddy.service:\n%s", string(unitBytes))
	}
}

func TestEdgeSwitchProxyRollbackOnProbeAndCheckAliveFailure(t *testing.T) {
	t.Run("https probe failure rolls back to caddy", func(t *testing.T) {
		ops, _, _ := newTestEdgeOps(t)
		seedCaddyEdgeNodeForSwitchTest(t, ops, "app.example.com")

		sysd := newFakeSwitchSystemdRunner()
		sysd.units["nd-edge.service"] = true
		ops.systemd = sysd
		ops.angie = &fakeEdgeAngieRunner{}

		var cmds []string
		ops.runCmd = func(_ context.Context, name string, args ...string) (string, error) {
			cmd := name + " " + strings.Join(args, " ")
			cmds = append(cmds, cmd)
			switch cmd {
			case "systemctl stop caddy.service":
				sysd.units["caddy.service"] = false
			case "systemctl enable --now angie.service":
				sysd.units["angie.service"] = true
			case "systemctl stop angie.service":
				sysd.units["angie.service"] = false
			case "systemctl start caddy.service":
				sysd.units["caddy.service"] = true
			}
			return "", nil
		}
		var probedHosts []string
		ops.probeSwitchHost = func(_ context.Context, host string) error {
			probedHosts = append(probedHosts, host)
			return errors.New("tls handshake failed")
		}

		err := runEdgeWithOps(ops, []string{"switch-proxy", "angie", "--yes"})
		if err == nil || !strings.Contains(err.Error(), "rolled back to caddy") {
			t.Fatalf("expected rollback error, got %v", err)
		}
		if len(probedHosts) != 1 || probedHosts[0] != "app.example.com" {
			t.Fatalf("expected HTTPS probe for app.example.com, got %v", probedHosts)
		}

		joined := strings.Join(cmds, "\n")
		for _, want := range []string{
			"systemctl stop angie.service",
			"systemctl start caddy.service",
			"systemctl start nd-edge",
		} {
			if !strings.Contains(joined, want) {
				t.Fatalf("expected rollback cmd %q in:\n%s", want, joined)
			}
		}

		unitBytes, _ := os.ReadFile(ops.unitPath)
		if !strings.Contains(string(unitBytes), "caddy.service") {
			t.Fatalf("expected restored unit file to target caddy.service:\n%s", string(unitBytes))
		}
		cfg, _ := edge.LoadConfig(ops.paths.ConfigFile)
		if cfg.EngineName() != edge.EngineCaddy {
			t.Fatalf("expected config proxy_engine=caddy after rollback, got %q", cfg.ProxyEngine)
		}
		meta, _ := edge.LoadApplyMeta(ops.paths.ApplyMeta)
		if meta == nil || !strings.Contains(meta.LastApplyError, "rolled back") {
			t.Fatalf("expected rollback error recorded in ApplyMeta, got %+v", meta)
		}
	})

	t.Run("CheckAlive failure rolls back to caddy", func(t *testing.T) {
		ops, _, _ := newTestEdgeOps(t)
		seedCaddyEdgeNodeForSwitchTest(t, ops, "app.example.com")

		sysd := newFakeSwitchSystemdRunner()
		ops.systemd = sysd
		ops.angie = &fakeEdgeAngieRunner{aliveErr: errors.New("angie pid file not found")}

		var cmds []string
		ops.runCmd = func(_ context.Context, name string, args ...string) (string, error) {
			cmd := name + " " + strings.Join(args, " ")
			cmds = append(cmds, cmd)
			if cmd == "systemctl enable --now angie.service" {
				sysd.units["angie.service"] = true
			}
			return "", nil
		}

		err := runEdgeWithOps(ops, []string{"switch-proxy", "angie", "--yes"})
		if err == nil || !strings.Contains(err.Error(), "CheckAlive failed") || !strings.Contains(err.Error(), "rolled back to caddy") {
			t.Fatalf("expected CheckAlive rollback error, got %v", err)
		}
		joined := strings.Join(cmds, "\n")
		if !strings.Contains(joined, "systemctl stop angie.service") || !strings.Contains(joined, "systemctl start caddy.service") {
			t.Fatalf("expected rollback cmds on CheckAlive failure:\n%s", joined)
		}
	})
}

func TestEdgeSwitchProxyHappyPathCaddyToAngieAndBack(t *testing.T) {
	ops, stdout, stderr := newTestEdgeOps(t)
	seedCaddyEdgeNodeForSwitchTest(t, ops, "app.example.com")

	sysd := newFakeSwitchSystemdRunner()
	sysd.units["nd-edge.service"] = true
	ops.systemd = sysd
	angieRunner := &fakeEdgeAngieRunner{version: "1.12.4"}
	ops.angie = angieRunner

	_ = os.WriteFile(ops.osReleasePath, []byte("ID=ubuntu\nID_LIKE=debian\nVERSION_ID=\"24.04\"\nVERSION_CODENAME=noble\n"), 0o644)
	angieInstalled := false
	ops.lookPath = func(bin string) (string, error) {
		if bin == "angie" && !angieInstalled {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + bin, nil
	}

	var cmds []string
	var activeEvents []string
	ops.runCmd = func(_ context.Context, name string, args ...string) (string, error) {
		cmd := name + " " + strings.Join(args, " ")
		cmds = append(cmds, cmd)
		switch {
		case name == "bash":
			angieInstalled = true
			activeEvents = append(activeEvents, "install-angie")
		case cmd == "systemctl stop caddy.service":
			sysd.units["caddy.service"] = false
			activeEvents = append(activeEvents, "stop-caddy")
		case cmd == "systemctl enable --now angie.service":
			sysd.units["angie.service"] = true
			activeEvents = append(activeEvents, "start-angie")
		case cmd == "systemctl disable caddy.service":
			activeEvents = append(activeEvents, "disable-caddy")
		case cmd == "systemctl stop angie.service":
			sysd.units["angie.service"] = false
			activeEvents = append(activeEvents, "stop-angie")
		case cmd == "systemctl enable --now caddy.service":
			sysd.units["caddy.service"] = true
			activeEvents = append(activeEvents, "start-caddy")
		case cmd == "systemctl disable angie.service":
			activeEvents = append(activeEvents, "disable-angie")
		}
		return "", nil
	}

	var probed []string
	ops.probeSwitchHost = func(_ context.Context, host string) error {
		probed = append(probed, host)
		activeEvents = append(activeEvents, "probe:"+host)
		return nil
	}

	// 1. Switch Caddy -> Angie
	if err := runEdgeWithOps(ops, []string{"switch-proxy", "angie", "--yes"}); err != nil {
		t.Fatalf("switch-proxy angie failed: %v\nstderr=%s", err, stderr.String())
	}

	if _, err := os.Stat(edgeAngieInstalledMarker(ops.paths)); err != nil {
		t.Fatalf("expected angie-installed-by-nd marker: %v", err)
	}
	dropIn, err := os.ReadFile(filepath.Join(filepath.Dir(ops.unitPath), "angie.service.d", "nd-edge.conf"))
	if err != nil || !strings.Contains(string(dropIn), "PIDFile="+edge.DefaultAngiePIDFile) || !strings.Contains(string(dropIn), "RuntimeDirectory=angie") ||
		!strings.Contains(string(dropIn), "ExecReload=/bin/kill -s HUP $MAINPID") {
		t.Fatalf("angie systemd drop-in must pin the pid file nd edge renders: %v\n%s", err, dropIn)
	}

	angiePaths := ops.paths.EnginePathsFor(edge.EngineAngie)
	certPath := filepath.Join(angiePaths.ManualCertsDir, "app.example.com.crt")
	keyPath := filepath.Join(angiePaths.ManualCertsDir, "app.example.com.key")
	if _, err := os.Stat(certPath); err != nil {
		t.Fatalf("expected imported cert at %s: %v", certPath, err)
	}
	keyInfo, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("expected imported key at %s: %v", keyPath, err)
	}
	if runtime.GOOS != "windows" && keyInfo.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 permissions on imported key %s, got %o", keyPath, keyInfo.Mode().Perm())
	}

	panelsDir, err := edge.ReadDirLink(angiePaths.PanelsLink)
	if err != nil {
		t.Fatalf("ReadDirLink(%s): %v", angiePaths.PanelsLink, err)
	}
	panelConf, err := os.ReadFile(filepath.Join(panelsDir, "panel-1.conf"))
	if err != nil {
		t.Fatalf("read panel-1.conf: %v", err)
	}
	panelText := string(panelConf)
	if !strings.Contains(panelText, "ssl_certificate") || !strings.Contains(panelText, "app.example.com.crt") {
		t.Fatalf("expected imported ssl_certificate directive in panel-1.conf:\n%s", panelText)
	}
	if strings.Contains(panelText, "acme_client") {
		t.Fatalf("expected manual imported cert to suppress ACME for app.example.com:\n%s", panelText)
	}

	if len(angieRunner.validated) == 0 || angieRunner.aliveChecks == 0 {
		t.Fatalf("expected angie validate & CheckAlive to run, got validate=%d alive=%d", len(angieRunner.validated), angieRunner.aliveChecks)
	}
	if len(probed) != 1 || probed[0] != "app.example.com" {
		t.Fatalf("expected probe for app.example.com, got %v", probed)
	}

	eventsOrder := strings.Join(activeEvents, " -> ")
	if !strings.Contains(eventsOrder, "install-angie -> stop-caddy -> start-angie -> probe:app.example.com -> disable-caddy") {
		t.Fatalf("unexpected cutover sequence: %s", eventsOrder)
	}

	cfg, err := edge.LoadConfig(ops.paths.ConfigFile)
	if err != nil || cfg.EngineName() != edge.EngineAngie {
		t.Fatalf("expected config proxy_engine=angie, got %+v err=%v", cfg, err)
	}
	unitBytes, _ := os.ReadFile(ops.unitPath)
	if !strings.Contains(string(unitBytes), "angie.service") {
		t.Fatalf("expected nd-edge.service to target angie.service:\n%s", string(unitBytes))
	}

	// 2. Switching to the already-active engine is a clean idempotent no-op.
	cmdsBeforeNoOp := len(cmds)
	stdout.Reset()
	if err := runEdgeWithOps(ops, []string{"switch-proxy", "angie"}); err != nil {
		t.Fatalf("idempotent switch-proxy angie failed: %v", err)
	}
	if len(cmds) != cmdsBeforeNoOp {
		t.Fatalf("expected no service commands on idempotent switch-proxy, got %v", cmds[cmdsBeforeNoOp:])
	}

	// 3. Switch back Angie -> Caddy
	activeEvents = nil
	if err := runEdgeWithOps(ops, []string{"switch-proxy", "caddy", "--yes"}); err != nil {
		t.Fatalf("switch-proxy caddy failed: %v\nstderr=%s", err, stderr.String())
	}
	cfgBack, err := edge.LoadConfig(ops.paths.ConfigFile)
	if err != nil || cfgBack.EngineName() != edge.EngineCaddy {
		t.Fatalf("expected config proxy_engine=caddy after switch back, got %+v err=%v", cfgBack, err)
	}
	unitBack, _ := os.ReadFile(ops.unitPath)
	if !strings.Contains(string(unitBack), "caddy.service") {
		t.Fatalf("expected nd-edge.service restored to caddy.service:\n%s", string(unitBack))
	}
	backOrder := strings.Join(activeEvents, " -> ")
	if !strings.Contains(backOrder, "stop-angie -> start-caddy -> probe:app.example.com -> disable-angie") {
		t.Fatalf("unexpected switch-back sequence: %s", backOrder)
	}
}

func TestEdgeSwitchProxyCaddyToFerronExperimentalWithRollback(t *testing.T) {
	// 1. Gated behind --experimental
	opsGate, _, _ := newTestEdgeOps(t)
	seedCaddyEdgeNodeForSwitchTest(t, opsGate, "app.example.com")
	if err := runEdgeWithOps(opsGate, []string{"switch-proxy", "ferron"}); err == nil || !strings.Contains(err.Error(), "--experimental") {
		t.Fatalf("expected --experimental gate error, got %v", err)
	}

	// 2. Happy path Caddy -> Ferron (--experimental) with cert import
	ops, _, stderr := newTestEdgeOps(t)
	seedCaddyEdgeNodeForSwitchTest(t, ops, "app.example.com")
	sysd := newFakeSwitchSystemdRunner()
	sysd.units["nd-edge.service"] = true
	ops.systemd = sysd
	ferronRunner := &fakeEdgeAngieRunner{version: "3.0.0-rc.11"}
	ops.ferron = ferronRunner
	_ = os.WriteFile(ops.osReleasePath, []byte("ID=debian\nVERSION_ID=\"12\"\nVERSION_CODENAME=bookworm\n"), 0o644)
	ferronInstalled := false
	ops.lookPath = func(bin string) (string, error) {
		if bin == "ferron" && !ferronInstalled {
			return "", errors.New("not found")
		}
		return "/usr/sbin/" + bin, nil
	}

	var cmds []string
	ops.runCmd = func(_ context.Context, name string, args ...string) (string, error) {
		cmd := name + " " + strings.Join(args, " ")
		cmds = append(cmds, cmd)
		switch {
		case name == "bash":
			ferronInstalled = true
		case cmd == "systemctl stop caddy.service":
			sysd.units["caddy.service"] = false
		case cmd == "systemctl enable --now ferron.service":
			sysd.units["ferron.service"] = true
		case cmd == "systemctl stop ferron.service":
			sysd.units["ferron.service"] = false
		case cmd == "systemctl start caddy.service":
			sysd.units["caddy.service"] = true
		}
		return "", nil
	}
	ops.probeSwitchHost = func(_ context.Context, host string) error {
		return nil
	}

	if err := runEdgeWithOps(ops, []string{"switch-proxy", "ferron", "--experimental", "--yes"}); err != nil {
		t.Fatalf("switch-proxy ferron failed: %v\nstderr=%s", err, stderr.String())
	}

	if _, err := os.Stat(edgeFerronInstalledMarker(ops.paths)); err != nil {
		t.Fatalf("expected ferron-installed-by-nd marker: %v", err)
	}
	fp := ops.paths.EnginePathsFor(edge.EngineFerron)
	if _, err := os.Stat(filepath.Join(fp.ManualCertsDir, "app.example.com.crt")); err != nil {
		t.Fatalf("expected imported manual cert in Ferron certs dir: %v", err)
	}
	panelsDir, err := edge.ReadDirLink(fp.PanelsLink)
	if err != nil {
		t.Fatalf("ReadDirLink(%s): %v", fp.PanelsLink, err)
	}
	panelBytes, err := os.ReadFile(filepath.Join(panelsDir, "panel-1.conf"))
	if err != nil {
		t.Fatalf("read Ferron panel-1.conf: %v", err)
	}
	if !strings.Contains(string(panelBytes), "provider manual") || !strings.Contains(string(panelBytes), "app.example.com.crt") {
		t.Fatalf("expected Ferron panel conf to use imported manual cert:\n%s", string(panelBytes))
	}
	cfg, _ := edge.LoadConfig(ops.paths.ConfigFile)
	if cfg == nil || cfg.EngineName() != edge.EngineFerron {
		t.Fatalf("expected proxy_engine=ferron persisted, got %+v", cfg)
	}

	// 3. Rollback on HTTPS probe failure when switching Caddy -> Ferron
	opsRB, _, _ := newTestEdgeOps(t)
	seedCaddyEdgeNodeForSwitchTest(t, opsRB, "app.example.com")
	sysdRB := newFakeSwitchSystemdRunner()
	opsRB.systemd = sysdRB
	opsRB.ferron = &fakeEdgeAngieRunner{version: "3.0.0-rc.11"}
	var rbCmds []string
	opsRB.runCmd = func(_ context.Context, name string, args ...string) (string, error) {
		cmd := name + " " + strings.Join(args, " ")
		rbCmds = append(rbCmds, cmd)
		switch cmd {
		case "systemctl stop caddy.service":
			sysdRB.units["caddy.service"] = false
		case "systemctl enable --now ferron.service":
			sysdRB.units["ferron.service"] = true
		case "systemctl stop ferron.service":
			sysdRB.units["ferron.service"] = false
		case "systemctl start caddy.service":
			sysdRB.units["caddy.service"] = true
		}
		return "", nil
	}
	opsRB.probeSwitchHost = func(_ context.Context, _ string) error {
		return errors.New("probe refused")
	}
	if err := runEdgeWithOps(opsRB, []string{"switch-proxy", "ferron", "--experimental", "--yes"}); err == nil || !strings.Contains(err.Error(), "rolled back to caddy") {
		t.Fatalf("expected rollback to caddy, got %v", err)
	}
	rbJoined := strings.Join(rbCmds, "\n")
	if !strings.Contains(rbJoined, "systemctl stop ferron.service") || !strings.Contains(rbJoined, "systemctl start caddy.service") {
		t.Fatalf("expected ferron rollback commands:\n%s", rbJoined)
	}
	cfgRB, _ := edge.LoadConfig(opsRB.paths.ConfigFile)
	if cfgRB == nil || cfgRB.EngineName() != edge.EngineCaddy {
		t.Fatalf("expected proxy_engine=caddy after rollback, got %+v", cfgRB)
	}
}
