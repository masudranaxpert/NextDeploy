package edge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

const (
	maxCNAMEDepth = 8
	dnsTimeout    = 4 * time.Second
)

var defaultPublicResolvers = []string{"1.1.1.1:53", "8.8.8.8:53"}

var (
	sslipDotIPv4Re  = regexp.MustCompile(`(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})`)
	sslipDashIPv4Re = regexp.MustCompile(`(\d{1,3}-\d{1,3}-\d{1,3}-\d{1,3})`)
)

// DNSResolver abstracts DNS wire queries for testing (fake resolver) and
// production (miekg/dns UDP with TCP fallback).
type DNSResolver interface {
	Exchange(ctx context.Context, server, qname string, qtype uint16) (*dns.Msg, error)
}

// SystemLookup abstracts OS resolver fallback when public resolvers are blocked (E-DNS-16).
type SystemLookup interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

type osSystemLookup struct{}

func (osSystemLookup) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, network, host)
}

// MiekgDNSResolver queries DNS servers over UDP and falls back to TCP if truncated.
type MiekgDNSResolver struct {
	Timeout time.Duration
}

// Exchange performs a DNS query against server (e.g. "1.1.1.1:53").
func (r *MiekgDNSResolver) Exchange(ctx context.Context, server, qname string, qtype uint16) (*dns.Msg, error) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = dnsTimeout
	}
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(qname), qtype)
	m.RecursionDesired = true

	udpClient := &dns.Client{Net: "udp", Timeout: timeout}
	resp, _, err := udpClient.ExchangeContext(ctx, m, server)
	if err == nil && resp != nil && !resp.Truncated {
		return resp, nil
	}
	if err == nil && resp != nil && resp.Truncated {
		tcpClient := &dns.Client{Net: "tcp", Timeout: timeout}
		tcpResp, _, tcpErr := tcpClient.ExchangeContext(ctx, m, server)
		if tcpErr == nil && tcpResp != nil {
			return tcpResp, nil
		}
		return resp, tcpErr
	}
	return nil, err
}

// DNSOutcome classifies the DNS gate decision for a host.
type DNSOutcome string

const (
	DNSOutcomeOK        DNSOutcome = "dns_ok"
	DNSOutcomeOKProxied DNSOutcome = "dns_ok_proxied"
	DNSOutcomePending   DNSOutcome = "pending_dns"
	DNSOutcomePendingCA DNSOutcome = "pending_caa"
)

// DNSCacheEntry stores the last evaluated DNS gate state for one host.
type DNSCacheEntry struct {
	Host           string       `json:"host"`
	Outcome        DNSOutcome   `json:"outcome"`
	Detail         string       `json:"detail,omitempty"`
	ResolvedIPs    []string     `json:"resolved_ips,omitempty"`
	FirstCheckedAt time.Time    `json:"first_checked_at"`
	LastCheckedAt  time.Time    `json:"last_checked_at"`
	NextCheckAt    time.Time    `json:"next_check_at"`
	SystemFallback bool         `json:"system_fallback,omitempty"`
}

type dnsCacheFile struct {
	Entries map[string]DNSCacheEntry `json:"entries"`
}

// DNSGate evaluates whether hosts point to the Shield (or Cloudflare) before
// rendering them into Caddy, preventing useless ACME failures (§3.5).
type DNSGate struct {
	mu        sync.Mutex
	resolver  DNSResolver
	sysLookup SystemLookup
	servers   []string
	cfIPs     *CFIPStore
	cachePath string
	entries   map[string]DNSCacheEntry
	acmeEmail string
	off       bool
}

// DNSGateOptions configures a DNSGate.
type DNSGateOptions struct {
	Resolver  DNSResolver
	SysLookup SystemLookup
	Servers   []string
	CFIPs     *CFIPStore
	CachePath string
	ACMEEmail string
	Off       bool
}

