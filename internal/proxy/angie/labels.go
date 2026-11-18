package angie

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"panel/internal/proxy"
)

// AppLabels emits Angie Docker upstream discovery labels for an app container
// matching its upstream name, port, and case-sensitive Docker network.
// When up.Name is empty or up.Port <= 0, it returns nil.
func (d *Driver) AppLabels(_ []proxy.Site, up proxy.UpstreamRef) map[string]string {
	name := SanitizeIdent(up.Name)
	if name == "" || up.Port <= 0 {
		return nil
	}
	netName := DefaultNetworkName
	if d != nil && strings.TrimSpace(d.networkName) != "" {
		netName = strings.TrimSpace(d.networkName)
	}
	return map[string]string{
		"angie.http.upstreams." + name + ".port": strconv.Itoa(up.Port),
		"angie.network":                          netName,
	}
}

// SanitizeIdent converts an identifier into an Angie-safe [A-Za-z0-9_]+ token.
// Returns "" if s contains no whitespace-free content.
func SanitizeIdent(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(trimmed))
	for _, r := range trimmed {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "nd"
	}
	if out[0] >= '0' && out[0] <= '9' {
		out = "nd_" + out
	}
	return out
}

// ACMEClientName derives a deterministic, collision-free, lowercase [a-z0-9_]+
// identifier for an Angie http_acme client from siteID.
func ACMEClientName(siteID string) string {
	trimmed := strings.ToLower(strings.TrimSpace(siteID))
	var b strings.Builder
	for _, r := range trimmed {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	stem := strings.Trim(b.String(), "_")
	if stem == "" {
		stem = "site"
	}
	if len(stem) > 40 {
		stem = strings.TrimRight(stem[:40], "_")
		if stem == "" {
			stem = "site"
		}
	}
	sum := sha256.Sum256([]byte(siteID))
	return "nd_" + stem + "_" + hex.EncodeToString(sum[:4])
}

// SafeFileStem converts an arbitrary site or upstream ID into a safe filename
// stem without path separators or traversal sequences.
func SafeFileStem(id string) string {
	trimmed := strings.TrimSpace(id)
	var b strings.Builder
	modified := false
	for _, r := range trimmed {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
			modified = true
		}
	}
	stem := strings.Trim(b.String(), "_")
	if stem == "" {
		stem = "item"
		modified = true
	}
	if len(stem) > 64 {
		stem = strings.TrimRight(stem[:64], "_")
		modified = true
	}
	if modified {
		sum := sha256.Sum256([]byte(id))
		return stem + "_" + hex.EncodeToString(sum[:4])
	}
	return stem
}
