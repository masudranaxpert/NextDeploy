package state

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"panel/internal/db"
	"panel/internal/proxy"
)

func TestBuildGoldenFixtures(t *testing.T) {
	cases := allGoldenCases()
	if len(cases) < 12 {
		t.Fatalf("expected at least 12 golden fixtures, got %d", len(cases))
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			st1, err := Build(tc.input)
			if err != nil {
				t.Fatalf("Build(%s) failed: %v", tc.name, err)
			}
			if st1.Generation == "" || len(st1.Generation) != 64 {
				t.Fatalf("expected 64-char SHA-256 generation, got %q", st1.Generation)
			}

			got1, err := json.MarshalIndent(st1, "", "  ")
			if err != nil {
				t.Fatalf("marshal state: %v", err)
			}
			got1 = append(got1, '\n')

			goldenPath := filepath.Join("testdata", tc.file)
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden file %s: %v", goldenPath, err)
			}
			want = bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))

			if !bytes.Equal(got1, want) {
				t.Fatalf("golden mismatch for %s (%s):\n--- got ---\n%s\n--- want ---\n%s",
					tc.name, goldenPath, string(got1), string(want))
			}

			// Deterministic re-run check: second Build must match byte-for-byte.
			st2, err := Build(tc.input)
			if err != nil {
				t.Fatalf("second Build(%s) failed: %v", tc.name, err)
			}
			got2, err := json.MarshalIndent(st2, "", "  ")
			if err != nil {
				t.Fatalf("marshal second state: %v", err)
			}
			got2 = append(got2, '\n')
			if !bytes.Equal(got1, got2) {
				t.Fatalf("non-deterministic Build output for %s", tc.name)
			}
		})
	}
}

func TestBuildSharedVolumeJSONAndCSVFallback(t *testing.T) {
	base := BuildInput{
		PanelDomain:          "panel.example.com",
		PanelEnableHTTPS:     true,
		PanelEnableWWW:       true,
		ACMEEmail:            "admin@example.com",
		SharedMountPrefix:    "/mnt/shared/",
		SharedVolumeNamesRaw: `["media_vol","assets_vol","media_vol"]`,
		Global: proxy.GlobalOptions{
			Profile:       "balanced",
			ReadHeader:    "15s",
			Idle:          "2m",
			GracePeriod:   "30s",
			RealIP:        "cloudflare",
			TrustedStrict: true,
		},
	}

	csv := base
	csv.SharedMountPrefix = "mnt\\shared"
	csv.SharedVolumeNamesRaw = "assets_vol, media_vol, assets_vol"

	stJSON, err := Build(base)
	if err != nil {
		t.Fatalf("Build(JSON): %v", err)
	}
	stCSV, err := Build(csv)
	if err != nil {
		t.Fatalf("Build(CSV): %v", err)
	}

	if stJSON.Generation != stCSV.Generation {
		t.Fatalf("JSON vs CSV shared mount generation mismatch: %s != %s", stJSON.Generation, stCSV.Generation)
	}
	if !reflect.DeepEqual(stJSON.SharedMounts, stCSV.SharedMounts) {
		t.Fatalf("JSON vs CSV SharedMounts mismatch: %+v vs %+v", stJSON.SharedMounts, stCSV.SharedMounts)
	}
	want := []proxy.SharedMount{
		{VolumeName: "assets_vol", Target: "/mnt/shared/assets_vol", ReadOnly: true},
		{VolumeName: "media_vol", Target: "/mnt/shared/media_vol", ReadOnly: true},
	}
	if !reflect.DeepEqual(stJSON.SharedMounts, want) {
		t.Fatalf("SharedMounts = %+v, want %+v", stJSON.SharedMounts, want)
	}
}