// NewDNSGate initializes a DNSGate and loads any cached entries from disk.
func NewDNSGate(opts DNSGateOptions) *DNSGate {
	res := opts.Resolver
	if res == nil {
		res = &MiekgDNSResolver{Timeout: dnsTimeout}
	}
	sys := opts.SysLookup
	if sys == nil {
		sys = osSystemLookup{}
	}
	servers := opts.Servers
	if len(servers) == 0 {
		servers = append([]string(nil), defaultPublicResolvers...)
	}
	cf := opts.CFIPs
	if cf == nil {
		cf = NewCFIPStore()
	}
	g := &DNSGate{
		resolver:  res,
		sysLookup: sys,
		servers:   servers,
		cfIPs:     cf,
		cachePath: opts.CachePath,
		entries:   make(map[string]DNSCacheEntry),
		acmeEmail: opts.ACMEEmail,
		off:       opts.Off,
	}
	g.loadCache()
	return g
}

// SetOff toggles --dns-gate=off.
func (g *DNSGate) SetOff(off bool) {
	g.mu.Lock()
	g.off = off
	g.mu.Unlock()
}

func (g *DNSGate) loadCache() {
	if g.cachePath == "" {
		return
	}
	raw, err := os.ReadFile(g.cachePath)
	if err != nil {
		return
	}
	var doc dnsCacheFile
	if err := json.Unmarshal(raw, &doc); err == nil && doc.Entries != nil {
		g.entries = doc.Entries
	}
}

// SaveCache persists the DNS cache atomically.
func (g *DNSGate) SaveCache() error {
	if g.cachePath == "" {
		return nil
	}
	g.mu.Lock()
	doc := dnsCacheFile{Entries: make(map[string]DNSCacheEntry, len(g.entries))}
	for k, v := range g.entries {
		doc.Entries[k] = v
	}
	g.mu.Unlock()
	return WriteJSONAtomic(g.cachePath, doc, 0o644)
}

// DNSResult is the outcome of checking one host against the DNS gate.
type DNSResult struct {
	Allowed        bool       // true if Caddy should render the host (includes sticky dns_drift)
	Outcome        DNSOutcome // dns_ok | dns_ok_proxied | pending_dns | pending_caa
	Drift          bool       // true if alreadyRendered && live DNS no longer matches
	Detail         string
	SystemFallback bool
}

// CheckHost evaluates DNS for normalized host, honoring backoff, 2-resolver
// agreement, sslip/nip embedded IP rules, CAA rules, and sticky dns_drift.
func (g *DNSGate) CheckHost(ctx context.Context, host string, shieldIPs []netip.Addr, alreadyRendered bool, forceCheck bool, now time.Time) DNSResult {
	g.mu.Lock()
	off := g.off
	cached, hasCached := g.entries[host]
	g.mu.Unlock()

	if off {
		return DNSResult{Allowed: true, Outcome: DNSOutcomeOK}
	}

	// Embedded sslip.io / nip.io check requires no DNS lookup (E-DNS-14).
	if embIP, isAuto := extractEmbeddedWildcardIP(host); isAuto {
		if !embIP.IsValid() {
			return g.finishSticky(host, DNSOutcomePending, "sslip.io/nip.io hostname missing valid embedded IP", nil, false, alreadyRendered, now)
		}
		if !ipInSet(embIP, shieldIPs) {
			detail := fmt.Sprintf("embedded IP %s does not match Shield IP", embIP)
			return g.finishSticky(host, DNSOutcomePending, detail, []string{embIP.String()}, false, alreadyRendered, now)
		}
		return g.finishSticky(host, DNSOutcomeOK, "", []string{embIP.String()}, false, alreadyRendered, now)
	}

	// Honor per-host re-check backoff unless forced or uncached.
	if !forceCheck && hasCached && !cached.NextCheckAt.IsZero() && now.Before(cached.NextCheckAt) {
		return resultFromEntry(cached, alreadyRendered)
	}

	entry := g.probeHost(ctx, host, shieldIPs, hasCached, cached, now)

	g.mu.Lock()
	g.entries[host] = entry
	g.mu.Unlock()

	return resultFromEntry(entry, alreadyRendered)
}

