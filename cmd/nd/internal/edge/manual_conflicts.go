package edge

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const ManualClaimPanelID = "manual"

// UserHostConflict identifies a host found in an operator-managed user config file.
type UserHostConflict struct {
	Host string
	File string
}

// ScanUserConfigHosts inspects operator user/*.conf / user/*.caddy files (and the
// unmanaged portion of Caddyfile when Caddy is active) and returns host -> file path.
func ScanUserConfigHosts(paths Paths, engineName string) (map[string]string, error) {
	sites, err := ScanUserConfigSites(paths, engineName)
	if err != nil {
		return nil, err
	}
	found := make(map[string]string)
	for _, s := range sites {
		for _, h := range s.Hosts {
			if _, exists := found[h]; !exists {
				found[h] = s.File
			}
		}
	}
	return found, nil
}

// parseUserConfigHostsText extracts normalized site hostnames from user proxy configs.
func parseUserConfigHostsText(content, engineName string) []string {
	sites := parseUserConfigSites(content, "", engineName)
	var hosts []string
	seen := make(map[string]struct{})
	for _, s := range sites {
		for _, h := range s.Hosts {
			if _, ok := seen[h]; !ok {
				seen[h] = struct{}{}
				hosts = append(hosts, h)
			}
		}
	}
	return hosts
}

func normalizeUserConfigHostToken(raw string) string {
	h := strings.ToLower(strings.TrimSpace(raw))
	h = strings.Trim(h, `"'`)
	if h == "" || h == "_" || h == "*" || strings.HasPrefix(h, "*:") || strings.HasPrefix(h, ":") {
		return ""
	}
	h = strings.TrimPrefix(h, "http://")
	h = strings.TrimPrefix(h, "https://")
	if slash := strings.IndexByte(h, '/'); slash >= 0 {
		h = h[:slash]
	}
	if colon := strings.LastIndexByte(h, ':'); colon >= 0 && !strings.Contains(h, "]") {
		portPart := h[colon+1:]
		allDigits := portPart != ""
		for i := 0; i < len(portPart); i++ {
			if portPart[i] < '0' || portPart[i] > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			h = h[:colon]
		}
	}
	h = strings.TrimSuffix(h, ".")
	if h == "" || h == "localhost" || h == "_" || h == "*" {
		return ""
	}
	return h
}

// CheckManualHostConflicts checks candidate hosts against existing manual domains,
// panel claims in claims.json, and hand-written user configs.
func CheckManualHostConflicts(
	candidate ManualDomainEntry,
	manualFile *ManualDomainsFile,
	claims *ClaimsStore,
	paths Paths,
	engineName string,
	ignoreID string,
) error {
	return checkManualHostConflicts(candidate, manualFile, claims, paths, engineName, ignoreID, false)
}

// CheckManualHostConflictsAllowUserReplace checks candidate hosts against existing
// manual domains and panel claims, allowing replacement of hand-written user config entries.
func CheckManualHostConflictsAllowUserReplace(
	candidate ManualDomainEntry,
	manualFile *ManualDomainsFile,
	claims *ClaimsStore,
	paths Paths,
	engineName string,
	ignoreID string,
) error {
	return checkManualHostConflicts(candidate, manualFile, claims, paths, engineName, ignoreID, true)
}

func checkManualHostConflicts(
	candidate ManualDomainEntry,
	manualFile *ManualDomainsFile,
	claims *ClaimsStore,
	paths Paths,
	engineName string,
	ignoreID string,
	allowUserReplace bool,
) error {
	if err := CheckInternalManualDuplicates(manualFile, candidate, ignoreID); err != nil {
		return err
	}

	var userHosts map[string]string
	if !allowUserReplace {
		var err error
		userHosts, err = ScanUserConfigHosts(paths, engineName)
		if err != nil {
			return err
		}
	}

	hosts := candidate.AllHosts()
	for _, h := range hosts {
		if claims != nil && claims.Claims != nil {
			if cl, claimed := claims.Claims[h]; claimed && cl.PanelID != "" && cl.PanelID != ManualClaimPanelID {
				return fmt.Errorf("host %q is already claimed by panel %q (remove the panel route before adding it manually)", h, cl.PanelID)
			}
		}
		if userHosts != nil {
			if filePath, inUser := userHosts[h]; inUser {
				return fmt.Errorf("host %q is already defined in user config %s (remove it with 'nd edge domain rm %s', edit %s, or re-run with '--replace')", h, filePath, h, filepath.Base(filePath))
			}
		}
	}
	return nil
}

// ManualClaimHostSet returns the normalized set of all hosts claimed by manual entries.
func ManualClaimHostSet(f *ManualDomainsFile) map[string]struct{} {
	out := make(map[string]struct{})
	if f == nil {
		return out
	}
	for _, e := range f.Domains {
		for _, h := range e.AllHosts() {
			out[h] = struct{}{}
		}
	}
	return out
}

// ManualClaimant builds the Claimant representing manual domains for ResolveClaims.
func ManualClaimant(f *ManualDomainsFile) *Claimant {
	hosts := ManualClaimHostSet(f)
	if len(hosts) == 0 {
		return nil
	}
	return &Claimant{
		PanelID:      ManualClaimPanelID,
		PanelName:    "manual",
		RegisteredAt: f.earliestCreatedAt(),
		DesiredHosts: hosts,
	}
}

func (f *ManualDomainsFile) earliestCreatedAt() time.Time {
	if f == nil || len(f.Domains) == 0 {
		return time.Unix(1, 0).UTC()
	}
	earliest := time.Time{}
	for _, d := range f.Domains {
		if d.CreatedAt.IsZero() {
			continue
		}
		if earliest.IsZero() || d.CreatedAt.Before(earliest) {
			earliest = d.CreatedAt
		}
	}
	if earliest.IsZero() {
		return time.Unix(1, 0).UTC()
	}
	return earliest.UTC()
}
