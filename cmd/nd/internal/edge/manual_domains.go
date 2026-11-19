package edge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/idna"
)

const (
	ManualDomainsSchema        = 1
	ManualTrashRetention       = 30 * 24 * time.Hour
	DefaultManualPruneGrace    = 7 * 24 * time.Hour
	DefaultManualTimeoutSec    = 300
	DefaultManualConnectSec    = 10
	DefaultManualKeepalive     = 32

	ManualTLSAuto   = "auto"
	ManualTLSNone   = "none"
	ManualTLSManual = "manual"

	ManualLBRoundRobin = "round_robin"
	ManualLBLeastConn  = "least_conn"
	ManualLBIPHash     = "ip_hash"

	ManualRedirectNone   = ""
	ManualRedirectToApex = "to-apex"
	ManualRedirectToWWW  = "to-www"

	ManualHostHeaderOriginal = "original"
	ManualHostHeaderUpstream = "upstream"

	ManualRealIPInherit    = ""
	ManualRealIPCloudflare = "cloudflare"
	ManualRealIPNone       = "none"
)

var (
	headerNameRe = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
	customHostRe = regexp.MustCompile(`^[A-Za-z0-9.-]+(:[0-9]{1,5})?$`)
)

// ManualDomainsFile persists user-managed proxy sites in /etc/nd-edge/domains.json.
type ManualDomainsFile struct {
	Schema  int                 `json:"schema"`
	Domains []ManualDomainEntry `json:"domains"`
}

// ManualDomainEntry represents a single operator-added reverse-proxy site.
type ManualDomainEntry struct {
	ID                string            `json:"id"`
	Domain            string            `json:"domain"`
	Aliases           []string          `json:"aliases,omitempty"`
	WWW               bool              `json:"www,omitempty"`
	RedirectWWW       string            `json:"redirect_www,omitempty"`
	Upstreams         []ManualUpstream  `json:"upstreams"`
	LB                string            `json:"lb,omitempty"`
	TLS               string            `json:"tls"`
	ForceHTTPS        *bool             `json:"force_https,omitempty"`
	CertFile          string            `json:"cert_file,omitempty"`
	KeyFile           string            `json:"key_file,omitempty"`
	Origin            ManualOriginOpts  `json:"origin,omitempty"`
	Websocket         *bool             `json:"websocket,omitempty"`
	MaxBodyBytes      int64             `json:"max_body_bytes,omitempty"`
	TimeoutSec        int               `json:"timeout_sec,omitempty"`
	ConnectTimeoutSec int               `json:"connect_timeout_sec,omitempty"`
	RealIP            string            `json:"real_ip,omitempty"`
	HeadersUp         []ManualHeader    `json:"headers_up,omitempty"`
	HeadersDown       []ManualHeader    `json:"headers_down,omitempty"`
	Keepalive         *int              `json:"keepalive,omitempty"`
	PathRoutes        []ManualPathRoute `json:"path_routes,omitempty"`
	Enabled           bool              `json:"enabled"`
	Note              string            `json:"note,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

// ManualPathRoute reserves forward-compatible per-path routing fields (§3.2).
type ManualPathRoute struct {
	Path      string           `json:"path"`
	Upstreams []ManualUpstream `json:"upstreams,omitempty"`
}

// ManualUpstream describes a single normalized backend endpoint.
type ManualUpstream struct {
	Scheme string `json:"scheme"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
}

// ManualOriginOpts controls Shield-to-origin HTTP/TLS behavior.
type ManualOriginOpts struct {
	NoVerify   bool   `json:"no_verify,omitempty"`
	SNI        string `json:"sni,omitempty"`
	HostHeader string `json:"host_header,omitempty"`
}

// ManualHeader stores an HTTP header override pair.
type ManualHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ManualTrashFile stores soft-deleted manual domains in /etc/nd-edge/domains.trash.json.
type ManualTrashFile struct {
	Schema  int                `json:"schema"`
	Entries []ManualTrashEntry `json:"entries"`
}

// ManualTrashEntry wraps a removed or pruned ManualDomainEntry for restore.
type ManualTrashEntry struct {
	Entry     ManualDomainEntry `json:"entry"`
	RemovedAt time.Time         `json:"removed_at"`
	Reason    string            `json:"reason"`
}

// ManualEntryID returns the deterministic identifier m_<12 hex chars> for domain.
func ManualEntryID(domain string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(domain))))
	return "m_" + hex.EncodeToString(sum[:6])
}