func (g *DNSGate) finishSticky(host string, outcome DNSOutcome, detail string, ips []string, sysFallback, alreadyRendered bool, now time.Time) DNSResult {
	entry := DNSCacheEntry{
		Host:           host,
		Outcome:        outcome,
		Detail:         detail,
		ResolvedIPs:    ips,
		FirstCheckedAt: now,
		LastCheckedAt:  now,
		NextCheckAt:    now.Add(5 * time.Minute),
		SystemFallback: sysFallback,
	}
	g.mu.Lock()
	if prev, ok := g.entries[host]; ok && !prev.FirstCheckedAt.IsZero() {
		entry.FirstCheckedAt = prev.FirstCheckedAt
	}
	g.entries[host] = entry
	g.mu.Unlock()
	return resultFromEntry(entry, alreadyRendered)
}

func resultFromEntry(e DNSCacheEntry, alreadyRendered bool) DNSResult {
	switch e.Outcome {
	case DNSOutcomeOK, DNSOutcomeOKProxied:
		return DNSResult{
			Allowed:        true,
			Outcome:        e.Outcome,
			Detail:         e.Detail,
			SystemFallback: e.SystemFallback,
		}
	default:
		if alreadyRendered {
			// Sticky (§3.5, E-DNS-8): never remove already-rendered hosts on DNS change.
			return DNSResult{
				Allowed:        true,
				Outcome:        e.Outcome,
				Drift:          true,
				Detail:         e.Detail,
				SystemFallback: e.SystemFallback,
			}
		}
		return DNSResult{
			Allowed:        false,
			Outcome:        e.Outcome,
			Detail:         e.Detail,
			SystemFallback: e.SystemFallback,
		}
	}
}

type singleServerResult struct {
	outcome DNSOutcome
	detail  string
	ips     []string
	err     error
}

func (g *DNSGate) probeHost(ctx context.Context, host string, shieldIPs []netip.Addr, hasCached bool, cached DNSCacheEntry, now time.Time) DNSCacheEntry {
	firstChecked := now
	if hasCached && !cached.FirstCheckedAt.IsZero() {
		firstChecked = cached.FirstCheckedAt
	}

	results := make([]singleServerResult, 0, len(g.servers))
	var networkFailures int
	for _, srv := range g.servers {
		res := g.queryOneServer(ctx, srv, host, shieldIPs)
		if res.err != nil {
			networkFailures++
		}
		results = append(results, res)
	}

	// E-DNS-16: all public resolvers failed -> fallback to system resolver and warn.
	if networkFailures == len(g.servers) {
		sysRes := g.querySystemFallback(ctx, host, shieldIPs)
		if sysRes.err == nil {
			return DNSCacheEntry{
				Host:           host,
				Outcome:        sysRes.outcome,
				Detail:         appendWarning(sysRes.detail, "public DNS resolvers unreachable; used system resolver fallback"),
				ResolvedIPs:    sysRes.ips,
				FirstCheckedAt: firstChecked,
				LastCheckedAt:  now,
				NextCheckAt:    nextDNSCheckTime(sysRes.outcome, firstChecked, now),
				SystemFallback: true,
			}
		}
		// E-BOOT-5: DNS completely down; keep previous cached state if we have one.
		if hasCached {
			cached.LastCheckedAt = now
			cached.NextCheckAt = now.Add(time.Minute)
			return cached
		}
		return DNSCacheEntry{
			Host:           host,
			Outcome:        DNSOutcomePending,
			Detail:         fmt.Sprintf("DNS lookup failed: %v", sysRes.err),
			FirstCheckedAt: firstChecked,
			LastCheckedAt:  now,
			NextCheckAt:    nextDNSCheckTime(DNSOutcomePending, firstChecked, now),
		}
	}

	// Collect successful resolver answers.
	var good []singleServerResult
	for _, r := range results {
		if r.err == nil {
			good = append(good, r)
		}
	}

	// Require 2-resolver agreement when >=2 resolvers responded (E-DNS-7).
	if len(good) >= 2 {
		if !resolversAgree(good[0], good[1]) {
			if hasCached {
				cached.LastCheckedAt = now
				cached.NextCheckAt = now.Add(time.Minute)
				return cached
			}
			return DNSCacheEntry{
				Host:           host,
				Outcome:        DNSOutcomePending,
				Detail:         fmt.Sprintf("DNS resolvers disagree (%s vs %s); waiting for propagation", good[0].outcome, good[1].outcome),
				FirstCheckedAt: firstChecked,
				LastCheckedAt:  now,
				NextCheckAt:    nextDNSCheckTime(DNSOutcomePending, firstChecked, now),
			}
		}
	} else if len(good) == 1 && len(g.servers) >= 2 {
		// One resolver unreachable, one answered: if we already have a cached entry,
		// avoid flipping state on a single-resolver view unless uncached.
		if hasCached && cached.Outcome != good[0].outcome {
			cached.LastCheckedAt = now
			cached.NextCheckAt = now.Add(time.Minute)
			return cached
		}
	}

	chosen := good[0]
	return DNSCacheEntry{
		Host:           host,
		Outcome:        chosen.outcome,
		Detail:         chosen.detail,
		ResolvedIPs:    chosen.ips,
		FirstCheckedAt: firstChecked,
		LastCheckedAt:  now,
		NextCheckAt:    nextDNSCheckTime(chosen.outcome, firstChecked, now),
	}
}

