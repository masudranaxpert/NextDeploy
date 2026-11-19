package edge

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	ep := paths.EnginePathsFor(engineName)
	found := make(map[string]string)

	ext := ".conf"
	if ep.Binary == "caddy" || engineName == EngineCaddy || engineName == "" {
		if ep.PanelExt == ".caddy" {
			ext = ".caddy"
		}
	}

	if ep.UserDir != "" {
		entries, err := os.ReadDir(ep.UserDir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read user dir %s: %w", ep.UserDir, err)
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name() < entries[j].Name()
		})
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ext) {
				continue
			}
			full := filepath.Join(ep.UserDir, entry.Name())
			data, err := os.ReadFile(full)
			if err != nil {
				return nil, fmt.Errorf("read %s: %w", full, err)
			}
			for _, h := range parseUserConfigHostsText(string(data), engineName) {
				if _, exists := found[h]; !exists {
					found[h] = full
				}
			}
		}
	}

	if (engineName == EngineCaddy || engineName == "") && paths.Caddyfile != "" {
		if raw, err := os.ReadFile(paths.Caddyfile); err == nil {
			unmanaged := StripManagedRegion(string(raw))
			for _, h := range parseUserConfigHostsText(unmanaged, EngineCaddy) {
				if _, exists := found[h]; !exists {
					found[h] = paths.Caddyfile
				}
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read %s: %w", paths.Caddyfile, err)
		}
	}

	return found, nil
}

// parseUserConfigHostsText extracts normalized site hostnames from user proxy configs.
func parseUserConfigHostsText(content, engineName string) []string {
	var hosts []string
	seen := make(map[string]struct{})
	addToken := func(tok string) {
		h := normalizeUserConfigHostToken(tok)
		if h == "" {
			return
		}
		if _, ok := seen[h]; !ok {
			seen[h] = struct{}{}
			hosts = append(hosts, h)
		}
	}

	scanner := bufio.NewScanner(strings.NewReader(content))
	depth := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, " #"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}

		if (engineName == EngineAngie || engineName == "") &&
			(strings.HasPrefix(line, "server_name ") || strings.HasPrefix(line, "server_name\t")) {
			stmt := strings.TrimSuffix(line, ";")
			for _, field := range strings.Fields(stmt)[1:] {
				addToken(field)
			}
		} else if depth == 0 && strings.HasSuffix(line, "{") {
			header := strings.TrimSpace(strings.TrimSuffix(line, "{"))
			low := strings.ToLower(header)
			isNonHostBlock := header == "" ||
				strings.HasPrefix(header, "(") ||
				strings.HasPrefix(low, "snippet ") ||
				strings.HasPrefix(low, "match ") ||
				strings.HasPrefix(low, "server") ||
				strings.HasPrefix(low, "upstream ") ||
				strings.HasPrefix(low, "globals")
			isFerronHTTP := engineName == EngineFerron && strings.HasPrefix(low, "http ")
			if !isNonHostBlock && (!strings.HasPrefix(low, "http ") || isFerronHTTP) {
				trimmedHeader := header
				if isFerronHTTP {
					trimmedHeader = strings.TrimSpace(header[len("http "):])
				}
				for _, commaPart := range strings.Split(trimmedHeader, ",") {
					for _, tok := range strings.Fields(commaPart) {
						addToken(tok)
					}
				}
			}
		}

		depth += strings.Count(line, "{") - strings.Count(line, "}")
		if depth < 0 {
			depth = 0
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
	if err := CheckInternalManualDuplicates(manualFile, candidate, ignoreID); err != nil {
		return err
	}

	userHosts, err := ScanUserConfigHosts(paths, engineName)
	if err != nil {
		return err
	}

	hosts := candidate.AllHosts()
	for _, h := range hosts {
		if claims != nil && claims.Claims != nil {
			if cl, claimed := claims.Claims[h]; claimed && cl.PanelID != "" && cl.PanelID != ManualClaimPanelID {
				return fmt.Errorf("host %q is already claimed by panel %q (remove the panel route before adding it manually)", h, cl.PanelID)
			}
		}
		if filePath, inUser := userHosts[h]; inUser {
			return fmt.Errorf("host %q is already defined in user config %s (remove it from %s before managing it with 'nd edge domain')", h, filePath, filepath.Base(filePath))
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
