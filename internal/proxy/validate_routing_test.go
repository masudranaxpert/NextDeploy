package proxy

import (
	"fmt"
	"strings"
	"testing"

	"panel/internal/db"
)

func TestCleanQuotedValueAndSharedTarget(t *testing.T) {
	for in, want := range map[string]string{
		`  "app.example.com"  `: "app.example.com",
		`'web'`:                 "web",
		"`quoted`":              "quoted",
		`"'nested'"`:            "nested",
		`plain`:                 "plain",
		`   `:                   "",
	} {
		if got := CleanQuotedValue(in); got != want {
			t.Errorf("CleanQuotedValue(%q) = %q, want %q", in, got, want)
		}
	}

	for path, want := range map[string]bool{
		"/mnt/shared/uploads": true,
		"/srv/assets":         true,
		"":                    false,
		"relative/path":       false,
		"/mnt/../etc":         false,
	} {
		if got := ValidSharedTarget(path); got != want {
			t.Errorf("ValidSharedTarget(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestNormalizeIPList(t *testing.T) {
	got, err := NormalizeIPList([]string{" 1.2.3.4 ", "", "1.2.3.4", "10.1.2.3/8", "::ffff:5.6.7.8", "2001:db8::1"})
	if err != nil {
		t.Fatalf("NormalizeIPList: %v", err)
	}
	want := "1.2.3.4,10.0.0.0/8,5.6.7.8,2001:db8::1"
	if strings.Join(got, ",") != want {
		t.Fatalf("got %v, want %s", got, want)
	}

	for _, bad := range []string{"not-an-ip", "1.2.3.4/99", "1.2.3", "1.2.3.4 extra"} {
		if _, err := NormalizeIPList([]string{bad}); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}

	tooMany := make([]string, 0, maxSecurityEntries+1)
	for i := 0; i < maxSecurityEntries+1; i++ {
		tooMany = append(tooMany, fmt.Sprintf("10.%d.%d.%d", (i>>16)&0xff, (i>>8)&0xff, i&0xff))
	}
	if _, err := NormalizeIPList(tooMany); err == nil {
		t.Fatal("expected error when exceeding maxSecurityEntries")
	}
}

func TestValidateRouteAndSSRF(t *testing.T) {
	validHost := func(h string) bool { return strings.Contains(h, ".") && !strings.ContainsAny(h, " /\n") }

	if err := ValidateRoute(nil, validHost); err == nil {
		t.Fatal("expected error for nil route")
	}

	blocked := []string{
		"http://127.0.0.1:8080",
		"http://localhost:3000",
		"http://sub.localhost:80",
		"http://169.254.169.254:80",
		"http://10.0.0.1:8080",
		"http://172.17.0.1:8080",
		"http://192.168.1.1:80",
		"http://100.64.0.1:8080",
		"http://[::1]:8080",
		"http://[fc00::1]:8080",
		"http://[fe80::1]:8080",
		"http://caddy:2019",
		"http://nd-proxy:8080",
		"http://panel:8080",
		"http://host.docker.internal:8080",
	}
	for _, target := range blocked {
		r := &db.ProxyRoute{Kind: "proxy", Domain: "app.example.com", Target: target}
		if err := ValidateRouteForUser(r, validHost, false); err == nil {
			t.Errorf("expected SSRF rejection for target %q", target)
		}
	}

	// Admin permits internal proxy targets and cleans target/skip_tls_verify.
	adminRoute := &db.ProxyRoute{
		Kind:          "proxy",
		Domain:        " App.Example.com ",
		Target:        "http://10.0.0.5:8080/",
		SkipTLSVerify: true,
		RedirectCode:  302,
		KeepPath:      true,
	}
	if err := ValidateRoute(adminRoute, validHost); err != nil {
		t.Fatalf("admin ValidateRoute failed: %v", err)
	}
	if adminRoute.Domain != "app.example.com" || adminRoute.Target != "http://10.0.0.5:8080" || adminRoute.SkipTLSVerify || adminRoute.RedirectCode != 0 || adminRoute.KeepPath {
		t.Fatalf("unexpected cleaned proxy route: %+v", adminRoute)
	}

	// Redirect normalization.
	redir := &db.ProxyRoute{
		Kind:         "redirect",
		Domain:       "old.example.com",
		Target:       "https://new.example.com/",
		RedirectCode: 999,
		KeepPath:     true,
	}
	if err := ValidateRouteForUser(redir, validHost, false); err != nil {
		t.Fatalf("user redirect failed: %v", err)
	}
	if redir.Target != "https://new.example.com" || redir.RedirectCode != 301 {
		t.Fatalf("unexpected normalized redirect: %+v", redir)
	}
}

func TestValidateAndMergeRouting(t *testing.T) {
	r := SiteRouting{
		FallbackMode:  FallbackCustom,
		FallbackEntry: "/app.php",
		Rules: []RoutingRule{
			{Kind: "prefix", MatchType: "prefix", Pattern: "docs"}, // invalid kind -> checked below
		},
	}
	if err := ValidateRouting(&r, false); err == nil {
		t.Fatal("expected invalid kind error")
	}

	valid := SiteRouting{
		FallbackMode:  FallbackCustom,
		FallbackEntry: "/app.php",
		Rules: []RoutingRule{
			{Kind: "rewrite", MatchType: "prefix", Pattern: "blog/", Target: "/blog.php", Enabled: true},
			{Kind: "redir", MatchType: "exact", Pattern: "old", Target: "/new", Enabled: true},
			{Kind: "block", MatchType: "regex", Pattern: `^/admin`, Enabled: true},
		},
	}
	if err := ValidateRouting(&valid, false); err != nil {
		t.Fatalf("ValidateRouting valid failed: %v", err)
	}
	if valid.FallbackEntry != "app.php" {
		t.Fatalf("FallbackEntry = %q, want app.php", valid.FallbackEntry)
	}
	if valid.Rules[0].Pattern != "/blog/*" || valid.Rules[1].Status != 301 || valid.Rules[2].Status != 404 {
		t.Fatalf("unexpected normalized rules: %+v", valid.Rules)
	}

	for idx, bad := range []SiteRouting{
		{FallbackMode: "unknown"},
		{FallbackMode: FallbackCustom, FallbackEntry: "../secret.php"},
		{FallbackMode: FallbackCustom, FallbackEntry: ".env"},
		{Rules: []RoutingRule{{Kind: "rewrite", MatchType: "exact", Pattern: "/a", Target: "/.git/config", Enabled: true}}},
		{Rules: []RoutingRule{{Kind: "rewrite", MatchType: "regex", Pattern: `(unclosed`, Target: "/a.php", Enabled: true}}},
		{Rules: []RoutingRule{{Kind: "redir", MatchType: "exact", Pattern: "/a", Target: "javascript:alert(1)", Enabled: true}}},
		{Rules: []RoutingRule{{Kind: "block", MatchType: "exact", Pattern: "/a", Status: 200, Enabled: true}}},
	} {
		b := bad
		if err := ValidateRouting(&b, false); err == nil {
			t.Errorf("bad[%d] expected error, got nil", idx)
		}
	}

	merged := MergeUniqueRoutingRules(
		[]RoutingRule{{ID: "r1", Kind: "rewrite", MatchType: "exact", Pattern: "/a", Target: "/b.php"}},
		[]RoutingRule{
			{Kind: "rewrite", MatchType: "exact", Pattern: "/a", Target: "/b.php"},
			{Kind: "redir", MatchType: "exact", Pattern: "/old", Target: "/new", Status: 301},
		},
	)
	if len(merged) != 2 || merged[1].ID != "ht-2" {
		t.Fatalf("unexpected merged rules: %+v", merged)
	}
}
