package proxy

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseAndValidEngine(t *testing.T) {
	cases := []struct {
		in   string
		want Engine
		ok   bool
	}{
		{"caddy", EngineCaddy, true},
		{"  ANGIE  ", EngineAngie, true},
		{"Ferron", EngineFerron, true},
		{"nginx", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := ParseEngine(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ParseEngine(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
		if ValidEngine(tc.in) != tc.ok {
			t.Errorf("ValidEngine(%q) = %v, want %v", tc.in, !tc.ok, tc.ok)
		}
	}
}

func fullSampleState() State {
	return State{
		Global: GlobalOptions{
			Profile:          "custom",
			ReadHeader:       "10s",
			ReadBodyIdle:     "30s",
			Idle:             "2m",
			GracePeriod:      "30s",
			MaxHeaderSize:    "16KB",
			RealIP:           "custom",
			TrustedProxies:   []string{"10.0.0.1", "192.168.1.0/24"},
			TrustedStrict:    true,
			ShieldProxies:    []string{"203.0.113.10", "198.51.100.20"},
			DirectCloudflare: true,
		},
		ACMEEmail: "ops@example.com",
		Panel: &Site{
			ID:           "panel",
			Hosts:        []string{"panel.example.com"},
			TLS:          TLSAuto,
			TrustedProxy: true,
			Kind:         SiteKindAppProxy,
			Upstream: &UpstreamRef{
				Name:      "nd_panel",
				Service:   "panel",
				Container: "panel",
				Port:      8080,
				Scheme:    "http",
				RequestHeaders: []HeaderOp{
					{Action: HeaderSet, Name: "X-Real-IP", Value: "{client_ip}"},
					{Action: HeaderSet, Name: "X-Forwarded-For", Value: "{client_ip}"},
				},
			},
		},
		Sites: []Site{
			{
				ID:           "app:app1:dom:1",
				AppID:        "app1",
				DomainID:     1,
				Slot:         "blue",
				Hosts:        []string{"app.example.com", "www.app.example.com"},
				TLS:          TLSAuto,
				BehindShield: false,
				TrustedProxy: false,
				Kind:         SiteKindAppProxy,
				Headers: []HeaderOp{
					{Action: HeaderSet, Name: "X-Frame-Options", Value: "SAMEORIGIN"},
					{Action: HeaderSet, Name: "X-Content-Type-Options", Value: "nosniff"},
				},
				Paths: []PathRoute{
					{
						Prefix:      "/static/*",
						StripPrefix: true,
						Priority:    1,
						Handler: Handler{
							Kind: HandlerStaticDir,
							Static: &StaticDirSpec{
								Root:          "/data/workspaces/app1/static",
								Hide:          []string{".git", ".env*"},
								Precompressed: []string{"gzip", "zstd"},
							},
						},
					},
					{
						Prefix:   "/api/*",
						Priority: 2,
						Handler: Handler{
							Kind:     HandlerProxy,
							Upstream: &UpstreamRef{Name: "nd_app1_api_4000", Service: "api", Container: "app1-blue-api-1", Port: 4000, Scheme: "http", LongStreams: true},
						},
					},
				},
				Upstream: &UpstreamRef{
					Name:          "nd_app1_web_3000_blue",
					Service:       "web",
					Container:     "app1-blue-web-1",
					Port:          3000,
					Scheme:        "http",
					LongStreams:   true,
					LBTryDuration: "5s",
					FailDuration:  "5s",
				},
			},
			{
				ID:       "php:blog",
				AppID:    "blog",
				Hosts:    []string{"blog.example.com"},
				TLS:      TLSInternal,
				Kind:     SiteKindPHP,
				Security: &SecuritySpec{BlockedIPs: []string{"1.2.3.4", "5.6.7.8"}, AllowOnly: []string{"203.0.113.0/24", "198.51.100.0/24"}, BasicAuthUser: "admin", BasicAuthHash: "$2y$10$hash", HSTS: true},
				PHP: &PHPSpec{
					DocRoot:       "/data/workspaces/blog/htdocs",
					Socket:        "/data/workspaces/.nd-php/u1-v83/run/blog.sock",
					PHPVersion:    "8.3",
					SiteType:      "wordpress",
					FallbackMode:  FallbackCustom,
					FallbackEntry: "index.php",
					Compression:   []string{"zstd", "gzip"},
					Precompressed: []string{"gzip", "zstd"},
					HidePatterns:  []string{".git", ".env*"},
					Cache:         &PHPCacheSpec{WordPressPageCache: true},
					Routing: []RoutingRule{
						{ID: "r1", Kind: "rewrite", MatchType: "regex", Pattern: `^/p/(.+)$`, Target: "/post.php?slug=$1", Enabled: true},
						{ID: "r2", Kind: "block", MatchType: "exact", Pattern: "/secret", Status: 404, Enabled: true},
					},
					HTTPS: true,
				},
			},
		},
		Gateways: []GatewayPort{
			{Port: 4013, ContainerPort: 3000, BindIP: "0.0.0.0", Protocol: "tcp", AppID: "app1"},
			{Port: 8090, ContainerPort: 8080, BindIP: "127.0.0.1", Protocol: "tcp", AppID: "app2"},
		},
		SharedMounts: []SharedMount{
			{VolumeName: "vol_b", Target: "/mnt/shared/vol_b", ReadOnly: true},
			{VolumeName: "vol_a", Target: "/mnt/shared/vol_a", ReadOnly: true},
		},
	}
}

func TestComputeGenerationDeterministicAndPure(t *testing.T) {
	var nilState *State
	empty := &State{}
	if nilState.ComputeGeneration() != empty.ComputeGeneration() {
		t.Fatalf("nil state hash %q != empty state hash %q", nilState.ComputeGeneration(), empty.ComputeGeneration())
	}

	s1 := fullSampleState()
	h1 := s1.ComputeGeneration()
	if len(h1) != 64 {
		t.Fatalf("expected 64-char SHA-256 hex, got %q", h1)
	}

	// Setting Generation field on State must not change ComputeGeneration().
	s1WithGen := fullSampleState()
	s1WithGen.Generation = "stale-old-generation"
	if got := s1WithGen.ComputeGeneration(); got != h1 {
		t.Fatalf("Generation field polluted hash: got %q, want %q", got, h1)
	}

	// Reordering unordered collections must yield the exact same hash.
	s2 := fullSampleState()
	s2.Sites[0], s2.Sites[1] = s2.Sites[1], s2.Sites[0]
	s2.Gateways[0], s2.Gateways[1] = s2.Gateways[1], s2.Gateways[0]
	s2.SharedMounts[0], s2.SharedMounts[1] = s2.SharedMounts[1], s2.SharedMounts[0]
	s2.Global.TrustedProxies = []string{"192.168.1.0/24", "10.0.0.1"}
	s2.Global.ShieldProxies = []string{"198.51.100.20", "203.0.113.10"}
	s2.Panel.Upstream.RequestHeaders[0], s2.Panel.Upstream.RequestHeaders[1] = s2.Panel.Upstream.RequestHeaders[1], s2.Panel.Upstream.RequestHeaders[0]
	s2.Sites[1].Headers[0], s2.Sites[1].Headers[1] = s2.Sites[1].Headers[1], s2.Sites[1].Headers[0]
	s2.Sites[0].Security.BlockedIPs = []string{"5.6.7.8", "1.2.3.4"}
	s2.Sites[0].Security.AllowOnly = []string{"198.51.100.0/24", "203.0.113.0/24"}
	s2.Sites[0].PHP.Compression = []string{"gzip", "zstd"}
	s2.Sites[0].PHP.HidePatterns = []string{".env*", ".git"}
	s2.Sites[1].Paths[0].Handler.Static.Hide = []string{".env*", ".git"}

	// Snapshot original order of s1 before computing to verify purity (no mutation).
	origSitesOrder := []string{s1.Sites[0].ID, s1.Sites[1].ID}
	origTrusted := append([]string(nil), s1.Global.TrustedProxies...)

	if h2 := s2.ComputeGeneration(); h2 != h1 {
		t.Fatalf("reordered unordered collections produced different hash:\nh1=%s\nh2=%s", h1, h2)
	}
	if s1.Sites[0].ID != origSitesOrder[0] || !reflect.DeepEqual(s1.Global.TrustedProxies, origTrusted) {
		t.Fatalf("ComputeGeneration mutated caller state")
	}

	// nil vs empty slices must hash identically.
	sNil := &State{}
	sEmpty := &State{
		Sites:        []Site{},
		Gateways:     []GatewayPort{},
		SharedMounts: []SharedMount{},
		Global: GlobalOptions{
			TrustedProxies: []string{},
			ShieldProxies:  []string{},
		},
	}
	if sNil.ComputeGeneration() != sEmpty.ComputeGeneration() {
		t.Fatalf("nil vs empty slices produced different hashes")
	}
}

func TestComputeGenerationSensitiveToMeaningfulChanges(t *testing.T) {
	base := fullSampleState()
	baseHash := base.ComputeGeneration()

	// Swapping ordered PHP routing rules MUST change the hash.
	reorderedRules := fullSampleState()
	r := reorderedRules.Sites[1].PHP.Routing
	r[0], r[1] = r[1], r[0]
	if reorderedRules.ComputeGeneration() == baseHash {
		t.Fatal("reordering ordered PHP routing rules must change generation hash")
	}

	// Swapping ordered PathRoutes MUST change the hash.
	reorderedPaths := fullSampleState()
	p := reorderedPaths.Sites[0].Paths
	p[0], p[1] = p[1], p[0]
	if reorderedPaths.ComputeGeneration() == baseHash {
		t.Fatal("reordering ordered PathRoutes must change generation hash")
	}

	// Changing BasicAuthHash MUST change the hash.
	changedAuth := fullSampleState()
	changedAuth.Sites[1].Security.BasicAuthHash = "$2y$10$different"
	if changedAuth.ComputeGeneration() == baseHash {
		t.Fatal("changing BasicAuthHash must change generation hash")
	}

	// Toggling Suspended MUST change the hash.
	suspended := fullSampleState()
	suspended.Sites[1].PHP.Suspended = true
	if suspended.ComputeGeneration() == baseHash {
		t.Fatal("toggling PHP.Suspended must change generation hash")
	}
}

func TestGlobalOptionsNormalize(t *testing.T) {
	o, err := GlobalOptions{
		ReadHeader:     "15S",
		Idle:           "2m",
		MaxHeaderSize:  "16KB",
		RealIP:         "custom",
		TrustedProxies: []string{" 10.0.0.1 ", "10.0.0.1"},
		TrustedStrict:  true,
		ShieldProxies:  []string{"107.189.6.222"},
	}.Normalize()
	if err != nil {
		t.Fatalf("Normalize valid options: %v", err)
	}
	if o.Profile != "custom" || o.ReadHeader != "15s" || len(o.TrustedProxies) != 1 {
		t.Fatalf("unexpected normalized options: %+v", o)
	}

	for name, bad := range map[string]GlobalOptions{
		"bad duration":    {ReadHeader: "soon"},
		"out of range":    {Idle: "48h"},
		"too short":       {ReadHeader: "10ms"},
		"injection":       {ReadHeader: "15s\nadmin"},
		"bad header size": {MaxHeaderSize: "16 KB"},
		"whole internet":  {RealIP: "custom", TrustedProxies: []string{"0.0.0.0/0"}},
		"empty custom":    {RealIP: "custom"},
		"unknown mode":    {RealIP: "magic"},
		"bad shield ip":   {RealIP: "none", ShieldProxies: []string{"not-an-ip"}},
	} {
		if _, err := bad.Normalize(); err == nil {
			t.Errorf("case %q: expected error, got nil", name)
		}
	}

	// Verify OptionProfiles normalize cleanly.
	for name, prof := range OptionProfiles {
		if _, err := prof.Normalize(); err != nil {
			t.Errorf("OptionProfiles[%q] failed Normalize: %v", name, err)
		}
	}
	if len(CloudflareRanges) == 0 || !strings.Contains(CloudflareRanges[0], "/") {
		t.Fatalf("unexpected CloudflareRanges: %v", CloudflareRanges)
	}
}

func TestPreflightWarnsWhenTargetCannotServeWordPressPageCache(t *testing.T) {
	st := &State{Sites: []Site{{
		ID: "wp", Hosts: []string{"blog.test"}, Kind: SiteKindPHP, TLS: TLSNone,
		PHP: &PHPSpec{Cache: &PHPCacheSpec{WordPressPageCache: true}},
	}}}
	with := EvaluateCapabilities(st, Capabilities{PHPFastCGI: true, WordPressPageCache: true})
	for _, w := range with.SoftWarnings {
		if w.Capability == "wordpress_page_cache" {
			t.Fatalf("no page-cache warning expected when supported: %+v", w)
		}
	}
	without := EvaluateCapabilities(st, Capabilities{PHPFastCGI: true})
	if !without.CanSwitch {
		t.Fatal("a missing page cache must not block the switch")
	}
	found := false
	for _, w := range without.SoftWarnings {
		if w.Capability == "wordpress_page_cache" && len(w.AffectedSites) == 1 && w.AffectedSites[0].Label == "blog.test" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a wordpress_page_cache warning for blog.test, got %+v", without.SoftWarnings)
	}
}
