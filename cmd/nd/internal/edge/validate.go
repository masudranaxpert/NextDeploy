package edge

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strings"

	"golang.org/x/net/idna"
)

const MaxCAPEMBytes = 64 * 1024

var (
	routeIDRe = regexp.MustCompile(`^[a-z0-9_-]{1,64}$`)
	panelIDRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	// Matches §3.3 after length check (<=253) and IDN->punycode conversion.
	strictHostRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

var internalSuffixes = []string{
	".localhost",
	".local",
	".internal",
	".test",
	".example",
	".invalid",
	".lan",
	".home.arpa",
}

// HostValidationError describes why a host was rejected and which §3.6 state it maps to.
type HostValidationError struct {
	State  string
	Detail string
}

func (e *HostValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.State, e.Detail)
}

// ValidatePanelID ensures panel_id is safe for filenames and Caddy snippet names.
func ValidatePanelID(id string) error {
	if !panelIDRe.MatchString(id) {
		return fmt.Errorf("invalid panel_id %q: must match [a-zA-Z0-9_-]{1,64}", id)
	}
	return nil
}

// ValidateRouteID ensures route id matches [a-z0-9_-]{1,64} (§3.3).
func ValidateRouteID(id string) error {
	if !routeIDRe.MatchString(id) {
		return fmt.Errorf("invalid route id %q: must match [a-z0-9_-]{1,64}", id)
	}
	return nil
}

// NormalizeAndValidateHost normalizes a raw hostname (lowercase, strip trailing
// dot, IDN -> punycode) and enforces §3.3 security rules to prevent Caddyfile
// injection and invalid/loop/wildcard hosts.
func NormalizeAndValidateHost(raw, shieldHostname string) (string, *HostValidationError) {
	if raw == "" {
		return "", &HostValidationError{State: StateExcludedInternal, Detail: "empty hostname"}
	}

	// Reject Caddyfile metacharacters, quotes, semicolons, and whitespace before any transformation.
	for i := 0; i < len(raw); i++ {
		b := raw[i]
		if b <= 0x20 || b == 0x7f || b == '{' || b == '}' || b == '"' || b == '\'' || b == ';' || b == '\\' || b == '/' || b == ':' {
			return "", &HostValidationError{State: StateExcludedInternal, Detail: "hostname contains forbidden characters"}
		}
	}

	trimmed := strings.TrimSuffix(strings.ToLower(raw), ".")
	if trimmed == "" {
		return "", &HostValidationError{State: StateExcludedInternal, Detail: "empty hostname"}
	}

	// Wildcards are unsupported in v1 (need DNS-01).
	if strings.HasPrefix(trimmed, "*.") || strings.Contains(trimmed, "*") {
		return "", &HostValidationError{State: StateUnsupportedWildcard, Detail: "wildcard domains are unsupported in v1"}
	}

	// Convert IDN (Unicode) -> punycode ASCII.
	asciiHost, err := idna.Lookup.ToASCII(trimmed)
	if err != nil {
		return "", &HostValidationError{State: StateExcludedInternal, Detail: fmt.Sprintf("invalid IDN hostname: %v", err)}
	}
	asciiHost = strings.TrimSuffix(strings.ToLower(asciiHost), ".")

	// Post-IDN defense-in-depth: strictly ASCII [a-z0-9.-] only, length 1..253.
	if len(asciiHost) == 0 || len(asciiHost) > 253 {
		return "", &HostValidationError{State: StateExcludedInternal, Detail: "hostname length out of range (1..253)"}
	}
	for i := 0; i < len(asciiHost); i++ {
		c := asciiHost[i]
		isValid := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' || c == '-'
		if !isValid {
			return "", &HostValidationError{State: StateExcludedInternal, Detail: "hostname contains non-LDH ASCII characters"}
		}
	}

	// Reject IP literals (IPv4 or IPv6).
	if net.ParseIP(asciiHost) != nil || strings.HasPrefix(asciiHost, "[") {
		return "", &HostValidationError{State: StateExcludedInternal, Detail: "IP literals are not allowed as hostnames"}
	}

	// Reject localhost and reserved/internal suffixes before multi-label regex so
	// single-label "localhost" gets a clear reason too.
	if asciiHost == "localhost" {
		return "", &HostValidationError{State: StateExcludedInternal, Detail: "localhost is excluded"}
	}
	for _, sfx := range internalSuffixes {
		if strings.HasSuffix(asciiHost, sfx) {
			return "", &HostValidationError{State: StateExcludedInternal, Detail: fmt.Sprintf("internal suffix %s is excluded", sfx)}
		}
	}

	if !strictHostRe.MatchString(asciiHost) {
		return "", &HostValidationError{State: StateExcludedInternal, Detail: "invalid DNS hostname syntax"}
	}

	// Reject Shield's own management hostname (loop prevention, E-DNS-13).
	if shieldHostname != "" {
		sh := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(shieldHostname)), ".")
		if shASCII, err := idna.Lookup.ToASCII(sh); err == nil {
			sh = strings.TrimSuffix(strings.ToLower(shASCII), ".")
		}
		if asciiHost == sh {
			return "", &HostValidationError{State: StateExcludedLoop, Detail: "hostname matches Shield management hostname"}
		}
	}

	return asciiHost, nil
}