func TestBuildDeterministicAcrossInputReorderingAndPure(t *testing.T) {
	in1 := BuildInput{
		PanelDomain:          "panel.example.com",
		PanelEnableHTTPS:     true,
		SharedVolumeNamesRaw: `["vol_b","vol_a"]`,
		ShieldNodes: []db.ShieldNode{
			{ID: 1, Name: "s1", IP: "198.51.100.1"},
			{ID: 2, Name: "s2", IP: "198.51.100.2"},
		},
		Apps: []db.App{
			{ID: "app-a", Name: "app-a", ActiveSlot: "blue"},
			{ID: "app-b", Name: "app-b", ActiveSlot: "green"},
		},
		AppDomains: []db.AppDomain{
			{ID: 1, AppID: "app-a", Domain: "a.example.com", Service: "web", Port: 3000, EnableHTTPS: true},
			{ID: 2, AppID: "app-b", Domain: "b.example.com", Service: "web", Port: 4000, EnableHTTPS: true, TrustedProxy: true},
			{ID: 3, AppID: "php-a", Domain: "php.example.com", Service: "php", Port: 80, EnableHTTPS: true},
		},
		PHPSites: []PHPSiteInput{
			{
				Site: db.PHPSite{AppID: "php-a", OwnerID: 1, SiteType: "php", PHPVersion: "8.3"},
				Security: proxy.SecuritySpec{
					BlockedIPs: []string{"5.6.7.8", "1.2.3.4"},
					AllowOnly:  []string{"203.0.113.0/24", "198.51.100.0/24"},
				},
			},
		},
		ProxyRoutes: []db.ProxyRoute{
			{ID: 1, Kind: "proxy", Domain: "r1.example.com", Target: "http://10.0.0.1:8080", EnableHTTPS: true, Enabled: true},
			{ID: 2, Kind: "redirect", Domain: "r2.example.com", Target: "https://r1.example.com", RedirectCode: 301, EnableHTTPS: true, Enabled: true},
		},
		AllocatedPorts: []db.AllocatedPort{
			{Port: 4001, BindIP: "0.0.0.0", AppID: "app-a", Service: "web", Protocol: "tcp", Direct: false},
			{Port: 4002, BindIP: "127.0.0.1", AppID: "app-b", Service: "web", Protocol: "tcp", Direct: false},
		},
	}

	in2 := BuildInput{
		PanelDomain:          "panel.example.com",
		PanelEnableHTTPS:     true,
		SharedVolumeNamesRaw: "vol_a, vol_b",
		ShieldNodes:          []db.ShieldNode{in1.ShieldNodes[1], in1.ShieldNodes[0]},
		Apps:                 []db.App{in1.Apps[1], in1.Apps[0]},
		AppDomains:           []db.AppDomain{in1.AppDomains[2], in1.AppDomains[1], in1.AppDomains[0]},
		PHPSites: []PHPSiteInput{
			{
				Site: db.PHPSite{AppID: "php-a", OwnerID: 1, SiteType: "php", PHPVersion: "8.3"},
				Security: proxy.SecuritySpec{
					BlockedIPs: []string{"1.2.3.4", "5.6.7.8"},
					AllowOnly:  []string{"198.51.100.0/24", "203.0.113.0/24"},
				},
			},
		},
		ProxyRoutes:    []db.ProxyRoute{in1.ProxyRoutes[1], in1.ProxyRoutes[0]},
		AllocatedPorts: []db.AllocatedPort{in1.AllocatedPorts[1], in1.AllocatedPorts[0]},
	}

	origDomainOrder := []int64{in1.AppDomains[0].ID, in1.AppDomains[1].ID, in1.AppDomains[2].ID}
	origBlocked := append([]string(nil), in1.PHPSites[0].Security.BlockedIPs...)

	st1, err := Build(in1)
	if err != nil {
		t.Fatalf("Build(in1): %v", err)
	}
	st2, err := Build(in2)
	if err != nil {
		t.Fatalf("Build(in2): %v", err)
	}

	b1, _ := json.MarshalIndent(st1, "", "  ")
	b2, _ := json.MarshalIndent(st2, "", "  ")
	if !bytes.Equal(b1, b2) {
		t.Fatalf("reordered inputs produced different IR JSON:\n%s\nvs\n%s", string(b1), string(b2))
	}
	if st1.Generation != st2.Generation {
		t.Fatalf("reordered inputs produced different Generation: %s != %s", st1.Generation, st2.Generation)
	}

	if !reflect.DeepEqual([]int64{in1.AppDomains[0].ID, in1.AppDomains[1].ID, in1.AppDomains[2].ID}, origDomainOrder) {
		t.Fatal("Build mutated input AppDomains order")
	}
	if !reflect.DeepEqual(in1.PHPSites[0].Security.BlockedIPs, origBlocked) {
		t.Fatal("Build mutated input BlockedIPs slice")
	}
}

func TestBuildPanelShieldAndValidationErrors(t *testing.T) {
	sid := int64(5)
	st, err := Build(BuildInput{
		PanelDomain:       "panel.example.com",
		PanelEnableHTTPS:  false,
		PanelShieldNodeID: &sid,
		Global:            proxy.GlobalOptions{RealIP: "none"},
	})
	if err != nil {
		t.Fatalf("Build shielded panel: %v", err)
	}
	if st.Panel == nil || !st.Panel.BehindShield || st.Panel.TLS != proxy.TLSInternal || st.Panel.TrustedProxy {
		t.Fatalf("unexpected shielded panel site: %+v", st.Panel)
	}

	badInputs := map[string]BuildInput{
		"bad global options": {
			Global: proxy.GlobalOptions{RealIP: "custom"},
		},
		"bad shared volume json": {
			SharedVolumeNamesRaw: `["unclosed`,
		},
		"bad proxy route kind": {
			ProxyRoutes: []db.ProxyRoute{{ID: 1, Kind: "ftp", Domain: "a.example.com", Target: "ftp://1.2.3.4", Enabled: true}},
		},
		"bad php security ip": {
			AppDomains: []db.AppDomain{{ID: 1, AppID: "p1", Domain: "p1.example.com", Service: "php", Port: 80}},
			PHPSites: []PHPSiteInput{{
				Site:     db.PHPSite{AppID: "p1", OwnerID: 1, SiteType: "php", PHPVersion: "8.3"},
				Security: proxy.SecuritySpec{BlockedIPs: []string{"not-an-ip"}},
			}},
		},
	}
	for name, bad := range badInputs {
		if _, err := Build(bad); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}
