package edge

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestNormalizeAndValidateHost(t *testing.T) {
	tests := []struct {
		name           string
		raw            string
		shieldHostname string
		want           string
		wantState      string
	}{
		{name: "simple", raw: "app.example.com", want: "app.example.com"},
		{name: "uppercase_trailing_dot", raw: "App.Example.COM.", want: "app.example.com"},
		{name: "idn_unicode", raw: "münchen.de", want: "xn--mnchen-3ya.de"},
		{name: "idn_cyrillic", raw: "пример.рф", want: "xn--e1afmkfd.xn--p1ai"},
		{name: "wildcard", raw: "*.example.com", wantState: StateUnsupportedWildcard},
		{name: "mid_wildcard", raw: "a.*.example.com", wantState: StateUnsupportedWildcard},
		{name: "ipv4_literal", raw: "1.2.3.4", wantState: StateExcludedInternal},
		{name: "ipv6_literal", raw: "2001:db8::1", wantState: StateExcludedInternal},
		{name: "localhost", raw: "localhost", wantState: StateExcludedInternal},
		{name: "sub_localhost", raw: "app.localhost", wantState: StateExcludedInternal},
		{name: "local_suffix", raw: "mybox.local", wantState: StateExcludedInternal},
		{name: "internal_suffix", raw: "db.internal", wantState: StateExcludedInternal},
		{name: "home_arpa", raw: "router.home.arpa", wantState: StateExcludedInternal},
		{name: "single_label", raw: "intranet", wantState: StateExcludedInternal},
		{name: "leading_dash_label", raw: "-bad.example.com", wantState: StateExcludedInternal},
		{name: "trailing_dash_label", raw: "bad-.example.com", wantState: StateExcludedInternal},
		{name: "leading_dash_tld", raw: "example.-com", wantState: StateExcludedInternal},
		{name: "caddy_brace_injection", raw: "a.com {\n respond 200", wantState: StateExcludedInternal},
		{name: "caddy_quote_injection", raw: `a.com"`, wantState: StateExcludedInternal},
		{name: "caddy_semicolon", raw: "a.com;b.com", wantState: StateExcludedInternal},
		{name: "space_in_host", raw: "a.com b.com", wantState: StateExcludedInternal},
		{name: "shield_hostname_loop", raw: "Shield.Example.Net.", shieldHostname: "shield.example.net", wantState: StateExcludedLoop},
		{name: "over_253_chars", raw: strings.Repeat("a.", 127) + "com", wantState: StateExcludedInternal},
		{name: "label_64_chars", raw: strings.Repeat("a", 64) + ".com", wantState: StateExcludedInternal},
		{name: "label_63_chars", raw: strings.Repeat("a", 63) + ".com", want: strings.Repeat("a", 63) + ".com"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, verr := NormalizeAndValidateHost(tc.raw, tc.shieldHostname)
			if tc.wantState != "" {
				if verr == nil {
					t.Fatalf("expected error state %s, got valid %q", tc.wantState, got)
				}
				if verr.State != tc.wantState {
					t.Fatalf("expected state %s, got %s (%s)", tc.wantState, verr.State, verr.Detail)
				}
				return
			}
			if verr != nil {
				t.Fatalf("unexpected validation error: %v", verr)
			}
			if got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func FuzzValidateHost(f *testing.F) {
	seeds := []string{
		"app.example.com",
		"APP.EXAMPLE.COM.",
		"münchen.de",
		"*.example.com",
		"a.com {\n respond \"pwned\"\n}",
		"a.com; rm -rf /",
		"127.0.0.1",
		"localhost",
		"test.internal",
		"",
		strings.Repeat("a", 300) + ".com",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		out, verr := NormalizeAndValidateHost(raw, "shield.internal.net")
		if verr != nil {
			if out != "" {
				t.Fatalf("expected empty output on error, got %q", out)
			}
			return
		}
		if len(out) == 0 || len(out) > 253 {
			t.Fatalf("invalid output length %d for %q", len(out), out)
		}
		if strings.ContainsAny(out, "{}\n\r\"'; \t\v\f") {
			t.Fatalf("unsafe character in validated host %q", out)
		}
		for i := 0; i < len(out); i++ {
			c := out[i]
			if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' || c == '-') {
				t.Fatalf("unexpected byte %q in validated host %q", c, out)
			}
		}
	})
}

func TestValidateOriginIP(t *testing.T) {
	shieldIPs := []netip.Addr{netip.MustParseAddr("107.189.6.222"), netip.MustParseAddr("2001:db8::10")}

	valid := []string{"204.136.10.51", "2606:4700:4700::1111"}
	for _, ip := range valid {
		if _, err := ValidateOriginIP(ip, shieldIPs); err != nil {
			t.Fatalf("expected valid origin IP %s, got %v", ip, err)
		}
	}

	invalid := []string{
		"",
		"not-an-ip",
		"127.0.0.1",
		"::1",
		"169.254.1.1",
		"fe80::1",
		"fe80::1%eth0",
		"0.0.0.0",
		"::",
		"224.0.0.1",
		"ff02::1",
		"107.189.6.222",
		"2001:db8::10",
	}
	for _, ip := range invalid {
		if _, err := ValidateOriginIP(ip, shieldIPs); err == nil {
			t.Fatalf("expected error for origin IP %q", ip)
		}
	}
}

func TestValidateRouteIDAndPanelID(t *testing.T) {
	if err := ValidateRouteID("dom_12-abc"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, bad := range []string{"", "DOM_1", "dom 1", "dom{1}", "../etc", strings.Repeat("a", 65)} {
		if err := ValidateRouteID(bad); err == nil {
			t.Fatalf("expected route id %q to fail", bad)
		}
	}

	if err := ValidatePanelID("aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"); err != nil {
		t.Fatalf("unexpected panel id error: %v", err)
	}
	for _, bad := range []string{"", "../bad", "bad/panel", "bad panel"} {
		if err := ValidatePanelID(bad); err == nil {
			t.Fatalf("expected panel id %q to fail", bad)
		}
	}
}

func generateTestCertPEM(t *testing.T, isCA bool) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-origin-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  isCA,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatalf("pem encode: %v", err)
	}
	return buf.String()
}

func TestValidateCAPEM(t *testing.T) {
	caPEM := generateTestCertPEM(t, true)
	if err := ValidateCAPEM(caPEM); err != nil {
		t.Fatalf("expected valid CA PEM, got %v", err)
	}

	leafPEM := generateTestCertPEM(t, false)
	if err := ValidateCAPEM(leafPEM); err == nil {
		t.Fatalf("expected non-CA certificate to be rejected")
	}

	if err := ValidateCAPEM("not a pem"); err == nil {
		t.Fatalf("expected garbage PEM to fail")
	}
	if err := ValidateCAPEM(strings.Repeat("A", MaxCAPEMBytes+1)); err == nil {
		t.Fatalf("expected oversized PEM to fail")
	}
}
