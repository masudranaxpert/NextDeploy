package edge

import (
	"flag"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update-golden", false, "update golden render files")

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *updateGolden {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("update golden %s: %v", path, err)
		}
	}
	wantBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	want := strings.ReplaceAll(string(wantBytes), "\r\n", "\n")
	if got != want {
		t.Fatalf("golden mismatch for %s:\n--- WANT ---\n%s\n--- GOT ---\n%s", name, want, got)
	}
}

func TestRenderCoreTemplates(t *testing.T) {
	paths := DefaultPaths()
	caddyfile := RenderCaddyfileManagedRegion(paths, "localhost:2019", "ops@example.com", "", "")
	if !strings.Contains(caddyfile, ManagedStartMarker) || !strings.Contains(caddyfile, "email ops@example.com") {
		t.Fatalf("unexpected Caddyfile region:\n%s", caddyfile)
	}
	if !strings.Contains(caddyfile, "import /etc/caddy/nd/panels/*.caddy") {
		t.Fatalf("missing panels glob import:\n%s", caddyfile)
	}

	globalOpts := RenderGlobalOpts(DefaultCloudflareRanges)
	if !strings.Contains(globalOpts, "trusted_proxies static ") ||
		!strings.Contains(globalOpts, "trusted_proxies_strict") ||
		!strings.Contains(globalOpts, "client_ip_headers CF-Connecting-IP X-Forwarded-For") {
		t.Fatalf("unexpected global.opts:\n%s", globalOpts)
	}
	if globalOpts != RenderGlobalOptsWithCF(DefaultCloudflareRanges) {
		t.Fatalf("expected RenderGlobalOpts and RenderGlobalOptsWithCF to match")
	}

	common := RenderCommonCaddy()
	if !strings.Contains(common, `header Cache-Control "no-store"`) || !strings.Contains(common, "handle_errors 502 503 504") {
		t.Fatalf("unexpected common.caddy:\n%s", common)
	}
}

