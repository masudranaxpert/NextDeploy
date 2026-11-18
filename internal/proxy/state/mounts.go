package state

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"panel/internal/proxy"
)

const defaultSharedPrefix = "/mnt/shared"

var volNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

func defaultHidePatterns() []string {
	return []string{".git", ".env*", "*.sql", "*.db", "*.sqlite", "*.key", "*.sock"}
}

func appSecurityHeaders() []proxy.HeaderOp {
	return []proxy.HeaderOp{
		{Action: proxy.HeaderSet, Name: "Referrer-Policy", Value: "strict-origin-when-cross-origin"},
		{Action: proxy.HeaderSet, Name: "X-Content-Type-Options", Value: "nosniff"},
		{Action: proxy.HeaderSet, Name: "X-Frame-Options", Value: "SAMEORIGIN"},
		{Action: proxy.HeaderSet, Name: "X-XSS-Protection", Value: "1; mode=block"},
	}
}

func phpBaseHeaders() []proxy.HeaderOp {
	return []proxy.HeaderOp{
		{Action: proxy.HeaderSet, Name: "Referrer-Policy", Value: "strict-origin-when-cross-origin"},
		{Action: proxy.HeaderSet, Name: "X-Content-Type-Options", Value: "nosniff"},
	}
}

func normalizeSharedMountPrefix(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\\", "/"))
	s = strings.TrimRight(s, "/")
	if s == "" {
		return defaultSharedPrefix
	}
	if s[0] != '/' {
		s = "/" + s
	}
	return s
}

func validVolumeName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || !volNameRe.MatchString(name) {
		return false
	}
	return !strings.ContainsAny(name, ":/\\%")
}

// parseSharedVolumeNames parses JSON array ["vol1","vol2"] cleanly with legacy
// comma-separated fallback, deduplicating and validating Docker volume names.
func parseSharedVolumeNames(raw string, explicit []string) ([]string, error) {
	var candidates []string
	s := strings.TrimSpace(raw)
	if s != "" && s != "[]" && s != "null" {
		if strings.HasPrefix(s, "[") {
			if err := json.Unmarshal([]byte(s), &candidates); err != nil {
				return nil, fmt.Errorf("shared_volume_names JSON: %w", err)
			}
		} else {
			for _, part := range strings.Split(s, ",") {
				candidates = append(candidates, proxy.CleanQuotedValue(part))
			}
		}
	}
	candidates = append(candidates, explicit...)
	if len(candidates) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, n := range candidates {
		n = strings.TrimSpace(n)
		if n == "" || !validVolumeName(n) {
			continue
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

func buildSharedMounts(prefixRaw, namesRaw string, explicitNames []string) ([]proxy.SharedMount, error) {
	prefix := normalizeSharedMountPrefix(prefixRaw)
	names, err := parseSharedVolumeNames(namesRaw, explicitNames)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, nil
	}
	out := make([]proxy.SharedMount, 0, len(names))
	for _, n := range names {
		target := prefix + "/" + n
		if !proxy.ValidSharedTarget(target) {
			continue
		}
		out = append(out, proxy.SharedMount{
			VolumeName: n,
			Target:     target,
			ReadOnly:   true,
		})
	}
	return out, nil
}