// URL returns scheme://host:port with IPv6 brackets when needed.
func (u ManualUpstream) URL() string {
	return u.Scheme + "://" + u.HostPort()
}

// HostPort returns host:port with IPv6 brackets when needed.
func (u ManualUpstream) HostPort() string {
	return net.JoinHostPort(u.Host, strconv.Itoa(u.Port))
}

// EffectiveForceHTTPS returns true when HTTPS is enabled and ForceHTTPS was not set false.
func (e ManualDomainEntry) EffectiveForceHTTPS() bool {
	if e.TLS == ManualTLSNone {
		return false
	}
	if e.ForceHTTPS == nil {
		return true
	}
	return *e.ForceHTTPS
}

// EffectiveWebsocket returns true unless Websocket was explicitly disabled.
func (e ManualDomainEntry) EffectiveWebsocket() bool {
	if e.Websocket == nil {
		return true
	}
	return *e.Websocket
}

// EffectiveTimeoutSec returns TimeoutSec or the 300s default.
func (e ManualDomainEntry) EffectiveTimeoutSec() int {
	if e.TimeoutSec <= 0 {
		return DefaultManualTimeoutSec
	}
	return e.TimeoutSec
}

// EffectiveConnectTimeoutSec returns ConnectTimeoutSec or the 10s default.
func (e ManualDomainEntry) EffectiveConnectTimeoutSec() int {
	if e.ConnectTimeoutSec <= 0 {
		return DefaultManualConnectSec
	}
	return e.ConnectTimeoutSec
}

// EffectiveKeepalive returns Keepalive or the 32 default (0 means disabled).
func (e ManualDomainEntry) EffectiveKeepalive() int {
	if e.Keepalive == nil {
		return DefaultManualKeepalive
	}
	if *e.Keepalive < 0 {
		return 0
	}
	return *e.Keepalive
}

// EffectiveLB returns the normalized load-balancer policy.
func (e ManualDomainEntry) EffectiveLB() string {
	if e.LB == "" {
		return ManualLBRoundRobin
	}
	return e.LB
}

// EffectiveHostHeader returns the origin Host header mode ("original", "upstream", or explicit value).
func (e ManualDomainEntry) EffectiveHostHeader() string {
	if e.Origin.HostHeader == "" {
		return ManualHostHeaderOriginal
	}
	return e.Origin.HostHeader
}

// EffectiveRealIP resolves real_ip against Shield trusted-proxy settings.
func (e ManualDomainEntry) EffectiveRealIP(shieldTrustsCF bool) string {
	switch e.RealIP {
	case ManualRealIPCloudflare, ManualRealIPNone:
		return e.RealIP
	default:
		if shieldTrustsCF {
			return ManualRealIPCloudflare
		}
		return ManualRealIPNone
	}
}

// ResolveWWWHosts returns the apex/www pair and whether the primary domain is wildcard.
func ResolveWWWHosts(domain string) (apexHost, wwwHost string, isWildcard bool) {
	if strings.HasPrefix(domain, "*.") {
		return domain, "", true
	}
	if strings.HasPrefix(domain, "www.") {
		apex := strings.TrimPrefix(domain, "www.")
		return apex, domain, false
	}
	return domain, "www." + domain, false
}

// PrimaryServedHosts returns the hosts that terminate proxy traffic (not the redirected twin).
func (e ManualDomainEntry) PrimaryServedHosts() []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(h string) {
		if h == "" {
			return
		}
		if _, ok := seen[h]; !ok {
			seen[h] = struct{}{}
			out = append(out, h)
		}
	}

	apex, www, isWildcard := ResolveWWWHosts(e.Domain)
	switch e.RedirectWWW {
	case ManualRedirectToApex:
		if isWildcard {
			add(e.Domain)
		} else {
			add(apex)
		}
	case ManualRedirectToWWW:
		if isWildcard {
			add(e.Domain)
		} else {
			add(www)
		}
	default:
		add(e.Domain)
		if e.WWW && !isWildcard {
			add(www)
		}
	}
	for _, a := range e.Aliases {
		add(a)
	}
	return out
}

// RedirectPair returns (fromHost, toHost, true) when RedirectWWW is active.
func (e ManualDomainEntry) RedirectPair() (fromHost, toHost string, ok bool) {
	apex, www, isWildcard := ResolveWWWHosts(e.Domain)
	if isWildcard || e.RedirectWWW == "" {
		return "", "", false
	}
	switch e.RedirectWWW {
	case ManualRedirectToApex:
		return www, apex, true
	case ManualRedirectToWWW:
		return apex, www, true
	default:
		return "", "", false
	}
}