func resolversAgree(a, b singleServerResult) bool {
	if a.outcome != b.outcome {
		return false
	}
	if a.outcome == DNSOutcomePending || a.outcome == DNSOutcomePendingCA {
		return true
	}
	// If both are OK / OKProxied, verify resolved IPs match when not CF-proxied.
	if a.outcome == DNSOutcomeOKProxied {
		return true
	}
	if len(a.ips) != len(b.ips) {
		return false
	}
	for i := range a.ips {
		if a.ips[i] != b.ips[i] {
			return false
		}
	}
	return true
}

func (g *DNSGate) queryOneServer(ctx context.Context, server, host string, shieldIPs []netip.Addr) singleServerResult {
	aIPs, nxdomainA, err := g.resolveAddrChain(ctx, server, host, dns.TypeA)
	if err != nil {
		return singleServerResult{err: err}
	}
	aaaaIPs, nxdomainAAAA, err := g.resolveAddrChain(ctx, server, host, dns.TypeAAAA)
	if err != nil {
		return singleServerResult{err: err}
	}

	caaBlocked, caaDetail, err := g.checkCAA(ctx, server, host)
	if err == nil && caaBlocked {
		return singleServerResult{
			outcome: DNSOutcomePendingCA,
			detail:  caaDetail,
		}
	}

	return classifyResolvedIPs(aIPs, aaaaIPs, nxdomainA && nxdomainAAAA, shieldIPs, g.cfIPs)
}

func (g *DNSGate) querySystemFallback(ctx context.Context, host string, shieldIPs []netip.Addr) singleServerResult {
	addrs, err := g.sysLookup.LookupNetIP(ctx, "ip", host)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return classifyResolvedIPs(nil, nil, true, shieldIPs, g.cfIPs)
		}
		return singleServerResult{err: err}
	}
	var aIPs, aaaaIPs []netip.Addr
	for _, a := range addrs {
		a = a.Unmap()
		if a.Is4() {
			aIPs = append(aIPs, a)
		} else if a.Is6() {
			aaaaIPs = append(aaaaIPs, a)
		}
	}
	return classifyResolvedIPs(aIPs, aaaaIPs, false, shieldIPs, g.cfIPs)
}

