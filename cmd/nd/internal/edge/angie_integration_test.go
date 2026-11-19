//go:build integration

package edge

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAngieDockerConfigValidation(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker binary not found; skipping live angie -t test")
	}
	image := "docker.angie.software/angie:1.12.4"
	if out, err := exec.Command("docker", "image", "inspect", image).CombinedOutput(); err != nil {
		t.Skipf("angie image %s not available locally (%v: %s)", image, err, strings.TrimSpace(string(out)))
	}

	hostRoot := t.TempDir()
	containerRoot := "/work"
	containerPaths := NewAngieEnginePaths(containerRoot)
	hostPaths := NewAngieEnginePaths(hostRoot)

	for _, dir := range []string{
		filepath.Dir(hostPaths.MainConfig),
		hostPaths.ManagedDir,
		hostPaths.PanelsLink,
		hostPaths.OriginsLink,
		hostPaths.UserDir,
		hostPaths.CertsDir,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	cfg := &Config{
		EdgeID:    "edge-int-1",
		ACMEEmail: "ops@example.com",
		ShieldIPs: []string{"107.189.6.222"},
	}
	eng := NewAngieEngineWithConfig(containerPaths, cfg, nil)

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
	writeContainerMapped(filepath.Join(containerPaths.UserDir, PlaceholderAngieFileName), RenderPlaceholderAngie())

	// Render panel from edge_contract.json + multi-route H2/TrustedProxy case.
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
				Hosts:        []string{"second.example.com"},
				TrustedProxy: false,
				Options:      RouteOptions{Websocket: true},
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderPanel second: %v", err)
	}
	writeContainerMapped(filepath.Join(containerPaths.PanelsLink, "bbbbbbbb-1111-4222-8333-444444444444.conf"), secondPanel)

	// Also render zz-manual.conf with diverse manual-domain variants and validate in Angie.
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
			LB:           ManualLBLeastConn,
			TLS:          ManualTLSAuto,
			ForceHTTPS:   BoolPtr(false),
			Origin:       ManualOriginOpts{NoVerify: true, SNI: "backend.internal", HostHeader: ManualHostHeaderUpstream},
			MaxBodyBytes: 100 * 1024 * 1024,
			RealIP:       ManualRealIPCloudflare,
			HeadersUp:    []ManualHeader{{Name: "X-Edge-Mode", Value: "shield"}},
			HeadersDown:  []ManualHeader{{Name: "X-Frame-Options", Value: "SAMEORIGIN"}},
			Keepalive:    IntPtr(32),
			Enabled:      true,
		},
	}
	for i := range manualEntries {
		if err := NormalizeAndValidateEntry(&manualEntries[i], ""); err != nil {
			t.Fatalf("NormalizeAndValidateEntry manual: %v", err)
		}
	}
	manualRes, err := eng.RenderManual(manualEntries, ManualRenderOptions{ACMEEmail: cfg.ACMEEmail})
	if err != nil {
		t.Fatalf("RenderManual angie: %v", err)
	}
	writeContainerMapped(filepath.Join(containerPaths.PanelsLink, ManualConfFileName), manualRes.Content)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "-v", hostRoot+":"+containerRoot, image, "angie", "-t", "-c", filepath.ToSlash(containerPaths.MainConfig))
	cmd.Env = append(os.Environ(), "MSYS_NO_PATHCONV=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker angie -t failed: %v\noutput:\n%s", err, string(out))
	}
	if !strings.Contains(string(out), "test is successful") {
		t.Fatalf("expected 'test is successful' in angie -t output, got:\n%s", string(out))
	}
	t.Logf("angie -t output:\n%s", strings.TrimSpace(string(out)))

	// Also verify 01-health.conf passes angie -t when 0 routes exist.
	if err := os.Remove(filepath.Join(hostPaths.PanelsLink, doc.RoutesResponse.PanelID+".conf")); err != nil {
		t.Fatalf("remove contract conf: %v", err)
	}
	if err := os.Remove(filepath.Join(hostPaths.PanelsLink, "bbbbbbbb-1111-4222-8333-444444444444.conf")); err != nil {
		t.Fatalf("remove second conf: %v", err)
	}
	_ = os.Remove(filepath.Join(hostPaths.PanelsLink, ManualConfFileName))
	writeContainerMapped(filepath.Join(containerPaths.PanelsLink, HealthAngieFileName), RenderHealthProbeAngie())
	cmdHealth := exec.CommandContext(ctx, "docker", "run", "--rm", "-v", hostRoot+":"+containerRoot, image, "angie", "-t", "-c", filepath.ToSlash(containerPaths.MainConfig))
	cmdHealth.Env = append(os.Environ(), "MSYS_NO_PATHCONV=1")
	outHealth, err := cmdHealth.CombinedOutput()
	if err != nil {
		t.Fatalf("docker angie -t (health probe) failed: %v\noutput:\n%s", err, string(outHealth))
	}
}

func generateIntegrationCAPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "ND Integration Origin CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA cert: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
