package edge

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// LoadManualDomains reads /etc/nd-edge/domains.json. A missing file returns an empty store.
func LoadManualDomains(path string) (*ManualDomainsFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &ManualDomainsFile{Schema: ManualDomainsSchema}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return &ManualDomainsFile{Schema: ManualDomainsSchema}, nil
	}
	var out ManualDomainsFile
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if out.Schema == 0 {
		out.Schema = ManualDomainsSchema
	}
	if out.Schema != ManualDomainsSchema {
		return nil, fmt.Errorf("%s: unsupported schema %d", path, out.Schema)
	}
	SortManualDomains(out.Domains)
	return &out, nil
}

// SaveManualDomains atomically persists domains.json with mode 0600.
func SaveManualDomains(path string, f *ManualDomainsFile) error {
	if f == nil {
		f = &ManualDomainsFile{Schema: ManualDomainsSchema}
	}
	if f.Schema == 0 {
		f.Schema = ManualDomainsSchema
	}
	SortManualDomains(f.Domains)
	return WriteJSONAtomic(path, f, 0o600)
}

// SortManualDomains sorts manual entries deterministically by normalized domain.
func SortManualDomains(entries []ManualDomainEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Domain < entries[j].Domain
	})
}

// CloneManualDomainsFile returns a deep copy of f so callers can stage edits without mutating live state.
func CloneManualDomainsFile(f *ManualDomainsFile) *ManualDomainsFile {
	if f == nil {
		return &ManualDomainsFile{Schema: ManualDomainsSchema}
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return &ManualDomainsFile{Schema: ManualDomainsSchema}
	}
	var cp ManualDomainsFile
	if err := json.Unmarshal(raw, &cp); err != nil {
		return &ManualDomainsFile{Schema: ManualDomainsSchema}
	}
	if cp.Schema == 0 {
		cp.Schema = ManualDomainsSchema
	}
	return &cp
}

// FindManualDomain locates an entry by ID, exact primary domain, or claimed host/alias.
func FindManualDomain(f *ManualDomainsFile, query string) (int, *ManualDomainEntry) {
	if f == nil {
		return -1, nil
	}
	q := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(query)), ".")
	for i := range f.Domains {
		if f.Domains[i].ID == q || f.Domains[i].Domain == q {
			return i, &f.Domains[i]
		}
	}
	for i := range f.Domains {
		if f.Domains[i].MatchesHost(q) {
			return i, &f.Domains[i]
		}
	}
	return -1, nil
}

// CheckInternalManualDuplicates rejects hosts already claimed by another manual entry.
func CheckInternalManualDuplicates(f *ManualDomainsFile, candidate ManualDomainEntry, ignoreID string) error {
	if f == nil {
		return nil
	}
	wanted := make(map[string]struct{})
	for _, h := range candidate.AllHosts() {
		wanted[h] = struct{}{}
	}
	for _, existing := range f.Domains {
		if ignoreID != "" && existing.ID == ignoreID {
			continue
		}
		if existing.Domain == candidate.Domain {
			return fmt.Errorf("manual domain %q already exists (use 'nd edge domain set' to update)", candidate.Domain)
		}
		for _, h := range existing.AllHosts() {
			if _, clash := wanted[h]; clash {
				return fmt.Errorf("host %q is already claimed by manual domain %q", h, existing.Domain)
			}
		}
	}
	return nil
}

// ManualCertStem returns a filesystem-safe stem for storing copied manual TLS certificates.
func ManualCertStem(domain string) string {
	norm := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	return strings.ReplaceAll(norm, "*", "_wildcard_")
}

// ImportManualTLSCertPair validates srcCertPath/srcKeyPath and copies them into certsDir.
func ImportManualTLSCertPair(certsDir, domain, srcCertPath, srcKeyPath string, now time.Time) (dstCertPath, dstKeyPath string, err error) {
	if strings.TrimSpace(certsDir) == "" {
		return "", "", fmt.Errorf("manual certificate directory is empty")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	certPEM, err := os.ReadFile(srcCertPath)
	if err != nil {
		return "", "", fmt.Errorf("read --cert %s: %w", srcCertPath, err)
	}
	keyPEM, err := os.ReadFile(srcKeyPath)
	if err != nil {
		return "", "", fmt.Errorf("read --key %s: %w", srcKeyPath, err)
	}
	leaf, err := parseAndVerifyKeyPair(certPEM, keyPEM, now)
	if err != nil {
		return "", "", fmt.Errorf("invalid TLS cert/key pair: %w", err)
	}
	verifyTarget := domain
	if strings.HasPrefix(domain, "*.") {
		verifyTarget = "sub." + strings.TrimPrefix(domain, "*.")
	}
	if err := leaf.VerifyHostname(verifyTarget); err != nil {
		return "", "", fmt.Errorf("certificate does not cover %q: %w", domain, err)
	}

	if err := os.MkdirAll(certsDir, 0o755); err != nil {
		return "", "", fmt.Errorf("mkdir %s: %w", certsDir, err)
	}
	stem := ManualCertStem(domain)
	dstCertPath = filepath.Join(certsDir, stem+".crt")
	dstKeyPath = filepath.Join(certsDir, stem+".key")
	if err := WriteFileAtomic(dstCertPath, certPEM, 0o644); err != nil {
		return "", "", fmt.Errorf("write %s: %w", dstCertPath, err)
	}
	if err := WriteFileAtomic(dstKeyPath, keyPEM, 0o600); err != nil {
		return "", "", fmt.Errorf("write %s: %w", dstKeyPath, err)
	}
	return dstCertPath, dstKeyPath, nil
}

// LoadManualTrash reads /etc/nd-edge/domains.trash.json and filters out expired items.
func LoadManualTrash(path string, now time.Time) (*ManualTrashFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &ManualTrashFile{Schema: ManualDomainsSchema}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return &ManualTrashFile{Schema: ManualDomainsSchema}, nil
	}
	var out ManualTrashFile
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if out.Schema == 0 {
		out.Schema = ManualDomainsSchema
	}
	out.Entries = pruneExpiredTrash(out.Entries, now)
	return &out, nil
}

