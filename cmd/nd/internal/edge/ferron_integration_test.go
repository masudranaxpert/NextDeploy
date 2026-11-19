//go:build integration

package edge

import (
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFerronDockerConfigValidation(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker binary not found; skipping live ferron validate test")
	}
	var image string
	for _, candidate := range []string{
		"ferronserver/ferron:3.0.0-rc.11-alpine",
		"ferronserver/ferron:3-alpine",
	} {
		if _, err := exec.Command("docker", "image", "inspect", candidate).CombinedOutput(); err == nil {
			image = candidate
			break
		}
	}
	if image == "" {
		t.Skip("ferron 3 alpine image not available locally")
	}

	hostRoot := t.TempDir()
	containerRoot := "/work"
	containerPaths := NewFerronEnginePaths(containerRoot)
	hostPaths := NewFerronEnginePaths(hostRoot)

	for _, dir := range []string{
		filepath.Dir(hostPaths.MainConfig),
		hostPaths.ManagedDir,
		hostPaths.PanelsLink,
		hostPaths.OriginsLink,
		hostPaths.UserDir,
		hostPaths.CertsDir,
		hostPaths.ManualCertsDir,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	cfg := &Config{
		EdgeID:    "edge-ferron-int-1",
		ACMEEmail: "ops@example.com",
		ShieldIPs: []string{"107.189.6.222"},
	}
	eng := NewFerronEngineWithConfig(containerPaths, cfg, nil)

	globalFiles, err := eng.RenderGlobal(cfg, DefaultCloudflareRanges, true)
	if err != nil {
		t.Fatalf("RenderGlobal: %v", err)
	}
	commonFiles, err := eng.RenderCommon()
	if err != nil {
		t.Fatalf("RenderCommon: %v", err)
	}

	writeContainerMapped := func(containerPath, content string) {
		t.Helper()
		rel := strings.TrimPrefix(filepath.ToSlash(containerPath), containerRoot+"/")
		dest := filepath.Join(hostRoot, filepath.FromSlash(rel))
		if err := os.WriteFile(dest, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", dest, err)
		}
	}
	for k, v := range globalFiles {
		writeContainerMapped(k, v)
	}
	for k, v := range commonFiles {
		writeContainerMapped(k, v)
	}
	writeContainerMapped(filepath.Join(containerPaths.UserDir, PlaceholderFerronFileName), RenderPlaceholderFerron())
	writeContainerMapped(filepath.Join(containerPaths.PanelsLink, PlaceholderFerronFileName), RenderPlaceholderFerron())

	// Render panel from edge_contract.json + multi-route H2/TrustedProxy/ManualCert cases.
	rawContract, err := os.ReadFile(filepath.Join("testdata", "edge_contract.json"))
	if err != nil {
		t.Fatalf("read edge_contract.json: %v", err)
	}
	var doc edgeContractFixture
	if err := json.Unmarshal(rawContract, &doc); err != nil {
		t.Fatalf("unmarshal edge_contract.json: %v", err)
	}

	caPEM := generateIntegrationCAPEM(t)
	if err := os.WriteFile(filepath.Join(hostPaths.OriginsLink, doc.RoutesResponse.PanelID+".pem"), caPEM, 0o644); err != nil {
		t.Fatalf("write origin CA PEM: %v", err)
	}

	contractPanel, err := eng.RenderPanel(RenderPanelInput{
		PanelID:     doc.RoutesResponse.PanelID,
		PanelName:   doc.RegisterResponse.PanelName,
		ETag:        doc.RoutesResponse.ETag,
		OriginIPs:   ParseIPStrings(doc.RoutesResponse.Origin.IPs),
		OriginPort:  doc.RoutesResponse.Origin.Port,
		CAStatus:    doc.RoutesResponse.Origin.CAStatus,
		OriginsLink: filepath.ToSlash(containerPaths.OriginsLink),
		Routes:      doc.RoutesResponse.Routes,
	})
	if err != nil {
		t.Fatalf("RenderPanel contract: %v", err)
	}
	writeContainerMapped(filepath.Join(containerPaths.PanelsLink, doc.RoutesResponse.PanelID+".conf"), contractPanel)

	// Add manual imported cert test inside Docker validation.
	now := time.Now().UTC()
	manCert, manKey := generateTestLeafCertAndKey(t, []string{"manual-int.example.com"}, now.Add(-time.Hour), now.Add(60*24*time.Hour))
	if err := os.WriteFile(filepath.Join(hostPaths.ManualCertsDir, "manual-int.example.com.crt"), manCert, 0o644); err != nil {
		t.Fatalf("write manual cert: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hostPaths.ManualCertsDir, "manual-int.example.com.key"), manKey, 0o600); err != nil {
		t.Fatalf("write manual key: %v", err)
	}

	secondPanel, err := eng.RenderPanel(RenderPanelInput{
		PanelID:     "bbbbbbbb-1111-4222-8333-444444444444",
		PanelName:   "vps-2",
		ETag:        "etag-2",
		OriginIPs:   []netip.Addr{netip.MustParseAddr("204.136.10.52"), netip.MustParseAddr("2001:db8::2")},
		OriginPort:  443,
		CAStatus:    CAStatusMissing,
		OriginsLink: filepath.ToSlash(containerPaths.OriginsLink),
		Routes: []EdgeRoute{
			{
				ID:           "proxy_9",
				Kind:         KindProxyRoute,
				Hosts:        []string{"grpc.example.com"},
				TrustedProxy: true,
				Options:      RouteOptions{UpstreamH2: true, MaxBodyMB: 64},
			},
			{
				ID:           "dom_12",
				Kind:         KindAppDomain,
				Hosts:        []string{"second.example.com", "manual-int.example.com"},
				TrustedProxy: false,
				Options:      RouteOptions{Websocket: true},
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderPanel second: %v", err)
	}
	writeContainerMapped(filepath.Join(containerPaths.PanelsLink, "bbbbbbbb-1111-4222-8333-444444444444.conf"), secondPanel)

	// Note: container Engine's RenderPanel checks e.paths.ManualCertsDir (on host); rewrite container manual dir in host test if needed:
	engHostManual := NewFerronEngineWithConfig(hostPaths, cfg, nil)
	engHostManual.paths.OriginsLink = containerPaths.OriginsLink
	secondPanelFixed, err := engHostManual.RenderPanel(RenderPanelInput{
		PanelID:     "bbbbbbbb-1111-4222-8333-444444444444",
		PanelName:   "vps-2",
		ETag:        "etag-2",
		OriginIPs:   []netip.Addr{netip.MustParseAddr("204.136.10.52"), netip.MustParseAddr("2001:db8::2")},
		OriginPort:  443,
		CAStatus:    CAStatusMissing,
		OriginsLink: filepath.ToSlash(containerPaths.OriginsLink),
		Routes: []EdgeRoute{
			{
				ID:           "proxy_9",
				Kind:         KindProxyRoute,
				Hosts:        []string{"grpc.example.com"},
				TrustedProxy: true,
				Options:      RouteOptions{UpstreamH2: true, MaxBodyMB: 64},
			},
			{
				ID:           "dom_12",
				Kind:         KindAppDomain,
				Hosts:        []string{"second.example.com", "manual-int.example.com"},
				TrustedProxy: false,
				Options:      RouteOptions{Websocket: true},
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderPanel hostManual: %v", err)
	}
	secondPanelFixed = strings.ReplaceAll(secondPanelFixed, filepath.ToSlash(hostPaths.ManualCertsDir), filepath.ToSlash(containerPaths.ManualCertsDir))
	secondPanelFixed = strings.ReplaceAll(secondPanelFixed, filepath.ToSlash(hostPaths.CertsDir), filepath.ToSlash(containerPaths.CertsDir))
	writeContainerMapped(filepath.Join(containerPaths.PanelsLink, "bbbbbbbb-1111-4222-8333-444444444444.conf"), secondPanelFixed)

	// Also render zz-manual.conf with diverse manual-domain variants and validate in Ferron 3.
	manualEntries := []ManualDomainEntry{
		{
			Domain:            "manual-plain.example.com",
			Upstreams:         []ManualUpstream{{Scheme: "http", Host: "10.0.0.2", Port: 8080}},
			TLS:               ManualTLSNone,
			Enabled:           true,
			Keepalive:         IntPtr(0),
			Websocket:         BoolPtr(false),
			RealIP:            ManualRealIPNone,
			TimeoutSec:        60,
			ConnectTimeoutSec: 5,
		},
		{
			Domain:       "manual-shop.example.com",
			RedirectWWW:  ManualRedirectToApex,
			Upstreams:    []ManualUpstream{{Scheme: "https", Host: "10.0.0.6", Port: 443}, {Scheme: "https", Host: "10.0.0.7", Port: 443}},
			LB:           ManualLBIPHash,
			TLS:          ManualTLSAuto,
			ForceHTTPS:   BoolPtr(false),
			Origin:       ManualOriginOpts{NoVerify: true, SNI: "backend.internal", HostHeader: ManualHostHeaderUpstream},
			RealIP:       ManualRealIPCloudflare,
			HeadersUp:    []ManualHeader{{Name: "X-Edge-Mode", Value: "shield"}},
			HeadersDown:  []ManualHeader{{Name: "X-Frame-Options", Value: "SAMEORIGIN"}},
			Keepalive:    IntPtr(32),
			Enabled:      true,
		},
		{
			Domain:    "*.manual-wild.example.com",
			Upstreams: []ManualUpstream{{Scheme: "http", Host: "10.0.0.9", Port: 9000}},
			TLS:       ManualTLSManual,
			CertFile:  filepath.ToSlash(filepath.Join(containerPaths.ManualCertsDir, "manual-int.example.com.crt")),
			KeyFile:   filepath.ToSlash(filepath.Join(containerPaths.ManualCertsDir, "manual-int.example.com.key")),
			Enabled:   true,
		},
	}
	for i := range manualEntries {
		if err := NormalizeAndValidateEntry(&manualEntries[i], ""); err != nil {
			t.Fatalf("NormalizeAndValidateEntry manual: %v", err)
		}
	}
	manualRes, err := eng.RenderManual(manualEntries, ManualRenderOptions{ACMEEmail: cfg.ACMEEmail})
	if err != nil {
		t.Fatalf("RenderManual ferron: %v", err)
	}
	writeContainerMapped(filepath.Join(containerPaths.PanelsLink, ManualConfFileName), manualRes.Content)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "-v", hostRoot+":"+containerRoot, image, "/usr/local/bin/ferron", "validate", "-c", filepath.ToSlash(containerPaths.MainConfig), "--json", "--config-adapter", "ferronconf")
	cmd.Env = append(os.Environ(), "MSYS_NO_PATHCONV=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker ferron validate failed: %v\noutput:\n%s", err, string(out))
	}
	var parsed ferronValidateJSON
	if jErr := json.Unmarshal(out, &parsed); jErr != nil || !parsed.Valid {
		t.Fatalf("expected valid=true JSON from ferron validate, got err=%v out:\n%s", jErr, string(out))
	}
	t.Logf("ferron validate output:\n%s", strings.TrimSpace(string(out)))

	// Verify 01-health.conf passes ferron validate when 0 routes exist.
	_ = os.Remove(filepath.Join(hostPaths.PanelsLink, doc.RoutesResponse.PanelID+".conf"))
	_ = os.Remove(filepath.Join(hostPaths.PanelsLink, "bbbbbbbb-1111-4222-8333-444444444444.conf"))
	_ = os.Remove(filepath.Join(hostPaths.PanelsLink, ManualConfFileName))
	writeContainerMapped(filepath.Join(containerPaths.PanelsLink, HealthFerronFileName), RenderHealthProbeFerron())
	cmdHealth := exec.CommandContext(ctx, "docker", "run", "--rm", "-v", hostRoot+":"+containerRoot, image, "/usr/local/bin/ferron", "validate", "-c", filepath.ToSlash(containerPaths.MainConfig), "--json", "--config-adapter", "ferronconf")
	cmdHealth.Env = append(os.Environ(), "MSYS_NO_PATHCONV=1")
	outHealth, err := cmdHealth.CombinedOutput()
	if err != nil {
		t.Fatalf("docker ferron validate (health probe) failed: %v\noutput:\n%s", err, string(outHealth))
	}
	var parsedHealth ferronValidateJSON
	if jErr := json.Unmarshal(outHealth, &parsedHealth); jErr != nil || !parsedHealth.Valid {
		t.Fatalf("expected valid=true JSON on health probe, got err=%v out:\n%s", jErr, string(outHealth))
	}
}