func classifyResolvedIPs(aIPs, aaaaIPs []netip.Addr, nxdomain bool, shieldIPs []netip.Addr, cfIPs *CFIPStore) singleServerResult {
	allStrs := make([]string, 0, len(aIPs)+len(aaaaIPs))
	for _, ip := range aIPs {
		allStrs = append(allStrs, ip.String())
	}
	for _, ip := range aaaaIPs {
		allStrs = append(allStrs, ip.String())
	}
	sort.Strings(allStrs)

	if len(aIPs) == 0 && len(aaaaIPs) == 0 {
		if nxdomain {
			return singleServerResult{outcome: DNSOutcomePending, detail: "NXDOMAIN (no DNS record found)"}
		}
		return singleServerResult{outcome: DNSOutcomePending, detail: "no A or AAAA records found"}
	}

	// Cloudflare proxied (E-DNS-3): any A/AAAA in Cloudflare ranges -> trust it.
	for _, ip := range aIPs {
		if cfIPs.Contains(ip) {
			return singleServerResult{outcome: DNSOutcomeOKProxied, ips: allStrs}
		}
	}
	for _, ip := range aaaaIPs {
		if cfIPs.Contains(ip) {
			return singleServerResult{outcome: DNSOutcomeOKProxied, ips: allStrs}
		}
	}

	// E-DNS-5: Let's Encrypt prefers IPv6; any AAAA pointing elsewhere breaks issuance.
	var wrongAAAA []string
	for _, ip := range aaaaIPs {
		if !ipInSet(ip, shieldIPs) {
			wrongAAAA = append(wrongAAAA, ip.String())
		}
	}
	if len(wrongAAAA) > 0 {
		return singleServerResult{
			outcome: DNSOutcomePending,
			detail:  fmt.Sprintf("remove or fix AAAA (%s); Let's Encrypt prefers IPv6", strings.Join(wrongAAAA, ", ")),
			ips:     allStrs,
		}
	}

	// Check A records against Shield IPs.
	var wrongA []string
	for _, ip := range aIPs {
		if !ipInSet(ip, shieldIPs) {
			wrongA = append(wrongA, ip.String())
		}
	}
	if len(wrongA) > 0 {
		return singleServerResult{
			outcome: DNSOutcomePending,
			detail:  fmt.Sprintf("points to %s instead of Shield IP", strings.Join(wrongA, ", ")),
			ips:     allStrs,
		}
	}

	if len(aIPs) == 0 && len(aaaaIPs) > 0 {
		// All AAAA match Shield IPv6.
		return singleServerResult{outcome: DNSOutcomeOK, ips: allStrs}
	}
	return singleServerResult{outcome: DNSOutcomeOK, ips: allStrs}
}

// resolveAddrChain follows CNAMEs up to maxCNAMEDepth (E-DNS-17) and returns resolved A/AAAA IPs.
func (g *DNSGate) resolveAddrChain(ctx context.Context, server, host string, qtype uint16) ([]netip.Addr, bool, error) {
	current := dns.Fqdn(host)
	visited := map[string]struct{}{current: {}}

	for depth := 0; depth <= maxCNAMEDepth; depth++ {
		msg, err := g.resolver.Exchange(ctx, server, current, qtype)
		if err != nil {
			return nil, false, err
		}
		if msg == nil {
			return nil, false, errors.New("nil DNS response")
		}
		if msg.Rcode == dns.RcodeNameError {
			return nil, true, nil
		}
		if msg.Rcode != dns.RcodeSuccess {
			return nil, false, fmt.Errorf("DNS rcode %s", dns.RcodeToString[msg.Rcode])
		}

		var ips []netip.Addr
		var nextCNAME string
		for _, rr := range msg.Answer {
			switch v := rr.(type) {
			case *dns.A:
				if qtype == dns.TypeA {
					if a, ok := netip.AddrFromSlice(v.A); ok {
						ips = append(ips, a.Unmap())
					}
				}
			case *dns.AAAA:
				if qtype == dns.TypeAAAA {
					if a, ok := netip.AddrFromSlice(v.AAAA); ok {
						ips = append(ips, a.Unmap())
					}
				}
			case *dns.CNAME:
				if strings.EqualFold(dns.Fqdn(v.Hdr.Name), current) {
					nextCNAME = dns.Fqdn(v.Target)
				}
			}
		}

		if len(ips) > 0 {
			return ips, false, nil
		}
		if nextCNAME == "" {
			return nil, false, nil
		}
		if _, seen := visited[nextCNAME]; seen {
			return nil, false, errors.New("CNAME loop detected")
		}
		if depth == maxCNAMEDepth {
			return nil, false, errors.New("CNAME chain exceeded max depth 8")
		}
		visited[nextCNAME] = struct{}{}
		current = nextCNAME
	}
	return nil, false, nil
}

