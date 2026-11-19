package edge

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	PruneReasonDNS    = "dns"
	PruneReasonOrigin = "origin"
	PruneReasonAll    = "all"
)

// DomainsHealthStore persists bad-check counters & first-seen timestamps in
// /var/lib/nd-edge/domains.health.json for multi-check grace-period pruning.
type DomainsHealthStore struct {
	Schema  int                         `json:"schema"`
	Records map[string]DomainHealthStat `json:"records"`
}

// DomainHealthStat tracks consecutive DNS and origin failure history for one domain.
type DomainHealthStat struct {
	Domain           string     `json:"domain"`
	FirstBadDNSAt    *time.Time `json:"first_bad_dns_at,omitempty"`
	BadDNSCount      int        `json:"bad_dns_count,omitempty"`
	LastDNSStatus    string     `json:"last_dns_status,omitempty"`
	FirstBadOriginAt *time.Time `json:"first_bad_origin_at,omitempty"`
	BadOriginCount   int        `json:"bad_origin_count,omitempty"`
	LastOriginStatus string     `json:"last_origin_status,omitempty"`
	LastCheckedAt    time.Time  `json:"last_checked_at"`
}

// OriginHealthResult describes an upstream dial + HTTP(S) probe outcome.
type OriginHealthResult struct {
	Status     string `json:"status"` // up | refused | timeout | tls_error
	Detail     string `json:"detail,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
	LatencyMs  int64  `json:"latency_ms,omitempty"`
}

// TLSHealthResult describes a site's served/stored certificate health.
type TLSHealthResult struct {
	Status   string     `json:"status"` // valid (N days) | expiring (N days) | missing | acme_failing | none
	DaysLeft int        `json:"days_left,omitempty"`
	NotAfter *time.Time `json:"not_after,omitempty"`
	Detail   string     `json:"detail,omitempty"`
}

// DomainCheckReport summarizes one domain's DNS, origin, TLS, and traffic health.
type DomainCheckReport struct {
	Domain         string             `json:"domain"`
	Source         string             `json:"source"` // "manual" or panel name
	Enabled        bool               `json:"enabled"`
	TLSMode        string             `json:"tls"`
	DNS            FreshDNSInspection `json:"dns"`
	Origin         OriginHealthResult `json:"origin"`
	TLS            TLSHealthResult    `json:"tls_health"`
	Traffic        string             `json:"traffic"` // count or "unknown"
	BadDNSCount    int                `json:"bad_dns_count,omitempty"`
	BadDNSSince    *time.Time         `json:"bad_dns_since,omitempty"`
	BadOriginCount int                `json:"bad_origin_count,omitempty"`
	BadOriginSince *time.Time         `json:"bad_origin_since,omitempty"`
}

// PruneCandidate explains why a manual domain matched (or did not yet match) prune criteria.
type PruneCandidate struct {
	Entry      ManualDomainEntry `json:"entry"`
	Report     DomainCheckReport `json:"report"`
	ReasonTag  string            `json:"reason"`   // e.g. prune:dns, prune:origin, prune:dns+origin
	Detail     string            `json:"detail"`
	BadSince   time.Time         `json:"bad_since"`
	CheckCount int               `json:"check_count"`
}

// LoadDomainsHealth reads /var/lib/nd-edge/domains.health.json.
func LoadDomainsHealth(path string) (*DomainsHealthStore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &DomainsHealthStore{
				Schema:  ManualDomainsSchema,
				Records: make(map[string]DomainHealthStat),
			}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return &DomainsHealthStore{
			Schema:  ManualDomainsSchema,
			Records: make(map[string]DomainHealthStat),
		}, nil
	}
	var out DomainsHealthStore
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if out.Schema == 0 {
		out.Schema = ManualDomainsSchema
	}
	if out.Records == nil {
		out.Records = make(map[string]DomainHealthStat)
	}
	return &out, nil
}

// SaveDomainsHealth atomically writes domains.health.json.
func SaveDomainsHealth(path string, st *DomainsHealthStore) error {
	if st == nil {
		st = &DomainsHealthStore{Schema: ManualDomainsSchema, Records: make(map[string]DomainHealthStat)}
	}
	if st.Schema == 0 {
		st.Schema = ManualDomainsSchema
	}
	if st.Records == nil {
		st.Records = make(map[string]DomainHealthStat)
	}
	return WriteJSONAtomic(path, st, 0o600)
}

// Record updates consecutive failure history for domain using dnsClass and originStatus.
func (s *DomainsHealthStore) Record(domain, dnsClass, originStatus string, now time.Time) DomainHealthStat {
	if s.Records == nil {
		s.Records = make(map[string]DomainHealthStat)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	rec := s.Records[domain]
	rec.Domain = domain
	rec.LastCheckedAt = now

	if dnsClass != "" {
		rec.LastDNSStatus = dnsClass
		if IsBadDNSClassification(dnsClass) {
			if rec.FirstBadDNSAt == nil {
				t := now
				rec.FirstBadDNSAt = &t
			}
			rec.BadDNSCount++
		} else if dnsClass == "ok" || dnsClass == "ok_proxied" {
			rec.FirstBadDNSAt = nil
			rec.BadDNSCount = 0
		}
		// "error" / "pending_caa" leave counters unchanged so transient DNS outages do not prune or reset.
	}

	if originStatus != "" {
		rec.LastOriginStatus = originStatus
		if IsBadOriginStatus(originStatus) {
			if rec.FirstBadOriginAt == nil {
				t := now
				rec.FirstBadOriginAt = &t
			}
			rec.BadOriginCount++
		} else if originStatus == "up" {
			rec.FirstBadOriginAt = nil
			rec.BadOriginCount = 0
		}
	}

	s.Records[domain] = rec
	return rec
}

// Delete removes records for removed domains.
func (s *DomainsHealthStore) Delete(domains ...string) {
	if s == nil || s.Records == nil {
		return
	}
	for _, d := range domains {
		delete(s.Records, d)
	}
}

// IsBadDNSClassification reports whether dnsClass represents dead or mispointed DNS.
// ok_proxied (Cloudflare) is explicitly safe and never considered bad.
func IsBadDNSClassification(dnsClass string) bool {
	switch dnsClass {
	case "nxdomain", "no_records", "elsewhere":
		return true
	default:
		return false
	}
}

// IsBadOriginStatus reports whether originStatus indicates a dead or broken origin.
func IsBadOriginStatus(status string) bool {
	switch status {
	case "refused", "timeout", "tls_error":
		return true
	default:
		return false
	}
}

// CheckOriginHealth probes each upstream configured on entry until one responds.
func CheckOriginHealth(ctx context.Context, entry ManualDomainEntry, timeout time.Duration) OriginHealthResult {
	urls := make([]string, len(entry.Upstreams))
	for i, u := range entry.Upstreams {
		urls[i] = u.URL()
	}
	hostHdr := ""
	switch entry.EffectiveHostHeader() {
	case ManualHostHeaderOriginal:
		if strings.HasPrefix(entry.Domain, "*.") {
			hostHdr = "sub." + strings.TrimPrefix(entry.Domain, "*.")
		} else {
			hostHdr = entry.Domain
		}
	case ManualHostHeaderUpstream:
		// Leave empty so net/http sends the upstream host:port.
	default:
		hostHdr = entry.EffectiveHostHeader()
	}
	sni := entry.Origin.SNI
	if sni == "$host" {
		sni = hostHdr
	}
	return CheckOriginHealthTargets(ctx, urls, hostHdr, sni, entry.Origin.NoVerify, timeout)
}

// CheckOriginHealthTargets probes one or more upstream HTTP(S) URLs using TCP connect + HTTP request.
func CheckOriginHealthTargets(ctx context.Context, urls []string, hostHeader, sni string, noVerify bool, timeout time.Duration) OriginHealthResult {
	if len(urls) == 0 {
		return OriginHealthResult{Status: "refused", Detail: "no upstreams configured"}
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	var last OriginHealthResult
	for _, target := range urls {
		res := probeSingleUpstream(ctx, target, hostHeader, sni, noVerify, timeout)
		if res.Status == "up" {
			return res
		}
		last = res
	}
	return last
}

func probeSingleUpstream(ctx context.Context, targetURL, hostHeader, sni string, noVerify bool, timeout time.Duration) OriginHealthResult {
	start := time.Now()
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	tlsCfg := &tls.Config{
		InsecureSkipVerify: noVerify,
	}
	if sni != "" {
		tlsCfg.ServerName = sni
	}
	tr := &http.Transport{
		DialContext: (&net.Dialer{Timeout: timeout}).DialContext,
		TLSClientConfig: tlsCfg,
		DisableKeepAlives: true,
	}
	defer tr.CloseIdleConnections()

	client := &http.Client{
		Transport: tr,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, targetURL+"/", nil)
	if err != nil {
		return OriginHealthResult{Status: "refused", Detail: err.Error()}
	}
	if hostHeader != "" {
		req.Host = hostHeader
	}
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err == nil {
		_ = resp.Body.Close()
		return OriginHealthResult{
			Status:     "up",
			StatusCode: resp.StatusCode,
			LatencyMs:  latency,
		}
	}

	msg := err.Error()
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return OriginHealthResult{Status: "timeout", Detail: msg, LatencyMs: latency}
	}
	var tlsRecord tls.RecordHeaderError
	var x509Unknown x509.UnknownAuthorityError
	var x509Host x509.HostnameError
	var x509Invalid x509.CertificateInvalidError
	if errors.As(err, &tlsRecord) || errors.As(err, &x509Unknown) || errors.As(err, &x509Host) || errors.As(err, &x509Invalid) ||
		strings.Contains(strings.ToLower(msg), "tls:") || strings.Contains(strings.ToLower(msg), "x509:") {
		return OriginHealthResult{Status: "tls_error", Detail: msg, LatencyMs: latency}
	}
	return OriginHealthResult{Status: "refused", Detail: msg, LatencyMs: latency}
}

// CheckHostTLSHealth inspects local TLS listener (via CertProbe) or stored manual certificate.
func CheckHostTLSHealth(ctx context.Context, probe *CertProbe, entry *ManualDomainEntry, host string, now time.Time) TLSHealthResult {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if entry != nil && entry.TLS == ManualTLSNone {
		return TLSHealthResult{Status: "none"}
	}

	probeHost := host
	if strings.HasPrefix(probeHost, "*.") {
		probeHost = "sub." + strings.TrimPrefix(probeHost, "*.")
	}
	if probe != nil {
		res := probe.ProbeHost(ctx, probeHost, now)
		if res.Valid && res.NotAfter != nil {
			days := int(res.NotAfter.Sub(now).Hours() / 24)
			if days < 0 {
				days = 0
			}
			label := fmt.Sprintf("valid (%d days)", days)
			if res.RenewWarn {
				label = fmt.Sprintf("expiring (%d days)", days)
			}
			return TLSHealthResult{
				Status:   label,
				DaysLeft: days,
				NotAfter: res.NotAfter,
			}
		}
		if entry != nil && entry.TLS == ManualTLSManual && entry.CertFile != "" {
			if fileRes, ok := inspectCertFileExpiry(entry.CertFile, now); ok {
				return fileRes
			}
		}
		if res.ErrDetail != "" {
			if strings.Contains(res.ErrDetail, "cert verify") || strings.Contains(res.ErrDetail, "tls handshake") {
				return TLSHealthResult{Status: "acme_failing", Detail: res.ErrDetail}
			}
		}
	}

	if entry != nil && entry.TLS == ManualTLSManual && entry.CertFile != "" {
		if fileRes, ok := inspectCertFileExpiry(entry.CertFile, now); ok {
			return fileRes
		}
	}
	if entry != nil && !entry.Enabled {
		return TLSHealthResult{Status: "missing", Detail: "domain is disabled"}
	}
	return TLSHealthResult{Status: "missing"}
}

func inspectCertFileExpiry(certPath string, now time.Time) (TLSHealthResult, bool) {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return TLSHealthResult{}, false
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return TLSHealthResult{}, false
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return TLSHealthResult{}, false
	}
	na := leaf.NotAfter.UTC()
	if !now.Before(na) {
		return TLSHealthResult{Status: "expiring (0 days)", DaysLeft: 0, NotAfter: &na, Detail: "certificate expired"}, true
	}
	days := int(na.Sub(now).Hours() / 24)
	label := fmt.Sprintf("valid (%d days)", days)
	if na.Sub(now) < certRenewWarnWindow {
		label = fmt.Sprintf("expiring (%d days)", days)
	}
	return TLSHealthResult{
		Status:   label,
		DaysLeft: days,
		NotAfter: &na,
	}, true
}

// EvaluatePruneCandidates selects manual domains eligible for pruning:
// a condition must match reason (dns|origin|all), persist for >= grace, and
// appear across at least 2 checks. ok_proxied is never pruned.
func EvaluatePruneCandidates(
	entries []ManualDomainEntry,
	reports map[string]DomainCheckReport,
	health *DomainsHealthStore,
	reason string,
	grace time.Duration,
	now time.Time,
) []PruneCandidate {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if reason == "" {
		reason = PruneReasonDNS
	}
	var candidates []PruneCandidate
	for _, e := range entries {
		rep, ok := reports[e.Domain]
		if !ok {
			continue
		}
		stat, hasStat := health.Records[e.Domain]
		if !hasStat {
			continue
		}

		dnsMatch := false
		originMatch := false

		if reason == PruneReasonDNS || reason == PruneReasonAll {
			if IsBadDNSClassification(rep.DNS.Classification) &&
				stat.FirstBadDNSAt != nil &&
				stat.BadDNSCount >= 2 &&
				now.Sub(*stat.FirstBadDNSAt) >= grace {
				dnsMatch = true
			}
		}
		if reason == PruneReasonOrigin || reason == PruneReasonAll {
			if IsBadOriginStatus(rep.Origin.Status) &&
				stat.FirstBadOriginAt != nil &&
				stat.BadOriginCount >= 2 &&
				now.Sub(*stat.FirstBadOriginAt) >= grace {
				originMatch = true
			}
		}

		if !dnsMatch && !originMatch {
			continue
		}

		var reasonTag string
		var details []string
		var badSince time.Time
		var count int

		switch {
		case dnsMatch && originMatch:
			reasonTag = "prune:dns+origin"
			details = append(details, fmt.Sprintf("dns=%s (%dd)", rep.DNS.Classification, int(now.Sub(*stat.FirstBadDNSAt).Hours()/24)))
			details = append(details, fmt.Sprintf("origin=%s (%dd)", rep.Origin.Status, int(now.Sub(*stat.FirstBadOriginAt).Hours()/24)))
			badSince = *stat.FirstBadDNSAt
			if stat.FirstBadOriginAt.Before(badSince) {
				badSince = *stat.FirstBadOriginAt
			}
			count = stat.BadDNSCount
			if stat.BadOriginCount > count {
				count = stat.BadOriginCount
			}
		case dnsMatch:
			reasonTag = "prune:dns"
			details = append(details, fmt.Sprintf("dns=%s", rep.DNS.Classification))
			badSince = *stat.FirstBadDNSAt
			count = stat.BadDNSCount
		default:
			reasonTag = "prune:origin"
			details = append(details, fmt.Sprintf("origin=%s", rep.Origin.Status))
			badSince = *stat.FirstBadOriginAt
			count = stat.BadOriginCount
		}

		candidates = append(candidates, PruneCandidate{
			Entry:      e,
			Report:     rep,
			ReasonTag:  reasonTag,
			Detail:     strings.Join(details, "; "),
			BadSince:   badSince,
			CheckCount: count,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Entry.Domain < candidates[j].Entry.Domain
	})
	return candidates
}

// CleanupRemovedDomainArtifacts removes DNS cache entries, health records, and
// orphaned ACME / manual cert files that are no longer referenced by any active
// manual domain or panel claim.
func CleanupRemovedDomainArtifacts(
	paths Paths,
	remaining *ManualDomainsFile,
	claims *ClaimsStore,
	dnsGate *DNSGate,
	health *DomainsHealthStore,
	removed []ManualDomainEntry,
) {
	if len(removed) == 0 {
		return
	}

	keepHosts := make(map[string]struct{})
	keepCertFiles := make(map[string]struct{})
	if remaining != nil {
		for _, e := range remaining.Domains {
			for _, h := range e.AllHosts() {
				keepHosts[h] = struct{}{}
			}
			if e.CertFile != "" {
				keepCertFiles[filepath.Clean(e.CertFile)] = struct{}{}
			}
			if e.KeyFile != "" {
				keepCertFiles[filepath.Clean(e.KeyFile)] = struct{}{}
			}
		}
	}
	if claims != nil {
		for h, cl := range claims.Claims {
			if cl.PanelID != "" && cl.PanelID != ManualClaimPanelID {
				keepHosts[h] = struct{}{}
			}
		}
	}

	var deadHosts []string
	var deadPrimary []string
	for _, e := range removed {
		deadPrimary = append(deadPrimary, e.Domain)
		for _, h := range e.AllHosts() {
			if _, stillUsed := keepHosts[h]; !stillUsed {
				deadHosts = append(deadHosts, h)
			}
		}
		if e.CertFile != "" {
			if _, keep := keepCertFiles[filepath.Clean(e.CertFile)]; !keep {
				_ = os.Remove(e.CertFile)
			}
		}
		if e.KeyFile != "" {
			if _, keep := keepCertFiles[filepath.Clean(e.KeyFile)]; !keep {
				_ = os.Remove(e.KeyFile)
			}
		}
	}

	if dnsGate != nil && len(deadHosts) > 0 {
		dnsGate.RemoveHosts(deadHosts)
	}
	if health != nil && len(deadPrimary) > 0 {
		health.Delete(deadPrimary...)
	}

	// Clean orphaned imported cert pairs and ACME host directories across all engine layouts.
	enginePaths := []EnginePaths{
		paths.EnginePathsFor(EngineCaddy),
		paths.EnginePathsFor(EngineAngie),
		paths.EnginePathsFor(EngineFerron),
	}
	for _, ep := range enginePaths {
		for _, h := range deadHosts {
			stem := ManualCertStem(h)
			if ep.ManualCertsDir != "" {
				crt := filepath.Join(ep.ManualCertsDir, stem+".crt")
				key := filepath.Join(ep.ManualCertsDir, stem+".key")
				if _, keep := keepCertFiles[filepath.Clean(crt)]; !keep {
					_ = os.Remove(crt)
				}
				if _, keep := keepCertFiles[filepath.Clean(key)]; !keep {
					_ = os.Remove(key)
				}
			}
			if ep.CertsDir != "" {
				if ep.Binary == "ferron" {
					_ = os.RemoveAll(filepath.Join(ep.CertsDir, stem))
				}
				if ep.Binary == "caddy" {
					acmeSiteDir := filepath.Join(ep.CertsDir, "certificates", "acme-v02.api.letsencrypt.org-directory", h)
					_ = os.RemoveAll(acmeSiteDir)
				}
			}
		}
		// Clean Angie ACME dirs keyed by zoneID nd_<hash> for removed primary entries.
		if ep.Binary == "angie" && ep.CertsDir != "" {
			for _, e := range removed {
				if _, stillUsed := keepHosts[e.Domain]; stillUsed {
					continue
				}
				zonePrefix := "nd_" + strings.TrimPrefix(e.ID, "m_")
				if matches, err := filepath.Glob(filepath.Join(ep.CertsDir, zonePrefix+"*")); err == nil {
					for _, m := range matches {
						_ = os.RemoveAll(m)
					}
				}
			}
		}
	}
}
