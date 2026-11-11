package caddy

import (
	"testing"

	"panel/internal/db"
)

func dummyValidHost(h string) bool {
	return h != "" && len(h) < 100
}

func TestValidateRouteForUser_SSRF(t *testing.T) {
	blockedTargets := []string{
		"http://127.0.0.1:8080",
		"http://127.0.0.2:3000",
		"http://localhost:3000",
		"http://sub.localhost:80",
		"http://169.254.169.254:80",
		"http://10.0.0.1:8080",
		"http://172.17.0.1:8080",
		"http://192.168.1.1:80",
		"http://caddy:2019",
		"http://nextdeploy:8080",
		"http://mysql:3306",
		"http://host.docker.internal:8080",
	}

	for _, target := range blockedTargets {
		r := &db.ProxyRoute{
			Kind:   "proxy",
			Domain: "userdomain.com",
			Target: target,
		}
		err := ValidateRouteForUser(r, dummyValidHost, false)
		if err == nil {
			t.Errorf("expected SSRF error for user target %q, got nil", target)
		}
	}

	// Admin should be allowed
	adminRoute := &db.ProxyRoute{
		Kind:   "proxy",
		Domain: "admindomain.com",
		Target: "http://127.0.0.1:8080",
	}
	if err := ValidateRouteForUser(adminRoute, dummyValidHost, true); err != nil {
		t.Errorf("admin should be allowed internal proxy target, got error: %v", err)
	}

	// Redirect should be allowed for user (browser redirect, not server-side proxy)
	redirectRoute := &db.ProxyRoute{
		Kind:         "redirect",
		Domain:       "redirectdomain.com",
		Target:       "https://google.com/search",
		RedirectCode: 301,
	}
	if err := ValidateRouteForUser(redirectRoute, dummyValidHost, false); err != nil {
		t.Errorf("user redirect should be allowed, got error: %v", err)
	}
}