// ValidateOriginIP validates a panel origin IP address and rejects loopback,
// link-local, unspecified, multicast, and any of the Shield's own IPs (§3.3).
func ValidateOriginIP(raw string, shieldIPs []netip.Addr) (netip.Addr, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return netip.Addr{}, errors.New("empty origin IP")
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("invalid origin IP %q: %w", raw, err)
	}
	if addr.Zone() != "" {
		return netip.Addr{}, fmt.Errorf("origin IP %q must not contain a zone", raw)
	}
	addr = addr.Unmap()

	if addr.IsLoopback() {
		return netip.Addr{}, fmt.Errorf("%s: origin IP %s is loopback", StateExcludedLoop, addr)
	}
	if addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() {
		return netip.Addr{}, fmt.Errorf("%s: origin IP %s is link-local", StateExcludedLoop, addr)
	}
	if addr.IsUnspecified() {
		return netip.Addr{}, fmt.Errorf("%s: origin IP %s is unspecified", StateExcludedLoop, addr)
	}
	if addr.IsMulticast() {
		return netip.Addr{}, fmt.Errorf("%s: origin IP %s is multicast", StateExcludedLoop, addr)
	}
	for _, sip := range shieldIPs {
		if sip.Unmap() == addr {
			return netip.Addr{}, fmt.Errorf("%s: origin IP %s matches Shield IP", StateExcludedLoop, addr)
		}
	}
	return addr, nil
}

// ValidateCAPEM verifies that pemData is <= 64 KB and contains at least one
// valid X.509 CA certificate (§3.3).
func ValidateCAPEM(pemData string) error {
	if len(pemData) == 0 {
		return errors.New("empty CA PEM")
	}
	if len(pemData) > MaxCAPEMBytes {
		return fmt.Errorf("CA PEM exceeds %d byte cap (%d bytes)", MaxCAPEMBytes, len(pemData))
	}

	rest := []byte(pemData)
	certCount := 0
	for len(rest) > 0 {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			return fmt.Errorf("unexpected PEM block type %q", block.Type)
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return fmt.Errorf("parse x509 certificate: %w", err)
		}
		if !cert.IsCA {
			return fmt.Errorf("certificate %q is not a CA (IsCA=false)", cert.Subject.CommonName)
		}
		certCount++
	}
	if certCount == 0 {
		return errors.New("no CERTIFICATE PEM blocks found")
	}
	return nil
}

// CAPEMSHA256 returns the lowercase hex SHA-256 of raw PEM bytes.
func CAPEMSHA256(pemData string) string {
	sum := sha256.Sum256([]byte(pemData))
	return hex.EncodeToString(sum[:])
}

// ParseIPStrings parses a slice of IP strings into netip.Addr, skipping empty/invalid ones.
func ParseIPStrings(ips []string) []netip.Addr {
	out := make([]netip.Addr, 0, len(ips))
	for _, raw := range ips {
		if a, err := netip.ParseAddr(strings.TrimSpace(raw)); err == nil && a.Zone() == "" {
			out = append(out, a.Unmap())
		}
	}
	return out
}