// SaveManualTrash prunes entries older than ManualTrashRetention and writes trash atomically.
func SaveManualTrash(path string, f *ManualTrashFile, now time.Time) error {
	if f == nil {
		f = &ManualTrashFile{Schema: ManualDomainsSchema}
	}
	if f.Schema == 0 {
		f.Schema = ManualDomainsSchema
	}
	f.Entries = pruneExpiredTrash(f.Entries, now)
	sort.Slice(f.Entries, func(i, j int) bool {
		if f.Entries[i].RemovedAt.Equal(f.Entries[j].RemovedAt) {
			return f.Entries[i].Entry.Domain < f.Entries[j].Entry.Domain
		}
		return f.Entries[i].RemovedAt.After(f.Entries[j].RemovedAt)
	})
	return WriteJSONAtomic(path, f, 0o600)
}

func pruneExpiredTrash(entries []ManualTrashEntry, now time.Time) []ManualTrashEntry {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cutoff := now.Add(-ManualTrashRetention)
	kept := make([]ManualTrashEntry, 0, len(entries))
	for _, item := range entries {
		if item.RemovedAt.IsZero() || !item.RemovedAt.Before(cutoff) {
			kept = append(kept, item)
		}
	}
	return kept
}

// MoveToTrash appends entry into trashFile, keeping newest entry per domain if overwritten.
func MoveToTrash(trashFile *ManualTrashFile, entry ManualDomainEntry, reason string, now time.Time) {
	if trashFile == nil {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	filtered := make([]ManualTrashEntry, 0, len(trashFile.Entries)+1)
	for _, existing := range trashFile.Entries {
		if existing.Entry.Domain == entry.Domain {
			continue
		}
		filtered = append(filtered, existing)
	}
	filtered = append(filtered, ManualTrashEntry{
		Entry:     entry,
		RemovedAt: now.UTC(),
		Reason:    reason,
	})
	trashFile.Entries = filtered
}

// TakeFromTrash removes and returns the matching entry from trashFile.
func TakeFromTrash(trashFile *ManualTrashFile, domain string) (ManualTrashEntry, bool) {
	if trashFile == nil {
		return ManualTrashEntry{}, false
	}
	q := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	for i, item := range trashFile.Entries {
		if item.Entry.Domain == q || item.Entry.ID == q {
			trashFile.Entries = append(trashFile.Entries[:i], trashFile.Entries[i+1:]...)
			return item, true
		}
	}
	return ManualTrashEntry{}, false
}

// ExportManualDomainsJSON serializes manual domains cleanly with schema version.
func ExportManualDomainsJSON(f *ManualDomainsFile) ([]byte, error) {
	cp := CloneManualDomainsFile(f)
	SortManualDomains(cp.Domains)
	return json.MarshalIndent(cp, "", "  ")
}

// ParseImportedManualDomains parses and validates an exported manual domains JSON payload.
func ParseImportedManualDomains(raw []byte, shieldHostname string) (*ManualDomainsFile, error) {
	var parsed ManualDomainsFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("parse import file: %w", err)
	}
	if parsed.Schema != 0 && parsed.Schema != ManualDomainsSchema {
		return nil, fmt.Errorf("unsupported schema %d", parsed.Schema)
	}
	parsed.Schema = ManualDomainsSchema
	seen := &ManualDomainsFile{Schema: ManualDomainsSchema}
	for i := range parsed.Domains {
		entry := parsed.Domains[i]
		if err := NormalizeAndValidateEntry(&entry, shieldHostname); err != nil {
			return nil, fmt.Errorf("entry %d (%q): %w", i+1, parsed.Domains[i].Domain, err)
		}
		if err := CheckInternalManualDuplicates(seen, entry, ""); err != nil {
			return nil, err
		}
		seen.Domains = append(seen.Domains, entry)
	}
	SortManualDomains(seen.Domains)
	return seen, nil
}