// checkCAA walks host labels upward looking for CAA records (RFC 8659) and
// checks whether Let's Encrypt (or ZeroSSL when acmeEmail is set) is allowed.
func (g *DNSGate) checkCAA(ctx context.Context, server, host string) (blocked bool, detail string, err error) {
	labels := strings.Split(strings.TrimSuffix(host, "."), ".")
	for i := 0; i < len(labels)-1; i++ {
		domain := strings.Join(labels[i:], ".")
		msg, exErr := g.resolver.Exchange(ctx, server, domain, dns.TypeCAA)
		if exErr != nil {
			return false, "", exErr
		}
		if msg == nil || msg.Rcode != dns.RcodeSuccess {
			continue
		}
		var issueVals []string
		for _, rr := range msg.Answer {
			if caa, ok := rr.(*dns.CAA); ok {
				if strings.EqualFold(caa.Tag, "issue") {
					issueVals = append(issueVals, strings.TrimSpace(caa.Value))
				}
			}
		}
		if len(issueVals) == 0 {
			continue
		}
		allowed := false
		for _, v := range issueVals {
			caDomain := strings.ToLower(strings.TrimSpace(strings.SplitN(v, ";", 2)[0]))
			if caDomain == "letsencrypt.org" {
				allowed = true
				break
			}
			if g.acmeEmail != "" && caDomain == "zerossl.com" {
				allowed = true
				break
			}
		}
		if !allowed {
			return true, fmt.Sprintf("CAA at %s forbids letsencrypt.org (%s)", domain, strings.Join(issueVals, ", ")), nil
		}
		return false, "", nil
	}
	return false, "", nil
}

func nextDNSCheckTime(outcome DNSOutcome, firstChecked, now time.Time) time.Time {
	if outcome == DNSOutcomeOK || outcome == DNSOutcomeOKProxied {
		return now.Add(5 * time.Minute)
	}
	age := now.Sub(firstChecked)
	switch {
	case age < time.Hour:
		return now.Add(60 * time.Second)
	case age < 24*time.Hour:
		return now.Add(5 * time.Minute)
	default:
		return now.Add(30 * time.Minute)
	}
}

func extractEmbeddedWildcardIP(host string) (netip.Addr, bool) {
	if !strings.HasSuffix(host, ".sslip.io") && !strings.HasSuffix(host, ".nip.io") {
		return netip.Addr{}, false
	}
	if matches := sslipDashIPv4Re.FindAllString(host, -1); len(matches) > 0 {
		candidate := strings.ReplaceAll(matches[len(matches)-1], "-", ".")
		if a, err := netip.ParseAddr(candidate); err == nil {
			return a.Unmap(), true
		}
	}
	if matches := sslipDotIPv4Re.FindAllString(host, -1); len(matches) > 0 {
		if a, err := netip.ParseAddr(matches[len(matches)-1]); err == nil {
			return a.Unmap(), true
		}
	}
	return netip.Addr{}, true
}

func ipInSet(ip netip.Addr, set []netip.Addr) bool {
	ip = ip.Unmap()
	for _, s := range set {
		if s.Unmap() == ip {
			return true
		}
	}
	return false
}

func appendWarning(base, warn string) string {
	if base == "" {
		return warn
	}
	return base + "; " + warn
}
