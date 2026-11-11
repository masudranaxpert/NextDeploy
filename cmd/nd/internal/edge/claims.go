package edge

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Claim records which panel owns a host in /var/lib/nd-edge/claims.json.
type Claim struct {
	PanelID   string    `json:"panel_id"`
	ClaimedAt time.Time `json:"claimed_at"`
}

// ClaimsStore maps normalized host -> Claim.
type ClaimsStore struct {
	Claims map[string]Claim `json:"claims"`
}

// NewClaimsStore returns an empty ClaimsStore.
func NewClaimsStore() *ClaimsStore {
	return &ClaimsStore{Claims: make(map[string]Claim)}
}

// LoadClaims loads /var/lib/nd-edge/claims.json.
// Per E-STATE-2, if claims.json is corrupt, it is quarantined and rebuilt from
// the currently rendered <panel_id>.caddy files in panelsDir.
func LoadClaims(path, panelsDir string, now time.Time) (*ClaimsStore, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewClaimsStore(), nil
		}
		return nil, fmt.Errorf("read claims %s: %w", path, err)
	}

	var cs ClaimsStore
	if err := json.Unmarshal(raw, &cs); err != nil || cs.Claims == nil {
		var parseErr error = err
		if parseErr == nil {
			parseErr = errors.New("null claims map")
		}
		q, qErr := quarantineCorruptFile(path, now)
		rebuilt, rbErr := RebuildClaimsFromCaddyDir(panelsDir, now)
		if rbErr != nil {
			rebuilt = NewClaimsStore()
		}
		// Persist rebuilt claims best-effort so subsequent runs start clean.
		_ = SaveClaims(path, rebuilt)
		if qErr != nil {
			return rebuilt, &CorruptFileError{Path: path, Err: fmt.Errorf("parse: %v (quarantine failed: %v)", parseErr, qErr)}
		}
		return rebuilt, &CorruptFileError{Path: path, Quarantine: q, Err: parseErr}
	}

	return &cs, nil
}

// SaveClaims writes claims atomically to disk.
func SaveClaims(path string, cs *ClaimsStore) error {
	if cs == nil {
		return errors.New("nil claims store")
	}
	if cs.Claims == nil {
		cs.Claims = make(map[string]Claim)
	}
	return WriteJSONAtomic(path, cs, 0o600)
}

// RebuildClaimsFromCaddyDir reconstructs host -> panel_id claims by reading the
// currently active <panel_id>.caddy files (E-STATE-2).
func RebuildClaimsFromCaddyDir(panelsDir string, fallbackTime time.Time) (*ClaimsStore, error) {
	cs := NewClaimsStore()
	if panelsDir == "" {
		return cs, nil
	}
	entries, err := os.ReadDir(panelsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cs, nil
		}
		return cs, err
	}

	// Deterministic order for reproducible rebuilds.
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == PlaceholderFileName || !strings.HasSuffix(name, ".caddy") {
			continue
		}
		panelID := strings.TrimSuffix(name, ".caddy")
		if panelID == "" {
			continue
		}
		fullPath := filepath.Join(panelsDir, name)
		info, err := entry.Info()
		claimedAt := fallbackTime
		if err == nil && !info.ModTime().IsZero() {
			claimedAt = info.ModTime().UTC()
		}
		hosts, err := parseHostsFromPanelCaddy(fullPath)
		if err != nil {
			continue
		}
		for _, h := range hosts {
			if _, exists := cs.Claims[h]; !exists {
				cs.Claims[h] = Claim{
					PanelID:   panelID,
					ClaimedAt: claimedAt,
				}
			}
		}
	}
	return cs, nil
}

func parseHostsFromPanelCaddy(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var hosts []string
	seen := make(map[string]struct{})
	scanner := bufio.NewScanner(f)
	depth := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if depth == 0 && strings.HasSuffix(line, "{") {
			header := strings.TrimSpace(strings.TrimSuffix(line, "{"))
			// Skip snippet definitions like "(nd_up_12345678)".
			if !strings.HasPrefix(header, "(") && header != "" {
				for _, part := range strings.Split(header, ",") {
					h := strings.ToLower(strings.TrimSpace(part))
					if h != "" {
						if _, ok := seen[h]; !ok {
							seen[h] = struct{}{}
							hosts = append(hosts, h)
						}
					}
				}
			}
		}
		depth += strings.Count(line, "{") - strings.Count(line, "}")
		if depth < 0 {
			depth = 0
		}
	}
	return hosts, scanner.Err()
}

