package edge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestManualUpstreamAndDomainParsing(t *testing.T) {
	t.Run("upstream_forms", func(t *testing.T) {
		cases := []struct {
			raw      string
			override string
			wantURL  string
		}{
			{raw: "1.2.3.4", wantURL: "http://1.2.3.4:80"},
			{raw: "1.2.3.4:8080", wantURL: "http://1.2.3.4:8080"},
			{raw: "1.2.3.4:443", wantURL: "https://1.2.3.4:443"},
			{raw: "1.2.3.4:443", override: "http", wantURL: "http://1.2.3.4:443"},
			{raw: "https://origin.example.com", wantURL: "https://origin.example.com:443"},
			{raw: "http://origin.example.com:9000", override: "https", wantURL: "https://origin.example.com:9000"},
			{raw: "[2001:db8::1]:8080", wantURL: "http://[2001:db8::1]:8080"},
			{raw: "[2001:db8::1]:443", wantURL: "https://[2001:db8::1]:443"},
		}
		for _, tc := range cases {
			got, err := ParseManualUpstream(tc.raw, tc.override)
			if err != nil {
				t.Fatalf("ParseManualUpstream(%q, %q) failed: %v", tc.raw, tc.override, err)
			}
			if got.URL() != tc.wantURL {
				t.Fatalf("ParseManualUpstream(%q, %q) = %s, want %s", tc.raw, tc.override, got.URL(), tc.wantURL)
			}
		}

		ups, err := ParseManualUpstreamList("10.0.0.1:80, 10.0.0.2:80", "")
		if err != nil || len(ups) != 2 {
			t.Fatalf("ParseManualUpstreamList multi failed: ups=%+v err=%v", ups, err)
		}
	})

	t.Run("invalid_upstreams", func(t *testing.T) {
		bad := []string{
			"",
			"http://1.2.3.4/api",
			"https://1.2.3.4?foo=bar",
			"1.2.3.4/path",
			"1.2.3.4:99999",
			"1.2.3.4:0",
			"ftp://1.2.3.4:21",
			"1.2.3.4;rm",
			"1.2.3.4{bad}",
			"0.0.0.0:80",
			"user:pass@1.2.3.4:80",
		}
		for _, raw := range bad {
			if u, err := ParseManualUpstream(raw, ""); err == nil {
				t.Fatalf("expected error for invalid upstream %q, got %+v", raw, u)
			}
		}
		if _, err := ParseManualUpstreamList("10.0.0.1:80,10.0.0.1:80", ""); err == nil {
			t.Fatal("expected duplicate upstream rejection")
		}
	})

	t.Run("domain_idna_and_wildcard_rules", func(t *testing.T) {
		idn, err := NormalizeManualDomain("Bücher.COM.", ManualTLSAuto, "")
		if err != nil || idn != "xn--bcher-kva.com" {
			t.Fatalf("expected punycode xn--bcher-kva.com, got %q (err=%v)", idn, err)
		}
		if _, err := NormalizeManualDomain("*.example.com", ManualTLSAuto, ""); err == nil {
			t.Fatal("expected wildcard rejection with tls=auto")
		}
		for _, mode := range []string{ManualTLSManual, ManualTLSNone} {
			w, err := NormalizeManualDomain("*.Example.COM.", mode, "")
			if err != nil || w != "*.example.com" {
				t.Fatalf("expected *.example.com for %s, got %q (err=%v)", mode, w, err)
			}
		}
		if _, err := NormalizeManualDomain("*.*.example.com", ManualTLSManual, ""); err == nil {
			t.Fatal("expected double wildcard rejection")
		}
		if _, err := NormalizeManualDomain("bad{host}.com", ManualTLSAuto, ""); err == nil {
			t.Fatal("expected metacharacter host rejection")
		}
	})

	t.Run("sizes_durations_headers_and_mixed_schemes", func(t *testing.T) {
		if sz, err := ParseByteSize("100m"); err != nil || sz != 100*1024*1024 {
			t.Fatalf("ParseByteSize(100m) = %d, %v", sz, err)
		}
		if sz, err := ParseByteSize("0"); err != nil || sz != 0 {
			t.Fatalf("ParseByteSize(0) = %d, %v", sz, err)
		}
		if d, err := ParsePositiveDurationSec("5m"); err != nil || d != 300 {
			t.Fatalf("ParsePositiveDurationSec(5m) = %d, %v", d, err)
		}
		if hdr, err := ParseHeaderPair("X-Custom=foo bar"); err != nil || hdr.Name != "X-Custom" || hdr.Value != "foo bar" {
			t.Fatalf("ParseHeaderPair = %+v, %v", hdr, err)
		}
		if _, err := ParseHeaderPair("Bad Header=val"); err == nil {
			t.Fatal("expected invalid header name rejection")
		}

		e := &ManualDomainEntry{
			Domain:    "mix.example.com",
			TLS:       ManualTLSNone,
			Upstreams: []ManualUpstream{{Scheme: "http", Host: "10.0.0.1", Port: 80}, {Scheme: "https", Host: "10.0.0.2", Port: 443}},
		}
		if err := NormalizeAndValidateEntry(e, ""); err == nil {
			t.Fatal("expected error when mixing http and https upstreams")
		}
	})
}