// AllHosts returns every host claimed/handled by this entry in deterministic order.
func (e ManualDomainEntry) AllHosts() []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(h string) {
		if h == "" {
			return
		}
		if _, ok := seen[h]; !ok {
			seen[h] = struct{}{}
			out = append(out, h)
		}
	}
	for _, h := range e.PrimaryServedHosts() {
		add(h)
	}
	if from, _, ok := e.RedirectPair(); ok {
		add(from)
	}
	return out
}

// MatchesHost reports whether host matches Domain, any Alias, or the www variant.
func (e ManualDomainEntry) MatchesHost(host string) bool {
	norm := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if e.Domain == norm {
		return true
	}
	for _, h := range e.AllHosts() {
		if h == norm {
			return true
		}
	}
	return false
}

// BoolPtr returns a pointer to b.
func BoolPtr(b bool) *bool { return &b }

// IntPtr returns a pointer to n.
func IntPtr(n int) *int { return &n }

// NormalizeManualDomain normalizes and validates a domain name for manual sites.
// Wildcard hosts (*.example.com) are accepted only when tlsMode is manual or none.
func NormalizeManualDomain(raw, tlsMode, shieldHostname string) (string, error) {
	trimmed := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
	if strings.Contains(trimmed, "*") {
		if !strings.HasPrefix(trimmed, "*.") || strings.Count(trimmed, "*") > 1 {
			return "", fmt.Errorf("invalid wildcard domain %q: only leading *.example.com is supported", raw)
		}
		if tlsMode != ManualTLSManual && tlsMode != ManualTLSNone {
			return "", fmt.Errorf("wildcard domain %q requires --tls manual or --tls none (no DNS-01 support)", trimmed)
		}
		base := strings.TrimPrefix(trimmed, "*.")
		normBase, verr := NormalizeAndValidateHost(base, shieldHostname)
		if verr != nil {
			return "", fmt.Errorf("invalid wildcard domain %q: %s", raw, verr.Detail)
		}
		if len(normBase)+2 > 253 {
			return "", fmt.Errorf("domain %q exceeds maximum length", raw)
		}
		return "*." + normBase, nil
	}
	norm, verr := NormalizeAndValidateHost(trimmed, shieldHostname)
	if verr != nil {
		return "", fmt.Errorf("invalid domain %q: %s", raw, verr.Detail)
	}
	return norm, nil
}

// ParseManualUpstreamList parses comma-separated upstreams per §3.1.
func ParseManualUpstreamList(raw, originSchemeOverride string) ([]ManualUpstream, error) {
	parts := strings.Split(raw, ",")
	if len(parts) == 0 {
		return nil, fmt.Errorf("at least one upstream is required")
	}
	out := make([]ManualUpstream, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil, fmt.Errorf("empty upstream entry in %q", raw)
		}
		u, err := ParseManualUpstream(p, originSchemeOverride)
		if err != nil {
			return nil, err
		}
		key := u.URL()
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("duplicate upstream %q", key)
		}
		seen[key] = struct{}{}
		out = append(out, u)
	}
	return out, nil
}

