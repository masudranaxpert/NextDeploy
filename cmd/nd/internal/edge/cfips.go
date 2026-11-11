package edge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	cfIPv4URL = "https://www.cloudflare.com/ips-v4"
	cfIPv6URL = "https://www.cloudflare.com/ips-v6"
)

// DefaultCloudflareRanges mirrors internal/caddy/options.go CloudflareRanges
// as the embedded fallback when /var/lib/nd-edge/cf-ips.json or Cloudflare's
// endpoints are unavailable.
var DefaultCloudflareRanges = []string{
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22", "141.101.64.0/18",
	"108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20", "197.234.240.0/22", "198.41.128.0/17",
	"162.158.0.0/15", "104.16.0.0/13", "104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
	"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32", "2405:8100::/32",
	"2a06:98c0::/29", "2c0f:f248::/32",
}

type cfIPsCacheFile struct {
	UpdatedAt time.Time `json:"updated_at"`
	Ranges    []string  `json:"ranges"`
}

// CFIPStore holds Cloudflare proxy CIDR ranges for DNS gate checks and
// Caddy trusted_proxies rendering.
type CFIPStore struct {
	mu       sync.RWMutex
	ranges   []string
	prefixes []netip.Prefix
}

// NewCFIPStore constructs a CFIPStore initialized with DefaultCloudflareRanges.
func NewCFIPStore() *CFIPStore {
	s := &CFIPStore{}
	_ = s.SetRanges(DefaultCloudflareRanges)
	return s
}

// LoadOrInitCFIPStore loads cached ranges from cachePath, falling back to
// DefaultCloudflareRanges if the cache does not exist or is invalid.
func LoadOrInitCFIPStore(cachePath string) *CFIPStore {
	s := NewCFIPStore()
	if cachePath == "" {
		return s
	}
	raw, err := os.ReadFile(cachePath)
	if err != nil {
		return s
	}
	var doc cfIPsCacheFile
	if err := json.Unmarshal(raw, &doc); err != nil || len(doc.Ranges) == 0 {
		return s
	}
	_ = s.SetRanges(doc.Ranges)
	return s
}

// SetRanges parses, normalizes, and deduplicates CIDR strings.
func (s *CFIPStore) SetRanges(cidrs []string) error {
	var parsed []netip.Prefix
	var strs []string
	seen := make(map[netip.Prefix]struct{})
	for _, raw := range cidrs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		p, err := netip.ParsePrefix(raw)
		if err != nil {
			continue
		}
		p = p.Masked()
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		parsed = append(parsed, p)
		strs = append(strs, p.String())
	}
	if len(parsed) == 0 {
		return errors.New("no valid Cloudflare CIDR ranges")
	}
	sort.Strings(strs)

	s.mu.Lock()
	s.prefixes = parsed
	s.ranges = strs
	s.mu.Unlock()
	return nil
}

// Contains reports whether addr falls inside any known Cloudflare CIDR range.
func (s *CFIPStore) Contains(addr netip.Addr) bool {
	if !addr.IsValid() {
		return false
	}
	addr = addr.Unmap()
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// Ranges returns a copy of the CIDR strings sorted deterministically.
func (s *CFIPStore) Ranges() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.ranges))
	copy(out, s.ranges)
	return out
}

// Refresh fetches Cloudflare IPv4/IPv6 CIDR lists, updates the in-memory store,
// and persists cachePath atomically. On failure it preserves existing ranges.
func (s *CFIPStore) Refresh(ctx context.Context, client *http.Client, cachePath string, ipv4URL, ipv6URL string) error {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	if ipv4URL == "" {
		ipv4URL = cfIPv4URL
	}
	if ipv6URL == "" {
		ipv6URL = cfIPv6URL
	}

	v4, err := fetchCIDRList(ctx, client, ipv4URL)
	if err != nil {
		return fmt.Errorf("fetch cloudflare ipv4: %w", err)
	}
	v6, err := fetchCIDRList(ctx, client, ipv6URL)
	if err != nil {
		return fmt.Errorf("fetch cloudflare ipv6: %w", err)
	}

	combined := append(v4, v6...)
	if err := s.SetRanges(combined); err != nil {
		return err
	}
	if cachePath != "" {
		doc := cfIPsCacheFile{
			UpdatedAt: time.Now().UTC(),
			Ranges:    s.Ranges(),
		}
		if err := WriteJSONAtomic(cachePath, doc, 0o644); err != nil {
			return fmt.Errorf("persist cf-ips cache: %w", err)
		}
	}
	return nil
}

func fetchCIDRList(ctx context.Context, client *http.Client, url string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP %d from %s", resp.StatusCode, url)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, err
	}

	var out []string
	sc := bufio.NewScanner(strings.NewReader(string(body)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, err := netip.ParsePrefix(line); err != nil {
			return nil, fmt.Errorf("invalid CIDR %q from %s: %w", line, url, err)
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty CIDR list from %s", url)
	}
	return out, sc.Err()
}