// Claimant describes one panel requesting ownership of one or more hosts.
type Claimant struct {
	PanelID      string
	PanelName    string
	RegisteredAt time.Time
	DesiredHosts map[string]struct{}
}

// ResolveClaims applies §3.4 ownership rules:
//   - A claim stays with its current owner as long as the owner still exists and
//     still desires the host (post mass-deletion guard).
//   - If the owner disappeared or dropped the host, the claim is released and
//     may immediately be claimed by another panel.
//   - Ties among multiple new claimants in the same reconcile break by earlier
//     RegisteredAt, then lexical PanelID.
//
// Returns per-panel owned hosts and per-panel conflicts.
func (cs *ClaimsStore) ResolveClaims(claimants []Claimant, now time.Time) (owned map[string]map[string]struct{}, conflicts map[string][]ConflictStatus) {
	if cs.Claims == nil {
		cs.Claims = make(map[string]Claim)
	}
	owned = make(map[string]map[string]struct{}, len(claimants))
	conflicts = make(map[string][]ConflictStatus, len(claimants))

	byPanel := make(map[string]Claimant, len(claimants))
	for _, c := range claimants {
		byPanel[c.PanelID] = c
		owned[c.PanelID] = make(map[string]struct{})
	}

	// Collect all requested hosts deterministically.
	hostSet := make(map[string]struct{})
	for _, c := range claimants {
		for h := range c.DesiredHosts {
			hostSet[h] = struct{}{}
		}
	}

	// Release claims whose owner panel was removed or no longer desires the host.
	for host, cl := range cs.Claims {
		owner, ok := byPanel[cl.PanelID]
		if !ok {
			delete(cs.Claims, host)
			continue
		}
		if _, wants := owner.DesiredHosts[host]; !wants {
			delete(cs.Claims, host)
		}
	}

	// Sort claimants by tie-break priority: earlier RegisteredAt, then lexical PanelID.
	ordered := make([]Claimant, len(claimants))
	copy(ordered, claimants)
	sort.Slice(ordered, func(i, j int) bool {
		if !ordered[i].RegisteredAt.Equal(ordered[j].RegisteredAt) {
			if ordered[i].RegisteredAt.IsZero() {
				return false
			}
			if ordered[j].RegisteredAt.IsZero() {
				return true
			}
			return ordered[i].RegisteredAt.Before(ordered[j].RegisteredAt)
		}
		return ordered[i].PanelID < ordered[j].PanelID
	})

	allHosts := make([]string, 0, len(hostSet))
	for h := range hostSet {
		allHosts = append(allHosts, h)
	}
	sort.Strings(allHosts)

	for _, host := range allHosts {
		cl, claimed := cs.Claims[host]
		if !claimed {
			// Assign to highest-priority claimant.
			for _, c := range ordered {
				if _, wants := c.DesiredHosts[host]; wants {
					cl = Claim{PanelID: c.PanelID, ClaimedAt: now.UTC()}
					cs.Claims[host] = cl
					break
				}
			}
		}

		ownerName := cl.PanelID
		if owner, ok := byPanel[cl.PanelID]; ok && owner.PanelName != "" {
			ownerName = owner.PanelName
		}

		for _, c := range ordered {
			if _, wants := c.DesiredHosts[host]; !wants {
				continue
			}
			if c.PanelID == cl.PanelID {
				owned[c.PanelID][host] = struct{}{}
			} else {
				conflicts[c.PanelID] = append(conflicts[c.PanelID], ConflictStatus{
					Host:       host,
					OwnerPanel: ownerName,
				})
			}
		}
	}

	for pid := range conflicts {
		sort.Slice(conflicts[pid], func(i, j int) bool {
			return conflicts[pid][i].Host < conflicts[pid][j].Host
		})
	}
	return owned, conflicts
}

// ReleasePanelClaims removes all claims held by panelID (used on panel removal).
func (cs *ClaimsStore) ReleasePanelClaims(panelID string) {
	if cs == nil || cs.Claims == nil {
		return
	}
	for host, cl := range cs.Claims {
		if cl.PanelID == panelID {
			delete(cs.Claims, host)
		}
	}
}