// ParseManualUpstream parses a single upstream expression per §3.1.
func ParseManualUpstream(raw, originSchemeOverride string) (ManualUpstream, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ManualUpstream{}, fmt.Errorf("upstream cannot be empty")
	}
	if hasUnsafeConfigChars(s) {
		return ManualUpstream{}, fmt.Errorf("upstream %q contains forbidden characters", raw)
	}
	if originSchemeOverride != "" && originSchemeOverride != "http" && originSchemeOverride != "https" {
		return ManualUpstream{}, fmt.Errorf("invalid origin scheme override %q", originSchemeOverride)
	}

	var scheme string
	var explicitScheme bool
	var host string
	var port int
	var explicitPort bool

	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil {
			return ManualUpstream{}, fmt.Errorf("invalid upstream %q: %w", raw, err)
		}
		scheme = strings.ToLower(u.Scheme)
		explicitScheme = true
		if scheme != "http" && scheme != "https" {
			return ManualUpstream{}, fmt.Errorf("unsupported upstream scheme %q (must be http or https)", u.Scheme)
		}
		if u.User != nil {
			return ManualUpstream{}, fmt.Errorf("upstream %q must not contain userinfo", raw)
		}
		if u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return ManualUpstream{}, fmt.Errorf("upstream %q must not contain a path, query, or fragment", raw)
		}
		host = u.Hostname()
		if p := u.Port(); p != "" {
			parsedPort, err := strconv.Atoi(p)
			if err != nil || parsedPort < 1 || parsedPort > 65535 {
				return ManualUpstream{}, fmt.Errorf("invalid upstream port %q", p)
			}
			port = parsedPort
			explicitPort = true
		}
	} else {
		if strings.ContainsAny(s, "/?#") {
			return ManualUpstream{}, fmt.Errorf("upstream %q must not contain a path, query, or fragment", raw)
		}
		var err error
		host, port, explicitPort, err = splitBareHostPort(s)
		if err != nil {
			return ManualUpstream{}, fmt.Errorf("invalid upstream %q: %w", raw, err)
		}
	}

	normHost, err := normalizeUpstreamHost(host)
	if err != nil {
		return ManualUpstream{}, fmt.Errorf("invalid upstream host %q: %w", host, err)
	}

	if !explicitScheme {
		if explicitPort && port == 443 {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	if originSchemeOverride != "" {
		scheme = originSchemeOverride
	}
	if !explicitPort {
		if scheme == "https" {
			port = 443
		} else {
			port = 80
		}
	}

	return ManualUpstream{
		Scheme: scheme,
		Host:   normHost,
		Port:   port,
	}, nil
}

func splitBareHostPort(s string) (host string, port int, explicitPort bool, err error) {
	// Bare bracketed IPv6 literal without port, e.g. [2001:db8::1]
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		inner := strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
		if _, parseErr := netip.ParseAddr(inner); parseErr == nil {
			return inner, 0, false, nil
		}
	}
	// Bare unbracketed IPv6 literal, e.g. 2001:db8::1
	if addr, parseErr := netip.ParseAddr(s); parseErr == nil && addr.Is6() {
		return addr.String(), 0, false, nil
	}
	if strings.Contains(s, ":") {
		h, p, splitErr := net.SplitHostPort(s)
		if splitErr != nil {
			return "", 0, false, splitErr
		}
		if strings.Contains(s, "]:") || strings.Count(s, ":") == 1 {
			if p == "" {
				return "", 0, false, fmt.Errorf("missing port after ':'")
			}
			parsedPort, convErr := strconv.Atoi(p)
			if convErr != nil || parsedPort < 1 || parsedPort > 65535 {
				return "", 0, false, fmt.Errorf("port %q out of range (1..65535)", p)
			}
			return h, parsedPort, true, nil
		}
	}
	return s, 0, false, nil
}

func normalizeUpstreamHost(raw string) (string, error) {
	trimmed := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
	if trimmed == "" {
		return "", fmt.Errorf("empty host")
	}
	if addr, err := netip.ParseAddr(trimmed); err == nil {
		if addr.IsUnspecified() {
			return "", fmt.Errorf("unspecified IP address %q is not allowed", trimmed)
		}
		return addr.String(), nil
	}
	asciiHost, err := idna.Lookup.ToASCII(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid IDN hostname: %w", err)
	}
	asciiHost = strings.TrimSuffix(strings.ToLower(asciiHost), ".")
	if len(asciiHost) == 0 || len(asciiHost) > 253 {
		return "", fmt.Errorf("hostname length out of range")
	}
	if asciiHost == "localhost" {
		return asciiHost, nil
	}
	for i := 0; i < len(asciiHost); i++ {
		c := asciiHost[i]
		ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' || c == '-'
		if !ok {
			return "", fmt.Errorf("host contains invalid character %q", c)
		}
	}
	// Single-label internal service names like "backend" or multi-label DNS names.
	labels := strings.Split(asciiHost, ".")
	for _, lbl := range labels {
		if lbl == "" || len(lbl) > 63 || strings.HasPrefix(lbl, "-") || strings.HasSuffix(lbl, "-") {
			return "", fmt.Errorf("invalid hostname label %q", lbl)
		}
	}
	return asciiHost, nil
}

func hasUnsafeConfigChars(s string) bool {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b <= 0x20 || b == 0x7f || b == '{' || b == '}' || b == '"' || b == '\'' || b == ';' || b == '\\' {
			return true
		}
	}
	return false
}

