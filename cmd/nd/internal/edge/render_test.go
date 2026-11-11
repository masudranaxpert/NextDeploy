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
	if !strings.Contains(globalOpts, "trusted_proxies static ") || !strings.Contains(globalOpts, "client_ip_headers CF-Connecting-IP X-Forwarded-For") {
		t.Fatalf("unexpected global.opts:\n%s", globalOpts)
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