func TestManualStoreTrashCertImportAndConflicts(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	paths := NewPaths(root)
	epAngie := paths.EnginePathsFor(EngineAngie)

	// Generate and import a cert pair; confirm copied into Managed/certs rather than referenced in-place.
	srcDir := t.TempDir()
	srcCert := filepath.Join(srcDir, "orig.crt")
	srcKey := filepath.Join(srcDir, "orig.key")
	certPEM, keyPEM := generateTestLeafCertAndKey(t, []string{"*.wild.example.com"}, now.Add(-time.Hour), now.Add(60*24*time.Hour))
	if err := os.WriteFile(srcCert, certPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcKey, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	dstCert, dstKey, err := ImportManualTLSCertPair(epAngie.ManualCertsDir, "*.wild.example.com", srcCert, srcKey, now)
	if err != nil {
		t.Fatalf("ImportManualTLSCertPair: %v", err)
	}
	if dstCert == srcCert || !strings.HasPrefix(dstCert, epAngie.ManualCertsDir) {
		t.Fatalf("cert must be copied into nd certs dir, got %s", dstCert)
	}
	if _, err := os.Stat(dstKey); err != nil {
		t.Fatalf("copied key missing: %v", err)
	}

	entry := ManualDomainEntry{
		Domain:    "*.wild.example.com",
		Upstreams: []ManualUpstream{{Scheme: "http", Host: "10.0.0.5", Port: 8080}},
		TLS:       ManualTLSManual,
		CertFile:  dstCert,
		KeyFile:   dstKey,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := NormalizeAndValidateEntry(&entry, ""); err != nil {
		t.Fatalf("NormalizeAndValidateEntry: %v", err)
	}

	store := &ManualDomainsFile{Schema: ManualDomainsSchema, Domains: []ManualDomainEntry{entry}}
	if err := SaveManualDomains(paths.DomainsFile, store); err != nil {
		t.Fatalf("SaveManualDomains: %v", err)
	}
	reloaded, err := LoadManualDomains(paths.DomainsFile)
	if err != nil || len(reloaded.Domains) != 1 || reloaded.Domains[0].Domain != "*.wild.example.com" {
		t.Fatalf("LoadManualDomains mismatch: %+v, err=%v", reloaded, err)
	}

	// Trash retention + restore round trip.
	trash := &ManualTrashFile{Schema: ManualDomainsSchema}
	expiredEntry := entry
	expiredEntry.Domain = "expired-trash.example.com"
	trash.Entries = append(trash.Entries, ManualTrashEntry{
		Entry:     expiredEntry,
		RemovedAt: now.Add(-31 * 24 * time.Hour),
		Reason:    "manual",
	})
	MoveToTrash(trash, entry, "manual", now)
	if err := SaveManualTrash(paths.DomainsTrashFile, trash, now); err != nil {
		t.Fatalf("SaveManualTrash: %v", err)
	}
	loadedTrash, err := LoadManualTrash(paths.DomainsTrashFile, now)
	if err != nil {
		t.Fatalf("LoadManualTrash: %v", err)
	}
	if len(loadedTrash.Entries) != 1 || loadedTrash.Entries[0].Entry.Domain != "*.wild.example.com" {
		t.Fatalf("expected expired trash pruned & recent entry kept, got %+v", loadedTrash.Entries)
	}

	// Export / Import round trip.
	rawExport, err := ExportManualDomainsJSON(reloaded)
	if err != nil {
		t.Fatalf("ExportManualDomainsJSON: %v", err)
	}
	imported, err := ParseImportedManualDomains(rawExport, "")
	if err != nil || len(imported.Domains) != 1 {
		t.Fatalf("ParseImportedManualDomains: %+v, err=%v", imported, err)
	}

	// Conflicts: panel claim + user conf + unmanaged Caddyfile block.
	claims := NewClaimsStore()
	claims.Claims["panel-owned.example.com"] = Claim{PanelID: "panel-1", ClaimedAt: now}
	_ = os.MkdirAll(epAngie.UserDir, 0o755)
	_ = os.WriteFile(filepath.Join(epAngie.UserDir, "custom.conf"), []byte("server {\n\tlisten 80;\n\tserver_name user-owned.example.com;\n}\n"), 0o644)
	_ = os.MkdirAll(filepath.Dir(paths.Caddyfile), 0o755)
	_ = os.WriteFile(paths.Caddyfile, []byte("unmanaged-caddy.example.com {\n\treverse_proxy 127.0.0.1:9999\n}\n"+RenderCaddyfileManagedRegion(paths, "", "", "", "")), 0o644)

	panelCandidate := ManualDomainEntry{Domain: "panel-owned.example.com", Upstreams: []ManualUpstream{{Scheme: "http", Host: "10.0.0.2", Port: 80}}, TLS: ManualTLSAuto}
	_ = NormalizeAndValidateEntry(&panelCandidate, "")
	if err := CheckManualHostConflicts(panelCandidate, &ManualDomainsFile{Schema: 1}, claims, paths, EngineAngie, ""); err == nil || !strings.Contains(err.Error(), "panel-1") {
		t.Fatalf("expected panel conflict error naming panel-1, got %v", err)
	}

	userCandidate := ManualDomainEntry{Domain: "user-owned.example.com", Upstreams: []ManualUpstream{{Scheme: "http", Host: "10.0.0.2", Port: 80}}, TLS: ManualTLSAuto}
	_ = NormalizeAndValidateEntry(&userCandidate, "")
	if err := CheckManualHostConflicts(userCandidate, &ManualDomainsFile{Schema: 1}, claims, paths, EngineAngie, ""); err == nil || !strings.Contains(err.Error(), "custom.conf") {
		t.Fatalf("expected user config conflict naming custom.conf, got %v", err)
	}

	caddyCandidate := ManualDomainEntry{Domain: "unmanaged-caddy.example.com", Upstreams: []ManualUpstream{{Scheme: "http", Host: "10.0.0.2", Port: 80}}, TLS: ManualTLSAuto}
	_ = NormalizeAndValidateEntry(&caddyCandidate, "")
	if err := CheckManualHostConflicts(caddyCandidate, &ManualDomainsFile{Schema: 1}, claims, paths, EngineCaddy, ""); err == nil || !strings.Contains(err.Error(), "Caddyfile") {
		t.Fatalf("expected unmanaged Caddyfile conflict, got %v", err)
	}
}

func buildGoldenManualEntries(t *testing.T, now time.Time) ([]ManualDomainEntry, string) {
	t.Helper()
	// Deterministic root-agnostic paths in golden entries.
	manualCertsDir := t.TempDir()
	certPEM, keyPEM := generateTestLeafCertAndKey(t, []string{"reused.example.com"}, now.Add(-time.Hour), now.Add(60*24*time.Hour))
	if err := os.WriteFile(filepath.Join(manualCertsDir, "reused.example.com.crt"), certPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manualCertsDir, "reused.example.com.key"), keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	entries := []ManualDomainEntry{
		{
			Domain:            "plain.example.com",
			Upstreams:         []ManualUpstream{{Scheme: "http", Host: "10.0.0.2", Port: 8080}},
			TLS:               ManualTLSNone,
			Enabled:           true,
			Websocket:         BoolPtr(false),
			Keepalive:         IntPtr(0),
			RealIP:            ManualRealIPNone,
			TimeoutSec:        60,
			ConnectTimeoutSec: 5,
		},
		{
			Domain:      "shop.example.com",
			RedirectWWW: ManualRedirectToApex,
			Aliases:     []string{"reused.example.com"},
			Upstreams:   []ManualUpstream{{Scheme: "http", Host: "10.0.0.5", Port: 8080}},
			TLS:         ManualTLSAuto,
			ForceHTTPS:  BoolPtr(true),
			Websocket:   BoolPtr(true),
			MaxBodyBytes: 100 * 1024 * 1024,
			RealIP:      ManualRealIPCloudflare,
			HeadersUp:   []ManualHeader{{Name: "X-Edge-Mode", Value: "shield"}},
			HeadersDown: []ManualHeader{{Name: "X-Frame-Options", Value: "SAMEORIGIN"}},
			Keepalive:   IntPtr(32),
			Enabled:     true,
		},
		{
			Domain:    "api.example.com",
			WWW:       true,
			Upstreams: []ManualUpstream{{Scheme: "https", Host: "10.0.0.6", Port: 443}, {Scheme: "https", Host: "10.0.0.7", Port: 443}},
			LB:        ManualLBLeastConn,
			TLS:       ManualTLSAuto,
			ForceHTTPS: BoolPtr(false),
			Origin: ManualOriginOpts{
				NoVerify:   true,
				SNI:        "backend.internal",
				HostHeader: ManualHostHeaderUpstream,
			},
			Websocket: BoolPtr(true),
			RealIP:    ManualRealIPNone,
			Keepalive: IntPtr(64),
			Enabled:   true,
		},
		{
			Domain:    "*.custom.example.com",
			Upstreams: []ManualUpstream{{Scheme: "http", Host: "10.0.0.9", Port: 9000}},
			TLS:       ManualTLSManual,
			CertFile:  "/etc/nd-certs/_wildcard_.custom.example.com.crt",
			KeyFile:   "/etc/nd-certs/_wildcard_.custom.example.com.key",
			LB:        ManualLBIPHash,
			Enabled:   true,
		},
		{
			Domain:    "disabled.example.com",
			Upstreams: []ManualUpstream{{Scheme: "http", Host: "10.0.0.99", Port: 80}},
			TLS:       ManualTLSAuto,
			Enabled:   false,
		},
	}
	for i := range entries {
		if err := NormalizeAndValidateEntry(&entries[i], ""); err != nil {
			t.Fatalf("NormalizeAndValidateEntry(%s): %v", entries[i].Domain, err)
		}
	}
	return entries, manualCertsDir
}

func TestManualRenderGoldenAllEngines(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	entries, manualCertsDir := buildGoldenManualEntries(t, now)
	// Swap order to verify rendering determinism regardless of input slice ordering.
	reversed := []ManualDomainEntry{entries[4], entries[3], entries[2], entries[1], entries[0]}

	opts := ManualRenderOptions{
		Now:            now,
		ACMEEmail:      "ops@example.com",
		ManualCertsDir: manualCertsDir,
	}

	normalizeTempPaths := func(s string) string {
		return strings.ReplaceAll(s, filepath.ToSlash(manualCertsDir), "/etc/edge/manual-certs")
	}

	caddyEng := NewCaddyEngineWithConfig(DefaultPaths(), &Config{ACMEEmail: "ops@example.com"}, nil)
	angieEng := NewAngieEngineWithConfig(DefaultAngieEnginePaths(), &Config{ACMEEmail: "ops@example.com"}, nil)
	ferronEng := NewFerronEngineWithConfig(DefaultFerronEnginePaths(), &Config{ACMEEmail: "ops@example.com"}, nil)

	t.Run("caddy", func(t *testing.T) {
		r1, err := caddyEng.RenderManual(entries, opts)
		if err != nil {
			t.Fatal(err)
		}
		r2, err := caddyEng.RenderManual(reversed, opts)
		if err != nil {
			t.Fatal(err)
		}
		if r1.Content != r2.Content {
			t.Fatal("caddy manual rendering is not deterministic")
		}
		if strings.Contains(r1.Content, "disabled.example.com") {
			t.Fatal("disabled entry must not be rendered")
		}
		assertGolden(t, "manual_caddy.golden", normalizeTempPaths(r1.Content))
	})

	t.Run("angie", func(t *testing.T) {
		r1, err := angieEng.RenderManual(entries, opts)
		if err != nil {
			t.Fatal(err)
		}
		r2, err := angieEng.RenderManual(reversed, opts)
		if err != nil {
			t.Fatal(err)
		}
		if r1.Content != r2.Content {
			t.Fatal("angie manual rendering is not deterministic")
		}
		if strings.Contains(r1.Content, "disabled.example.com") {
			t.Fatal("disabled entry must not be rendered")
		}
		// Critical Shield 107 regression guard: never emit set_real_ip_from inside zz-manual.conf.
		if strings.Contains(r1.Content, "set_real_ip_from") || strings.Contains(r1.Content, "real_ip_header") {
			t.Fatalf("zz-manual.conf must never emit global real_ip directives:\n%s", r1.Content)
		}
		assertGolden(t, "manual_angie.golden", normalizeTempPaths(r1.Content))
	})

	t.Run("ferron", func(t *testing.T) {
		r1, err := ferronEng.RenderManual(entries, opts)
		if err != nil {
			t.Fatal(err)
		}
		r2, err := ferronEng.RenderManual(reversed, opts)
		if err != nil {
			t.Fatal(err)
		}
		if r1.Content != r2.Content {
			t.Fatal("ferron manual rendering is not deterministic")
		}
		if strings.Contains(r1.Content, "disabled.example.com") {
			t.Fatal("disabled entry must not be rendered")
		}
		assertGolden(t, "manual_ferron.golden", normalizeTempPaths(r1.Content))
	})
}

func TestManualReconcileZeroPanelsConflictsAndDNSGate(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	paths := NewPaths(root)
	caddy := &fakeCaddyRunner{}
	systemd := &fakeSystemdRunner{active: true}
	cfIPs := NewCFIPStore()
	_ = cfIPs.SetRanges([]string{"173.245.48.0/20", "2400:cb00::/32"})

	dnsRes := newFakeDNSResolver()
	// Pending domain resolves elsewhere; cf-proxied resolves to CF IP.
	dnsRes.setBoth("pending-manual.example.com", dns.TypeA, msgA("pending-manual.example.com", "203.0.113.50"), nil)
	dnsRes.setBoth("pending-manual.example.com", dns.TypeAAAA, msgAAAA("pending-manual.example.com"), nil)
	dnsRes.setBoth("cf-manual.example.com", dns.TypeA, msgA("cf-manual.example.com", "173.245.48.10"), nil)
	dnsRes.setBoth("cf-manual.example.com", dns.TypeAAAA, msgAAAA("cf-manual.example.com"), nil)

	dnsGate := NewDNSGate(DNSGateOptions{Resolver: dnsRes, CFIPs: cfIPs, CachePath: paths.DNSCacheFile})
	rec := NewReconciler(paths, dnsGate, NewApplier(paths, caddy), NewCertProbe(paths.CaddyCertsDir, systemd), systemd, cfIPs)
	rec.SetExtraShieldIPs([]netip.Addr{netip.MustParseAddr("107.189.6.222")})

	mCF := ManualDomainEntry{
		Domain:    "cf-manual.example.com",
		Upstreams: []ManualUpstream{{Scheme: "http", Host: "10.0.0.5", Port: 8080}},
		TLS:       ManualTLSAuto,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	mPending := ManualDomainEntry{
		Domain:    "pending-manual.example.com",
		Upstreams: []ManualUpstream{{Scheme: "http", Host: "10.0.0.6", Port: 8080}},
		TLS:       ManualTLSAuto,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_ = NormalizeAndValidateEntry(&mCF, "")
	_ = NormalizeAndValidateEntry(&mPending, "")
	_ = SaveManualDomains(paths.DomainsFile, &ManualDomainsFile{
		Schema:  ManualDomainsSchema,
		Domains: []ManualDomainEntry{mCF, mPending},
	})

	// Reconcile with 0 panels: must NOT skip boot, renders cf-manual, skips pending-manual.
	cfg := &Config{
		EdgeID:     "edge-manual-1",
		ACMEEmail:  "ops@example.com",
		ShieldIPs:  []string{"107.189.6.222"},
		Panels:     nil,
	}
	claims := NewClaimsStore()
	meta := &ApplyMeta{}
	out, err := rec.ReconcileOnce(context.Background(), cfg, map[string]*PanelState{}, claims, meta, false, false, now)
	if err != nil {
		t.Fatalf("ReconcileOnce with 0 panels failed: %v", err)
	}
	if out.SkippedEmptyBoot || !out.Applied {
		t.Fatalf("expected Applied=true and SkippedEmptyBoot=false, got %+v", out)
	}

	// Verify global.opts enabled trusted proxies because cf-manual was CF-proxied.
	globalBytes, _ := os.ReadFile(paths.GlobalOpts)
	if !strings.Contains(string(globalBytes), "trusted_proxies static") {
		t.Fatalf("expected global.opts to trust CF for cf-manual, got:\n%s", string(globalBytes))
	}
	activePanelsDir, linkErr := ReadDirLink(paths.PanelsLink)
	if linkErr != nil || activePanelsDir == "" {
		t.Fatalf("ReadDirLink(PanelsLink): dir=%q err=%v", activePanelsDir, linkErr)
	}
	manualBytes, _ := os.ReadFile(filepath.Join(activePanelsDir, ManualCaddyFileName))
	if !strings.Contains(string(manualBytes), "cf-manual.example.com") {
		t.Fatalf("expected cf-manual.example.com rendered in zz-manual.caddy:\n%s", string(manualBytes))
	}
	if strings.Contains(string(manualBytes), "pending-manual.example.com") {
		t.Fatalf("pending-manual.example.com must not be rendered before DNS points to Shield:\n%s", string(manualBytes))
	}

	// Now add a panel claiming cf-manual.example.com -> panel must get StateConflict instead of overriding manual.
	cfg.Panels = []PanelConfig{{PanelID: "p1", PanelName: "panel-one", RegisteredAt: now.Add(time.Minute)}}
	stP1 := NewPanelState("p1", "panel-one")
	stP1.LastGoodETag = "e1"
	stP1.LastGood = &RoutesResponse{
		APIVersion: 1,
		PanelID:    "p1",
		ETag:       "e1",
		Origin:     OriginInfo{IPs: []string{"204.136.10.51"}, Port: 443, CAStatus: CAStatusMissing},
		Routes:     []EdgeRoute{{ID: "r1", Hosts: []string{"cf-manual.example.com"}}},
	}
	out2, err := rec.ReconcileOnce(context.Background(), cfg, map[string]*PanelState{"p1": stP1}, claims, meta, false, false, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("ReconcileOnce with conflicting panel failed: %v", err)
	}
	_ = out2
	if len(stP1.Conflicts) != 1 || stP1.Conflicts[0].Host != "cf-manual.example.com" || stP1.Conflicts[0].OwnerPanel != "manual" {
		t.Fatalf("expected panel conflict owned by manual, got %+v", stP1.Conflicts)
	}
	if stP1.HostStates["cf-manual.example.com"].State != StateConflict {
		t.Fatalf("expected StateConflict on panel host, got %+v", stP1.HostStates["cf-manual.example.com"])
	}
}

func TestManualPruneAndOriginHealth(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	shieldIPs := []netip.Addr{netip.MustParseAddr("107.189.6.222")}
	cfIPs := NewCFIPStore()
	_ = cfIPs.SetRanges([]string{"173.245.48.0/20", "2400:cb00::/32"})

	dnsRes := newFakeDNSResolver()
	dnsRes.setBoth("dead.example.com", dns.TypeA, msgNXDOMAIN(), nil)
	dnsRes.setBoth("dead.example.com", dns.TypeAAAA, msgNXDOMAIN(), nil)
	dnsRes.setBoth("proxied.example.com", dns.TypeA, msgA("proxied.example.com", "173.245.48.55"), nil)
	dnsRes.setBoth("proxied.example.com", dns.TypeAAAA, msgAAAA("proxied.example.com"), nil)
	dnsRes.setBoth("healthy.example.com", dns.TypeA, msgA("healthy.example.com", "107.189.6.222"), nil)
	dnsRes.setBoth("healthy.example.com", dns.TypeAAAA, msgAAAA("healthy.example.com"), nil)

	gate := NewDNSGate(DNSGateOptions{Resolver: dnsRes, CFIPs: cfIPs})
	ctx := context.Background()

	if ins := gate.InspectHostFresh(ctx, "dead.example.com", shieldIPs); ins.Classification != "nxdomain" {
		t.Fatalf("expected nxdomain, got %+v", ins)
	}
	if ins := gate.InspectHostFresh(ctx, "proxied.example.com", shieldIPs); ins.Classification != "ok_proxied" {
		t.Fatalf("expected ok_proxied, got %+v", ins)
	}
	if ins := gate.InspectHostFresh(ctx, "healthy.example.com", shieldIPs); ins.Classification != "ok" {
		t.Fatalf("expected ok, got %+v", ins)
	}

	// Origin probe against live HTTP test server vs closed port.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()

	upEntry := ManualDomainEntry{Domain: "healthy.example.com"}
	uUp, err := ParseManualUpstream(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	upEntry.Upstreams = []ManualUpstream{uUp}
	if res := CheckOriginHealth(ctx, upEntry, 2*time.Second); res.Status != "up" {
		t.Fatalf("expected origin up, got %+v", res)
	}

	srv.Close()
	if res := CheckOriginHealth(ctx, upEntry, 2*time.Second); res.Status != "refused" {
		t.Fatalf("expected origin refused after server close, got %+v", res)
	}

	// Grace + >=2 check rule for EvaluatePruneCandidates.
	deadEntry := ManualDomainEntry{Domain: "dead.example.com", TLS: ManualTLSAuto}
	proxiedEntry := ManualDomainEntry{Domain: "proxied.example.com", TLS: ManualTLSAuto}
	healthyEntry := ManualDomainEntry{Domain: "healthy.example.com", TLS: ManualTLSAuto}
	entries := []ManualDomainEntry{deadEntry, proxiedEntry, healthyEntry}

	health := &DomainsHealthStore{Schema: 1, Records: make(map[string]DomainHealthStat)}
	reports := make(map[string]DomainCheckReport)

	// 1st check at day 0 -> must NEVER prune on single failed check.
	for _, e := range entries {
		dnsIns := gate.InspectHostFresh(ctx, e.Domain, shieldIPs)
		health.Record(e.Domain, dnsIns.Classification, "up", now)
		reports[e.Domain] = DomainCheckReport{Domain: e.Domain, DNS: dnsIns, Origin: OriginHealthResult{Status: "up"}}
	}
	cands1 := EvaluatePruneCandidates(entries, reports, health, PruneReasonDNS, 7*24*time.Hour, now)
	if len(cands1) != 0 {
		t.Fatalf("must not prune after single failed check: %+v", cands1)
	}

	// 2nd check at day 3 -> inside 7d grace -> still not pruned.
	t3 := now.Add(3 * 24 * time.Hour)
	health.Record("dead.example.com", "nxdomain", "up", t3)
	cands2 := EvaluatePruneCandidates(entries, reports, health, PruneReasonDNS, 7*24*time.Hour, t3)
	if len(cands2) != 0 {
		t.Fatalf("must not prune inside grace period: %+v", cands2)
	}

	// Check at day 8 -> >=2 checks and past 7d grace -> dead.example.com pruned, proxied & healthy kept.
	t8 := now.Add(8 * 24 * time.Hour)
	health.Record("dead.example.com", "nxdomain", "up", t8)
	health.Record("proxied.example.com", "ok_proxied", "up", t8)
	health.Record("healthy.example.com", "ok", "up", t8)
	cands3 := EvaluatePruneCandidates(entries, reports, health, PruneReasonDNS, 7*24*time.Hour, t8)
	if len(cands3) != 1 || cands3[0].Entry.Domain != "dead.example.com" {
		t.Fatalf("expected only dead.example.com pruned, got %+v", cands3)
	}
}

func TestAngieManualUpstreamResolvesHostnames(t *testing.T) {
	var b strings.Builder
	writeAngieManualUpstream(&b, "m_x", ManualDomainEntry{Upstreams: []ManualUpstream{
		{Scheme: "https", Host: "origin.example.com", Port: 443},
		{Scheme: "http", Host: "10.0.0.5", Port: 80},
		{Scheme: "http", Host: "2001:db8::1", Port: 8080},
	}})
	out := b.String()
	for _, want := range []string{"server origin.example.com:443 resolve;", "server 10.0.0.5:80;", "server [2001:db8::1]:8080;"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