// ParseByteSize parses sizes like "0", "100m", "2g", "512k", "1048576".
func ParseByteSize(raw string) (int64, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" || s == "0" || s == "unlimited" {
		return 0, nil
	}
	var mult int64 = 1
	numPart := s
	switch {
	case strings.HasSuffix(s, "gb"):
		mult = 1024 * 1024 * 1024
		numPart = strings.TrimSuffix(s, "gb")
	case strings.HasSuffix(s, "g"):
		mult = 1024 * 1024 * 1024
		numPart = strings.TrimSuffix(s, "g")
	case strings.HasSuffix(s, "mb"):
		mult = 1024 * 1024
		numPart = strings.TrimSuffix(s, "mb")
	case strings.HasSuffix(s, "m"):
		mult = 1024 * 1024
		numPart = strings.TrimSuffix(s, "m")
	case strings.HasSuffix(s, "kb"):
		mult = 1024
		numPart = strings.TrimSuffix(s, "kb")
	case strings.HasSuffix(s, "k"):
		mult = 1024
		numPart = strings.TrimSuffix(s, "k")
	case strings.HasSuffix(s, "b"):
		numPart = strings.TrimSuffix(s, "b")
	}
	n, err := strconv.ParseInt(strings.TrimSpace(numPart), 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid size %q (expected e.g. 100m, 2g, 512k, or 0)", raw)
	}
	if n > 0 && mult > 1 && n > (1<<62)/mult {
		return 0, fmt.Errorf("size %q overflows int64", raw)
	}
	return n * mult, nil
}

// ParsePositiveDurationSec parses "300s", "5m", "1h", or integer seconds.
func ParsePositiveDurationSec(raw string) (int, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("duration cannot be empty")
	}
	if secs, err := strconv.Atoi(s); err == nil {
		if secs <= 0 || secs > 86400 {
			return 0, fmt.Errorf("duration %q must be between 1s and 24h", raw)
		}
		return secs, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", raw, err)
	}
	secs := int(d.Seconds())
	if d > 0 && secs == 0 {
		secs = 1
	}
	if secs <= 0 || secs > 86400 {
		return 0, fmt.Errorf("duration %q must be between 1s and 24h", raw)
	}
	return secs, nil
}

// ParseHeaderPair parses and validates a K=V HTTP header directive.
func ParseHeaderPair(raw string) (ManualHeader, error) {
	name, val, ok := strings.Cut(raw, "=")
	if !ok {
		return ManualHeader{}, fmt.Errorf("header %q must be in K=V format", raw)
	}
	name = strings.TrimSpace(name)
	val = strings.TrimSpace(val)
	if !headerNameRe.MatchString(name) {
		return ManualHeader{}, fmt.Errorf("invalid header name %q", name)
	}
	if strings.ContainsAny(val, "\r\n\x00\"\\{};") {
		return ManualHeader{}, fmt.Errorf("header value for %q contains unsafe characters", name)
	}
	return ManualHeader{Name: name, Value: val}, nil
}