func TestRenderPanelGoldenCases(t *testing.T) {
	panelID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"

	cases := []struct {
		name   string
		golden string
		in     RenderPanelInput
	}{
		{
			name:   "zero_routes",
			golden: "render_zero.golden",
			in: RenderPanelInput{
				PanelID:    panelID,
				PanelName:  "vps-1",
				ETag:       "etag-zero",
				OriginIPs:  []netip.Addr{netip.MustParseAddr("204.136.10.51")},
				OriginPort: 443,
				CAStatus:   CAStatusOK,
				Routes:     nil,
			},
		},
		{
			name:   "one_route",
			golden: "render_one.golden",
			in: RenderPanelInput{
				PanelID:    panelID,
				PanelName:  "vps-1",
				ETag:       "etag-one",
				OriginIPs:  []netip.Addr{netip.MustParseAddr("204.136.10.51")},
				OriginPort: 443,
				CAStatus:   CAStatusOK,
				Routes: []EdgeRoute{
					{
						ID:    "dom_12",
						Kind:  KindAppDomain,
						Hosts: []string{"www.app.example.com", "app.example.com"},
					},
				},
			},
		},
		{
			name:   "many_routes_multi_ip_h2",
			golden: "render_many_multi_ip_h2.golden",
			in: RenderPanelInput{
				PanelID:    panelID,
				PanelName:  "vps-1",
				ETag:       "etag-many",
				OriginIPs:  []netip.Addr{netip.MustParseAddr("204.136.10.52"), netip.MustParseAddr("204.136.10.51")},
				OriginPort: 443,
				CAStatus:   CAStatusOK,
				Routes: []EdgeRoute{
					{
						ID:      "proxy_9",
						Kind:    KindProxyRoute,
						Hosts:   []string{"grpc.example.com"},
						Options: RouteOptions{UpstreamH2: true, MaxBodyMB: 64},
					},
					{
						ID:    "dom_2",
						Kind:  KindAppDomain,
						Hosts: []string{"zeta.example.com", "alpha.example.com"},
					},
				},
			},
		},
		{
			name:   "missing_ca",
			golden: "render_missing_ca.golden",
			in: RenderPanelInput{
				PanelID:    panelID,
				PanelName:  "vps-1",
				ETag:       "etag-noca",
				OriginIPs:  []netip.Addr{netip.MustParseAddr("204.136.10.51")},
				OriginPort: 443,
				CAStatus:   CAStatusMissing,
				Routes: []EdgeRoute{
					{
						ID:    "dom_1",
						Kind:  KindAppDomain,
						Hosts: []string{"noca.example.com"},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := RenderPanelCaddy(tc.in)
			if err != nil {
				t.Fatalf("RenderPanelCaddy: %v", err)
			}
			assertGolden(t, tc.golden, got)

			// Verify deterministic byte stability regardless of input slice order.
			got2, err := RenderPanelCaddy(tc.in)
			if err != nil {
				t.Fatalf("second RenderPanelCaddy: %v", err)
			}
			if got != got2 {
				t.Fatalf("non-deterministic render output")
			}
		})
	}
}

func TestRenderPanelDeterministicAcrossOrderPermutations(t *testing.T) {
	panelID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	inA := RenderPanelInput{
		PanelID:   panelID,
		PanelName: "vps-1",
		ETag:      "etag-perm",
		OriginIPs: []netip.Addr{netip.MustParseAddr("204.136.10.51"), netip.MustParseAddr("204.136.10.52")},
		CAStatus:  CAStatusOK,
		Routes: []EdgeRoute{
			{ID: "r1", Hosts: []string{"b.com", "a.com"}},
			{ID: "r2", Hosts: []string{"d.com", "c.com"}},
		},
	}
	inB := RenderPanelInput{
		PanelID:   panelID,
		PanelName: "vps-1",
		ETag:      "etag-perm",
		OriginIPs: []netip.Addr{netip.MustParseAddr("204.136.10.52"), netip.MustParseAddr("204.136.10.51")},
		CAStatus:  CAStatusOK,
		Routes: []EdgeRoute{
			{ID: "r2", Hosts: []string{"c.com", "d.com"}},
			{ID: "r1", Hosts: []string{"a.com", "b.com"}},
		},
	}
	outA, err := RenderPanelCaddy(inA)
	if err != nil {
		t.Fatalf("render A: %v", err)
	}
	outB, err := RenderPanelCaddy(inB)
	if err != nil {
		t.Fatalf("render B: %v", err)
	}
	if outA != outB {
		t.Fatalf("expected byte-identical output across permutations:\nA:\n%s\nB:\n%s", outA, outB)
	}
}

func TestRenderPanelRejectsUnvalidatedHost(t *testing.T) {
	_, err := RenderPanelCaddy(RenderPanelInput{
		PanelID:   "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
		PanelName: "vps-1",
		OriginIPs: []netip.Addr{netip.MustParseAddr("204.136.10.51")},
		CAStatus:  CAStatusMissing,
		Routes: []EdgeRoute{
			{ID: "r1", Hosts: []string{"evil.com {\n respond 200"}},
		},
	})
	if err == nil {
		t.Fatalf("expected RenderPanelCaddy to reject unvalidated Caddyfile-injection host")
	}
}

func TestRenderGlobalOptsForConfig(t *testing.T) {
	preserved := "servers {\n\ttimeouts {\n\t\tread_body 10s\n\t}\n\ttrusted_proxies static 103.21.244.0/22 5.175.220.216\n\tmetrics\n}"

	t.Run("case1_clean_edge_no_trusted_proxy", func(t *testing.T) {
		out := RenderGlobalOptsForConfig(&Config{}, DefaultCloudflareRanges, false)
		if !strings.Contains(out, "# nd-edge: trusted_proxies disabled (no domains require proxy trust)") {
			t.Fatalf("expected disabled comment, got:\n%s", out)
		}
		if strings.Contains(out, "servers {") || strings.Contains(out, "trusted_proxies static") {
			t.Fatalf("expected no servers/trusted_proxies block when no domains require proxy trust, got:\n%s", out)
		}
	})

	t.Run("case1_adopted_edge_no_trusted_proxy_preserves_untouched", func(t *testing.T) {
		out := RenderGlobalOptsForConfig(&Config{
			TrustedProxies:  true,
			PreservedGlobal: preserved,
		}, DefaultCloudflareRanges, false)
		if !strings.Contains(out, "5.175.220.216") || !strings.Contains(out, "read_body 10s") || !strings.Contains(out, "metrics") {
			t.Fatalf("expected preserved directives untouched, got:\n%s", out)
		}
		if strings.Contains(out, "client_ip_headers") || strings.Contains(out, "trusted_proxies_strict") {
			t.Fatalf("expected nd-edge trusted_proxies merge to be skipped when anyTrustedProxy=false, got:\n%s", out)
		}
	})

	t.Run("case2_clean_edge_any_trusted_proxy", func(t *testing.T) {
		out := RenderGlobalOptsForConfig(&Config{}, DefaultCloudflareRanges, true)
		if !strings.Contains(out, "trusted_proxies static ") ||
			!strings.Contains(out, "2400:cb00::/32") ||
			!strings.Contains(out, "173.245.48.0/20") ||
			!strings.Contains(out, "trusted_proxies_strict") ||
			!strings.Contains(out, "client_ip_headers CF-Connecting-IP X-Forwarded-For") {
			t.Fatalf("expected full CF IPv4+IPv6 + strict + client_ip_headers, got:\n%s", out)
		}
	})

	t.Run("case2_adopted_edge_merges_custom_ips_and_cf_ranges", func(t *testing.T) {
		customCF := []string{"173.245.48.0/20", "103.21.244.0/22", "2400:cb00::/32"}
		out := RenderGlobalOptsForConfig(&Config{
			TrustedProxies:  true,
			PreservedGlobal: preserved + "\n\nacme_dns cloudflare token",
		}, customCF, true)
		if strings.Count(out, "servers {") != 1 {
			t.Fatalf("expected single merged servers block, got:\n%s", out)
		}
		for _, want := range []string{
			"173.245.48.0/20",
			"103.21.244.0/22",
			"2400:cb00::/32",
			"5.175.220.216",
			"trusted_proxies_strict",
			"client_ip_headers CF-Connecting-IP X-Forwarded-For",
			"read_body 10s",
			"metrics",
			"acme_dns cloudflare token",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("expected %q in merged global.opts, got:\n%s", want, out)
			}
		}
		if strings.Count(out, "103.21.244.0/22") != 1 {
			t.Fatalf("expected CF CIDR deduplicated, got:\n%s", out)
		}
	})
}

func TestRenderPanelPerDomainTrustedProxySnippets(t *testing.T) {
	panelID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	base := RenderPanelInput{
		PanelID:   panelID,
		PanelName: "vps-1",
		ETag:      "etag-tp",
		OriginIPs: []netip.Addr{netip.MustParseAddr("204.136.10.51")},
		CAStatus:  CAStatusOK,
	}

	// Only direct (TrustedProxy=false) -> only (nd_up_aaaaaaaa), no _tp snippet.
	inDirect := base
	inDirect.Routes = []EdgeRoute{{ID: "r1", Hosts: []string{"direct.example.com"}}}
	outDirect, err := RenderPanelCaddy(inDirect)
	if err != nil {
		t.Fatalf("render direct: %v", err)
	}
	if !strings.Contains(outDirect, "(nd_up_aaaaaaaa) {") || strings.Contains(outDirect, "_tp") {
		t.Fatalf("expected only (nd_up_aaaaaaaa) snippet, got:\n%s", outDirect)
	}
	if !strings.Contains(outDirect, "header_up X-Real-IP {remote_host}") ||
		!strings.Contains(outDirect, "header_up X-Forwarded-For {remote_host}") ||
		!strings.Contains(outDirect, "header_up -CF-Connecting-IP") {
		t.Fatalf("expected direct remote_host + strip CF-Connecting-IP headers, got:\n%s", outDirect)
	}

	// Only trusted_proxy=true -> only (nd_up_aaaaaaaa_tp), no non-_tp snippet.
	inTP := base
	inTP.Routes = []EdgeRoute{{ID: "r1", Hosts: []string{"cf.example.com"}, TrustedProxy: true}}
	outTP, err := RenderPanelCaddy(inTP)
	if err != nil {
		t.Fatalf("render tp: %v", err)
	}
	if strings.Contains(outTP, "(nd_up_aaaaaaaa) {") || !strings.Contains(outTP, "(nd_up_aaaaaaaa_tp) {") {
		t.Fatalf("expected only (nd_up_aaaaaaaa_tp) snippet, got:\n%s", outTP)
	}
	if !strings.Contains(outTP, "header_up X-Real-IP {client_ip}") ||
		!strings.Contains(outTP, "header_up X-Forwarded-For {client_ip}") ||
		strings.Contains(outTP, "{remote_host}") ||
		strings.Contains(outTP, "-CF-Connecting-IP") {
		t.Fatalf("expected client_ip headers without remote_host/-CF-Connecting-IP, got:\n%s", outTP)
	}

	// Mixed -> both snippets rendered and imported by respective domains.
	inMixed := base
	inMixed.Routes = []EdgeRoute{
		{ID: "r1", Hosts: []string{"direct.example.com"}, TrustedProxy: false},
		{ID: "r2", Hosts: []string{"cf.example.com"}, TrustedProxy: true},
	}
	outMixed, err := RenderPanelCaddy(inMixed)
	if err != nil {
		t.Fatalf("render mixed: %v", err)
	}
	if !strings.Contains(outMixed, "(nd_up_aaaaaaaa) {") || !strings.Contains(outMixed, "(nd_up_aaaaaaaa_tp) {") {
		t.Fatalf("expected both snippets rendered, got:\n%s", outMixed)
	}
	if !strings.Contains(outMixed, "direct.example.com {\n\timport nd_common\n\timport nd_up_aaaaaaaa\n}") {
		t.Fatalf("expected direct route to import nd_up_aaaaaaaa, got:\n%s", outMixed)
	}
	if !strings.Contains(outMixed, "cf.example.com {\n\timport nd_common\n\timport nd_up_aaaaaaaa_tp\n}") {
		t.Fatalf("expected tp route to import nd_up_aaaaaaaa_tp, got:\n%s", outMixed)
	}
}
