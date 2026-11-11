package edge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReconcilerGlobalTrustedProxiesLifecycle(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 11, 6, 0, 0, 0, time.UTC)

	t.Run("clean_edge_toggles_global_opts_on_any_trusted_proxy", func(t *testing.T) {
		paths := NewPaths(t.TempDir())
		caddy := &fakeCaddyRunner{}
		systemd := &fakeSystemdRunner{active: true}
		cfIPs := NewCFIPStore()
		_ = cfIPs.SetRanges([]string{"173.245.48.0/20", "2400:cb00::/32"})
		dnsGate := NewDNSGate(DNSGateOptions{Off: true})
		rec := NewReconciler(paths, dnsGate, NewApplier(paths, caddy), NewCertProbe(paths.CaddyCertsDir, systemd), systemd, cfIPs)

		cfg := &Config{
			EdgeID:     "edge-clean",
			DNSGateOff: true,
			Panels: []PanelConfig{
				{PanelID: "panel-a", PanelName: "vps-a", RegisteredAt: now.Add(-time.Hour)},
				{PanelID: "panel-b", PanelName: "vps-b", RegisteredAt: now},
			},
		}
		stA := NewPanelState("panel-a", "vps-a")
		stA.LastGoodETag = "a1"
		stA.LastGood = &RoutesResponse{
			APIVersion: 1,
			PanelID:    "panel-a",
			ETag:       "a1",
			Origin:     OriginInfo{IPs: []string{"204.136.10.51"}, Port: 443, CAStatus: CAStatusMissing},
			Routes:     []EdgeRoute{{ID: "r1", Hosts: []string{"a.example.com"}, TrustedProxy: false}},
		}
		stB := NewPanelState("panel-b", "vps-b")
		stB.LastGoodETag = "b1"
		stB.LastGood = &RoutesResponse{
			APIVersion: 1,
			PanelID:    "panel-b",
			ETag:       "b1",
			Origin:     OriginInfo{IPs: []string{"204.136.10.52"}, Port: 443, CAStatus: CAStatusMissing},
			Routes:     []EdgeRoute{{ID: "r1", Hosts: []string{"b.example.com"}, TrustedProxy: false}},
		}
		states := map[string]*PanelState{"panel-a": stA, "panel-b": stB}
		claims := NewClaimsStore()
		meta := &ApplyMeta{}

		// 1. All routes TrustedProxy=false -> global.opts has no servers/trusted_proxies block.
		out, err := rec.ReconcileOnce(ctx, cfg, states, claims, meta, false, false, now)
		if err != nil || !out.Applied {
			t.Fatalf("reconcile 1 failed: out=%+v err=%v", out, err)
		}
		globalBytes, err := os.ReadFile(paths.GlobalOpts)
		if err != nil {
			t.Fatalf("read global.opts: %v", err)
		}
		if strings.Contains(string(globalBytes), "trusted_proxies static") || !strings.Contains(string(globalBytes), "trusted_proxies disabled") {
			t.Fatalf("expected disabled trusted_proxies on clean edge, got:\n%s", string(globalBytes))
		}
		panelsDir, _ := ReadDirLink(paths.PanelsLink)
		aCaddy, _ := os.ReadFile(filepath.Join(panelsDir, "panel-a.caddy"))
		if !strings.Contains(string(aCaddy), "{remote_host}") || !strings.Contains(string(aCaddy), "header_up -CF-Connecting-IP") {
			t.Fatalf("expected direct remote_host headers on panel-a, got:\n%s", string(aCaddy))
		}

		// 2. Enable TrustedProxy=true on panel-b -> global.opts gains CF IPv4+IPv6 + strict + headers.
		stB.LastGoodETag = "b2"
		stB.LastGood.ETag = "b2"
		stB.LastGood.Routes[0].TrustedProxy = true
		out, err = rec.ReconcileOnce(ctx, cfg, states, claims, meta, false, false, now.Add(time.Minute))
		if err != nil || !out.Applied {
			t.Fatalf("reconcile 2 failed: out=%+v err=%v", out, err)
		}
		globalBytes, _ = os.ReadFile(paths.GlobalOpts)
		globalStr := string(globalBytes)
		for _, want := range []string{
			"trusted_proxies static 173.245.48.0/20 2400:cb00::/32",
			"trusted_proxies_strict",
			"client_ip_headers CF-Connecting-IP X-Forwarded-For",
		} {
			if !strings.Contains(globalStr, want) {
				t.Fatalf("expected %q in global.opts, got:\n%s", want, globalStr)
			}
		}
		panelsDir, _ = ReadDirLink(paths.PanelsLink)
		bCaddy, _ := os.ReadFile(filepath.Join(panelsDir, "panel-b.caddy"))
		if !strings.Contains(string(bCaddy), "(nd_up_panelb_tp)") || !strings.Contains(string(bCaddy), "header_up X-Real-IP {client_ip}") {
			t.Fatalf("expected _tp snippet with client_ip on panel-b, got:\n%s", string(bCaddy))
		}

		// 3. Preserved disk panel-b (LastGood == nil) still keeps anyTrustedProxy=true.
		stB.LastGood = nil
		stB.LastGoodETag = ""
		out, err = rec.ReconcileOnce(ctx, cfg, states, claims, meta, false, false, now.Add(2*time.Minute))
		if err != nil {
			t.Fatalf("reconcile 3 failed: %v", err)
		}
		globalBytes, _ = os.ReadFile(paths.GlobalOpts)
		if !strings.Contains(string(globalBytes), "trusted_proxies static") {
			t.Fatalf("expected preserved disk _tp panel to retain global trusted_proxies, got:\n%s", string(globalBytes))
		}
	})

	t.Run("adopted_edge_merges_custom_ips_when_any_trusted_proxy", func(t *testing.T) {
		paths := NewPaths(t.TempDir())
		caddy := &fakeCaddyRunner{}
		systemd := &fakeSystemdRunner{active: true}
		cfIPs := NewCFIPStore()
		_ = cfIPs.SetRanges([]string{"173.245.48.0/20", "103.21.244.0/22", "2400:cb00::/32"})
		dnsGate := NewDNSGate(DNSGateOptions{Off: true})
		rec := NewReconciler(paths, dnsGate, NewApplier(paths, caddy), NewCertProbe(paths.CaddyCertsDir, systemd), systemd, cfIPs)

		preserved := "servers {\n\ttimeouts {\n\t\tread_body 15s\n\t}\n\ttrusted_proxies static 103.21.244.0/22 5.175.220.216\n\tmetrics\n}"
		cfg := &Config{
			EdgeID:          "edge-adopted",
			DNSGateOff:      true,
			TrustedProxies:  true,
			PreservedGlobal: preserved,
			Panels: []PanelConfig{
				{PanelID: "panel-a", PanelName: "vps-a", RegisteredAt: now},
			},
		}
		stA := NewPanelState("panel-a", "vps-a")
		stA.LastGoodETag = "a1"
		stA.LastGood = &RoutesResponse{
			APIVersion: 1,
			PanelID:    "panel-a",
			ETag:       "a1",
			Origin:     OriginInfo{IPs: []string{"204.136.10.51"}, Port: 443, CAStatus: CAStatusMissing},
			Routes:     []EdgeRoute{{ID: "r1", Hosts: []string{"a.example.com"}, TrustedProxy: false}},
		}
		states := map[string]*PanelState{"panel-a": stA}
		claims := NewClaimsStore()
		meta := &ApplyMeta{}

		// 1. TrustedProxy=false -> preserved 100% untouched.
		if _, err := rec.ReconcileOnce(ctx, cfg, states, claims, meta, false, false, now); err != nil {
			t.Fatalf("adopted reconcile 1: %v", err)
		}
		globalBytes, _ := os.ReadFile(paths.GlobalOpts)
		if strings.Contains(string(globalBytes), "2400:cb00::/32") || strings.Contains(string(globalBytes), "trusted_proxies_strict") {
			t.Fatalf("expected adopted global.opts untouched when anyTrustedProxy=false, got:\n%s", string(globalBytes))
		}
		if !strings.Contains(string(globalBytes), "5.175.220.216") {
			t.Fatalf("expected 5.175.220.216 preserved, got:\n%s", string(globalBytes))
		}

		// 2. TrustedProxy=true -> merges 5.175.220.216 + CF IPv4/IPv6 + strict + headers + keeps custom servers directives.
		stA.LastGoodETag = "a2"
		stA.LastGood.ETag = "a2"
		stA.LastGood.Routes[0].TrustedProxy = true
		if _, err := rec.ReconcileOnce(ctx, cfg, states, claims, meta, false, false, now.Add(time.Minute)); err != nil {
			t.Fatalf("adopted reconcile 2: %v", err)
		}
		globalBytes, _ = os.ReadFile(paths.GlobalOpts)
		globalStr := string(globalBytes)
		for _, want := range []string{
			"173.245.48.0/20",
			"103.21.244.0/22",
			"2400:cb00::/32",
			"5.175.220.216",
			"trusted_proxies_strict",
			"client_ip_headers CF-Connecting-IP X-Forwarded-For",
			"read_body 15s",
			"metrics",
		} {
			if !strings.Contains(globalStr, want) {
				t.Fatalf("expected %q in merged adopted global.opts, got:\n%s", want, globalStr)
			}
		}
	})
}