// NormalizeAndValidateEntry validates and normalizes an entire ManualDomainEntry.
func NormalizeAndValidateEntry(e *ManualDomainEntry, shieldHostname string) error {
	if e == nil {
		return fmt.Errorf("manual domain entry cannot be nil")
	}
	if e.TLS == "" {
		e.TLS = ManualTLSAuto
	}
	switch e.TLS {
	case ManualTLSAuto, ManualTLSNone, ManualTLSManual:
	default:
		return fmt.Errorf("invalid --tls %q (must be auto, none, or manual)", e.TLS)
	}

	normDomain, err := NormalizeManualDomain(e.Domain, e.TLS, shieldHostname)
	if err != nil {
		return err
	}
	e.Domain = normDomain
	if e.ID == "" {
		e.ID = ManualEntryID(normDomain)
	}

	isWildcard := strings.HasPrefix(normDomain, "*.")
	if isWildcard && (e.WWW || e.RedirectWWW != "") {
		return fmt.Errorf("--www and --redirect-www cannot be combined with wildcard domain %q", normDomain)
	}
	switch e.RedirectWWW {
	case ManualRedirectNone, ManualRedirectToApex, ManualRedirectToWWW:
	default:
		return fmt.Errorf("invalid --redirect-www %q (must be to-apex or to-www)", e.RedirectWWW)
	}

	seenHosts := make(map[string]struct{})
	for _, h := range []string{normDomain} {
		seenHosts[h] = struct{}{}
	}
	if e.WWW || e.RedirectWWW != "" {
		apex, www, _ := ResolveWWWHosts(normDomain)
		seenHosts[apex] = struct{}{}
		seenHosts[www] = struct{}{}
	}

	var normAliases []string
	for _, rawAlias := range e.Aliases {
		a, err := NormalizeManualDomain(rawAlias, e.TLS, shieldHostname)
		if err != nil {
			return fmt.Errorf("alias %q: %w", rawAlias, err)
		}
		if _, dup := seenHosts[a]; dup {
			continue
		}
		seenHosts[a] = struct{}{}
		normAliases = append(normAliases, a)
	}
	sort.Strings(normAliases)
	e.Aliases = normAliases

	if len(e.Upstreams) == 0 {
		return fmt.Errorf("at least one upstream is required")
	}
	for i := range e.Upstreams {
		u := e.Upstreams[i]
		reParsed, err := ParseManualUpstream(u.URL(), "")
		if err != nil {
			return err
		}
		if i > 0 && reParsed.Scheme != e.Upstreams[0].Scheme {
			return fmt.Errorf("cannot mix http and https upstreams (%q and %q) in a single entry", e.Upstreams[0].URL(), reParsed.URL())
		}
		e.Upstreams[i] = reParsed
	}

	if e.LB == "" {
		e.LB = ManualLBRoundRobin
	}
	switch e.LB {
	case ManualLBRoundRobin, ManualLBLeastConn, ManualLBIPHash:
	default:
		return fmt.Errorf("invalid --lb %q (must be round_robin, least_conn, or ip_hash)", e.LB)
	}

	if e.TLS == ManualTLSManual {
		if strings.TrimSpace(e.CertFile) == "" || strings.TrimSpace(e.KeyFile) == "" {
			return fmt.Errorf("--tls manual requires both --cert and --key")
		}
	}

	if e.Origin.SNI != "" {
		if hasUnsafeConfigChars(e.Origin.SNI) || strings.ContainsAny(e.Origin.SNI, "/:") {
			return fmt.Errorf("invalid --sni %q", e.Origin.SNI)
		}
		if e.Origin.SNI != "$host" {
			asciiSNI, err := idna.Lookup.ToASCII(strings.TrimSuffix(strings.ToLower(strings.TrimSpace(e.Origin.SNI)), "."))
			if err != nil || !customHostRe.MatchString(asciiSNI) {
				return fmt.Errorf("invalid --sni %q", e.Origin.SNI)
			}
			e.Origin.SNI = asciiSNI
		}
	}

	if e.Origin.HostHeader == "" {
		e.Origin.HostHeader = ManualHostHeaderOriginal
	}
	if e.Origin.HostHeader != ManualHostHeaderOriginal && e.Origin.HostHeader != ManualHostHeaderUpstream {
		hh := strings.TrimSpace(e.Origin.HostHeader)
		if hasUnsafeConfigChars(hh) || !customHostRe.MatchString(hh) {
			return fmt.Errorf("invalid --host-header %q (must be original, upstream, or host[:port])", e.Origin.HostHeader)
		}
		e.Origin.HostHeader = hh
	}

	if e.MaxBodyBytes < 0 {
		return fmt.Errorf("--max-body cannot be negative")
	}
	if e.TimeoutSec <= 0 {
		e.TimeoutSec = DefaultManualTimeoutSec
	}
	if e.ConnectTimeoutSec <= 0 {
		e.ConnectTimeoutSec = DefaultManualConnectSec
	}
	switch e.RealIP {
	case ManualRealIPInherit, ManualRealIPCloudflare, ManualRealIPNone:
	default:
		return fmt.Errorf("invalid --real-ip %q (must be cloudflare or none)", e.RealIP)
	}

	for i, h := range e.HeadersUp {
		clean, err := ParseHeaderPair(h.Name + "=" + h.Value)
		if err != nil {
			return err
		}
		e.HeadersUp[i] = clean
	}
	for i, h := range e.HeadersDown {
		clean, err := ParseHeaderPair(h.Name + "=" + h.Value)
		if err != nil {
			return err
		}
		e.HeadersDown[i] = clean
	}
	if e.Keepalive != nil && (*e.Keepalive < 0 || *e.Keepalive > 4096) {
		return fmt.Errorf("--keepalive must be between 0 and 4096")
	}
	if e.Note != "" {
		e.Note = sanitizeSingleLine(e.Note)
	}
	return nil
}
