package edge

import (
	"fmt"
	"net"
	"net/netip"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	ManagedStartMarker = "# >>> nd-edge managed (do not edit inside markers) >>>"
	ManagedEndMarker   = "# <<< nd-edge managed <<<"
)

// RenderPanelInput describes the validated, claim-resolved, DNS-gated routes
// ready to be rendered for a single panel (<panel_id>.caddy).
type RenderPanelInput struct {
	PanelID      string
	PanelName    string
	ETag         string
	OriginIPs    []netip.Addr
	OriginPort   int
	CAStatus     string // CAStatusOK or CAStatusMissing
	OriginsLink  string // path to origins symlink, e.g. /etc/caddy/nd/origins
	Routes       []EdgeRoute
}

// RenderCaddyfileManagedRegion renders the nd-edge managed block (§5.1).
func RenderCaddyfileManagedRegion(paths Paths, adminAddr, acmeEmail string, panelsGlobDir, userGlobDir string) string {
	if strings.TrimSpace(adminAddr) == "" {
		adminAddr = DefaultAdminAddr
	}
	if panelsGlobDir == "" {
		panelsGlobDir = paths.PanelsLink
	}
	if userGlobDir == "" {
		userGlobDir = paths.UserCaddyDir
	}

	var b strings.Builder
	b.WriteString(ManagedStartMarker + "\n")
	b.WriteString("{\n")
	b.WriteString("\tadmin " + sanitizeSingleLine(adminAddr) + "\n")
	if email := sanitizeSingleLine(acmeEmail); email != "" {
		b.WriteString("\temail " + email + "\n")
	}
	b.WriteString("\timport " + filepath.ToSlash(paths.GlobalOpts) + "\n")
	b.WriteString("}\n")
	b.WriteString("import " + filepath.ToSlash(paths.CommonCaddy) + "\n")
	b.WriteString("import " + filepath.ToSlash(filepath.Join(userGlobDir, "*.caddy")) + "\n")
	b.WriteString("import " + filepath.ToSlash(filepath.Join(panelsGlobDir, "*.caddy")) + "\n")
	b.WriteString(ManagedEndMarker + "\n")
	return b.String()
}

// RenderGlobalOpts renders /etc/caddy/nd/global.opts (§5.2) with
// Cloudflare trusted_proxies, trusted_proxies_strict, and client_ip_headers.
func RenderGlobalOpts(cfRanges []string) string {
	return RenderGlobalOptsWithCF(cfRanges)
}

// RenderGlobalOptsWithCF renders a Caddy global `servers` block trusting cfRanges
// (falling back to DefaultCloudflareRanges when cfRanges is empty) with
// trusted_proxies_strict and Cloudflare client IP headers enabled.
func RenderGlobalOptsWithCF(cfRanges []string) string {
	ranges := normalizeCIDRList(cfRanges)
	if len(ranges) == 0 {
		ranges = normalizeCIDRList(DefaultCloudflareRanges)
	}

	var b strings.Builder
	b.WriteString("servers {\n")
	b.WriteString("\ttrusted_proxies static " + strings.Join(ranges, " ") + "\n")
	b.WriteString("\ttrusted_proxies_strict\n")
	b.WriteString("\tclient_ip_headers CF-Connecting-IP X-Forwarded-For\n")
	b.WriteString("}\n")
	return b.String()
}

// RenderGlobalOptsForConfig renders /etc/caddy/nd/global.opts based on whether
// any route on the Edge requires proxy trust (`anyTrustedProxy`) and whether
// the adopted Caddyfile already defined `trusted_proxies` (`cfg.TrustedProxies`).
func RenderGlobalOptsForConfig(cfg *Config, cfRanges []string, anyTrustedProxy bool) string {
	preserved := ""
	adoptedTP := false
	if cfg != nil {
		preserved = strings.TrimSpace(cfg.PreservedGlobal)
		adoptedTP = cfg.TrustedProxies
	}

	if !anyTrustedProxy {
		var b strings.Builder
		if adoptedTP {
			b.WriteString("# nd-edge skipped trusted_proxies (already defined in adopted Caddyfile)\n")
		} else {
			b.WriteString("# nd-edge: trusted_proxies disabled (no domains require proxy trust)\n")
		}
		if preserved != "" {
			b.WriteString(preserved + "\n")
		}
		return b.String()
	}

	if preserved != "" {
		if merged, ok := mergeTrustedProxiesIntoPreservedGlobal(preserved, cfRanges); ok {
			return merged
		}
	}

	out := RenderGlobalOptsWithCF(cfRanges)
	if preserved != "" {
		out += "\n" + preserved + "\n"
	}
	return out
}

func normalizeCIDRList(cidrs []string) []string {
	seen := make(map[string]struct{}, len(cidrs))
	out := make([]string, 0, len(cidrs))
	for _, r := range cidrs {
		p, err := netip.ParsePrefix(strings.TrimSpace(r))
		if err != nil {
			continue
		}
		s := p.Masked().String()
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func parsePrefixSet(cidrs []string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(cidrs))
	for _, r := range cidrs {
		if p, err := netip.ParsePrefix(strings.TrimSpace(r)); err == nil {
			out = append(out, p.Masked())
		}
	}
	return out
}

func isCloudflareEntry(tok string, cfPrefixes, defaultCFPrefixes []netip.Prefix) bool {
	if p, err := netip.ParsePrefix(tok); err == nil {
		pm := p.Masked()
		for _, c := range cfPrefixes {
			if c == pm || (c.Bits() <= pm.Bits() && c.Contains(pm.Addr())) {
				return true
			}
		}
		for _, c := range defaultCFPrefixes {
			if c == pm || (c.Bits() <= pm.Bits() && c.Contains(pm.Addr())) {
				return true
			}
		}
		return false
	}
	if a, err := netip.ParseAddr(tok); err == nil {
		au := a.Unmap()
		for _, c := range cfPrefixes {
			if c.Contains(au) {
				return true
			}
		}
		for _, c := range defaultCFPrefixes {
			if c.Contains(au) {
				return true
			}
		}
	}
	return false
}

// mergeTrustedProxiesIntoPreservedGlobal merges auto-updated Cloudflare CIDR
// ranges with any custom non-CF IPs already inside an adopted `servers { ... }`
// block, ensuring `trusted_proxies_strict` and `client_ip_headers` are present
// while leaving all other custom directives untouched.
func mergeTrustedProxiesIntoPreservedGlobal(preserved string, cfRanges []string) (string, bool) {
	lines := strings.Split(strings.ReplaceAll(preserved, "\r\n", "\n"), "\n")

	// Locate top-level `servers` blocks inside PreservedGlobal.
	type serverBlock struct {
		openIdx   int
		closeIdx  int
		hasTP     bool
		isBare    bool
	}
	var blocks []serverBlock
	depth := 0
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if depth == 0 && (trimmed == "servers {" || ((strings.HasPrefix(trimmed, "servers ") || strings.HasPrefix(trimmed, "servers\t")) && strings.HasSuffix(trimmed, "{"))) {
			closeIdx := i
			d := 0
			hasTP := false
			for j := i; j < len(lines); j++ {
				t := strings.TrimSpace(lines[j])
				if d == 1 && strings.HasPrefix(t, "trusted_proxies") && !strings.HasPrefix(t, "trusted_proxies_strict") {
					hasTP = true
				}
				d += strings.Count(lines[j], "{") - strings.Count(lines[j], "}")
				if d <= 0 && j > i {
					closeIdx = j
					break
				}
			}
			if closeIdx > i {
				blocks = append(blocks, serverBlock{
					openIdx:  i,
					closeIdx: closeIdx,
					hasTP:    hasTP,
					isBare:   trimmed == "servers {",
				})
				i = closeIdx
				continue
			}
		}
		depth += strings.Count(lines[i], "{") - strings.Count(lines[i], "}")
		if depth < 0 {
			depth = 0
		}
	}

	if len(blocks) == 0 {
		return "", false
	}

	target := blocks[0]
	for _, blk := range blocks {
		if blk.hasTP {
			target = blk
			break
		}
		if blk.isBare && !target.hasTP {
			target = blk
		}
	}

	cfList := normalizeCIDRList(cfRanges)
	if len(cfList) == 0 {
		cfList = normalizeCIDRList(DefaultCloudflareRanges)
	}
	cfPrefixes := parsePrefixSet(cfList)
	defaultCFPrefixes := parsePrefixSet(DefaultCloudflareRanges)

	var customIPs []string
	seenCustom := make(map[string]struct{})
	hasStrict := false
	hasClientIPHeaders := false

	// Inspect direct children (depth == 1) of the target `servers` block.
	d := 1
	for i := target.openIdx + 1; i < target.closeIdx; i++ {
		t := strings.TrimSpace(lines[i])
		if d == 1 {
			if t == "trusted_proxies_strict" || strings.HasPrefix(t, "trusted_proxies_strict ") || strings.HasPrefix(t, "trusted_proxies_strict\t") {
				hasStrict = true
			} else if t == "client_ip_headers" || strings.HasPrefix(t, "client_ip_headers ") || strings.HasPrefix(t, "client_ip_headers\t") {
				hasClientIPHeaders = true
			} else if strings.HasPrefix(t, "trusted_proxies ") || strings.HasPrefix(t, "trusted_proxies\t") {
				fields := strings.Fields(t)
				if len(fields) >= 2 && fields[1] == "static" {
					for _, rawTok := range fields[2:] {
						tok := strings.TrimSpace(rawTok)
						if tok == "" {
							continue
						}
						if isCloudflareEntry(tok, cfPrefixes, defaultCFPrefixes) {
							continue
						}
						var norm string
						if p, err := netip.ParsePrefix(tok); err == nil {
							norm = p.Masked().String()
						} else if a, err := netip.ParseAddr(tok); err == nil {
							norm = a.Unmap().String()
						} else {
							continue
						}
						if _, ok := seenCustom[norm]; !ok {
							seenCustom[norm] = struct{}{}
							customIPs = append(customIPs, norm)
						}
					}
				}
			}
		}
		d += strings.Count(lines[i], "{") - strings.Count(lines[i], "}")
	}
	sort.Strings(customIPs)

	mergedRanges := make([]string, 0, len(cfList)+len(customIPs))
	mergedRanges = append(mergedRanges, cfList...)
	mergedRanges = append(mergedRanges, customIPs...)

	var out []string
	out = append(out, lines[:target.openIdx+1]...)

	d = 1
	replacedTP := false
	for i := target.openIdx + 1; i < target.closeIdx; i++ {
		line := lines[i]
		t := strings.TrimSpace(line)
		if d == 1 {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			if indent == "" {
				indent = "\t"
			}
			if (strings.HasPrefix(t, "trusted_proxies ") || strings.HasPrefix(t, "trusted_proxies\t")) &&
				!strings.HasPrefix(t, "trusted_proxies_strict") {
				if !replacedTP {
					out = append(out, indent+"trusted_proxies static "+strings.Join(mergedRanges, " "))
					if !hasStrict {
						out = append(out, indent+"trusted_proxies_strict")
					}
					if !hasClientIPHeaders {
						out = append(out, indent+"client_ip_headers CF-Connecting-IP X-Forwarded-For")
					}
					replacedTP = true
				}
				d += strings.Count(line, "{") - strings.Count(line, "}")
				continue
			}
			if t == "client_ip_headers" || strings.HasPrefix(t, "client_ip_headers ") || strings.HasPrefix(t, "client_ip_headers\t") {
				out = append(out, indent+"client_ip_headers "+mergeClientIPHeaders(strings.Fields(t)[1:]))
				d += strings.Count(line, "{") - strings.Count(line, "}")
				continue
			}
		}
		out = append(out, line)
		d += strings.Count(line, "{") - strings.Count(line, "}")
	}

	if !replacedTP {
		out = append(out, "\ttrusted_proxies static "+strings.Join(mergedRanges, " "))
		if !hasStrict {
			out = append(out, "\ttrusted_proxies_strict")
		}
		if !hasClientIPHeaders {
			out = append(out, "\tclient_ip_headers CF-Connecting-IP X-Forwarded-For")
		}
	}

	out = append(out, lines[target.closeIdx:]...)
	res := strings.Join(out, "\n")
	if !strings.HasSuffix(res, "\n") {
		res += "\n"
	}
	return res, true
}

func mergeClientIPHeaders(existing []string) string {
	base := []string{"CF-Connecting-IP", "X-Forwarded-For"}
	seen := map[string]struct{}{
		"cf-connecting-ip": {},
		"x-forwarded-for":  {},
	}
	for _, h := range existing {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		low := strings.ToLower(h)
		if _, ok := seen[low]; ok {
			continue
		}
		seen[low] = struct{}{}
		base = append(base, h)
	}
	return strings.Join(base, " ")
}

// RenderCommonCaddy renders /etc/caddy/nd/common.caddy (§5.3).
func RenderCommonCaddy() string {
	return `(nd_common) {
	encode zstd gzip
	handle_errors 502 503 504 {
		header Cache-Control "no-store"
		respond "Origin is temporarily unavailable. Please retry shortly." 503
	}
}
`
}

// RenderPlaceholderCaddy renders 00-placeholder.caddy so glob imports never
// match zero files (E-CADDY-11).
func RenderPlaceholderCaddy() string {
	return "# Managed by nd edge. Placeholder so Caddy import glob always matches at least one file.\n"
}

// ShortPanelID returns a deterministic 8-character identifier safe for Caddy
// snippet names (e.g. "aaaaaaaa" from a UUIDv4).
func ShortPanelID(panelID string) string {
	var clean strings.Builder
	for i := 0; i < len(panelID); i++ {
		c := panelID[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			clean.WriteByte(c)
		} else if c >= 'A' && c <= 'Z' {
			clean.WriteByte(c + ('a' - 'A'))
		}
		if clean.Len() == 8 {
			break
		}
	}
	if clean.Len() == 0 {
		return "panel000"
	}
	return clean.String()
}

// RenderPanelCaddy deterministically renders <panel_id>.caddy (§5.4).
// All routes are sorted by ID and all hosts within each route are sorted
// lexicographically so unchanged inputs always produce byte-identical output.
func RenderPanelCaddy(in RenderPanelInput) (string, error) {
	if err := ValidatePanelID(in.PanelID); err != nil {
		return "", err
	}
	originsLink := in.OriginsLink
	if originsLink == "" {
		originsLink = "/etc/caddy/nd/origins"
	}
	port := in.OriginPort
	if port <= 0 || port > 65535 {
		port = 443
	}

	panelLabel := sanitizeCommentValue(in.PanelName)
	if panelLabel == "" {
		panelLabel = in.PanelID
	}
	etagLabel := sanitizeTokenValue(in.ETag)
	pid8 := ShortPanelID(in.PanelID)

	// Normalize, sort, and deduplicate routes & hosts deterministically.
	routes := make([]EdgeRoute, 0, len(in.Routes))
	var needH1Direct, needH1TP, needH2Direct, needH2TP bool
	for _, r := range in.Routes {
		if len(r.Hosts) == 0 {
			continue
		}
		if err := ValidateRouteID(r.ID); err != nil {
			return "", err
		}
		hosts := make([]string, 0, len(r.Hosts))
		for _, rawHost := range r.Hosts {
			norm, verr := NormalizeAndValidateHost(rawHost, "")
			if verr != nil {
				return "", fmt.Errorf("route %q host %q rejected: %w", r.ID, rawHost, verr)
			}
			hosts = append(hosts, norm)
		}
		sort.Strings(hosts)
		rCopy := r
		rCopy.Hosts = hosts
		routes = append(routes, rCopy)
		switch {
		case rCopy.Options.UpstreamH2 && rCopy.TrustedProxy:
			needH2TP = true
		case rCopy.Options.UpstreamH2 && !rCopy.TrustedProxy:
			needH2Direct = true
		case !rCopy.Options.UpstreamH2 && rCopy.TrustedProxy:
			needH1TP = true
		default:
			needH1Direct = true
		}
	}
	sort.Slice(routes, func(i, j int) bool {
		return routes[i].ID < routes[j].ID
	})

	var b strings.Builder
	fmt.Fprintf(&b, "# Managed by nd edge for panel %q (%s). DO NOT EDIT. etag=%s\n", panelLabel, in.PanelID, etagLabel)

	if len(routes) == 0 || len(in.OriginIPs) == 0 {
		return b.String(), nil
	}

	// Build sorted unique origin upstream targets.
	upstreams := make([]string, 0, len(in.OriginIPs))
	seenIP := make(map[netip.Addr]struct{}, len(in.OriginIPs))
	for _, addr := range in.OriginIPs {
		a := addr.Unmap()
		if _, ok := seenIP[a]; ok {
			continue
		}
		seenIP[a] = struct{}{}
		upstreams = append(upstreams, "https://"+net.JoinHostPort(a.String(), strconv.Itoa(port)))
	}
	sort.Strings(upstreams)

	pemPath := filepath.ToSlash(filepath.Join(originsLink, in.PanelID+".pem"))
	caMissing := in.CAStatus != CAStatusOK

	if needH1Direct {
		writeUpstreamSnippet(&b, "nd_up_"+pid8, upstreams, pemPath, caMissing, false, false)
		b.WriteByte('\n')
	}
	if needH1TP {
		writeUpstreamSnippet(&b, "nd_up_"+pid8+"_tp", upstreams, pemPath, caMissing, false, true)
		b.WriteByte('\n')
	}
	if needH2Direct {
		writeUpstreamSnippet(&b, "nd_up_"+pid8+"_h2", upstreams, pemPath, caMissing, true, false)
		b.WriteByte('\n')
	}
	if needH2TP {
		writeUpstreamSnippet(&b, "nd_up_"+pid8+"_h2_tp", upstreams, pemPath, caMissing, true, true)
		b.WriteByte('\n')
	}

	for i, r := range routes {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.Join(r.Hosts, ", ") + " {\n")
		b.WriteString("\timport nd_common\n")
		if r.Options.MaxBodyMB > 0 {
			fmt.Fprintf(&b, "\trequest_body {\n\t\tmax_size %dMB\n\t}\n", r.Options.MaxBodyMB)
		}
		snippetName := "nd_up_" + pid8
		if r.Options.UpstreamH2 {
			snippetName += "_h2"
		}
		if r.TrustedProxy {
			snippetName += "_tp"
		}
		b.WriteString("\timport " + snippetName + "\n")
		b.WriteString("}\n")
	}

	return b.String(), nil
}

func writeUpstreamSnippet(b *strings.Builder, name string, upstreams []string, pemPath string, caMissing, h2, trustedProxy bool) {
	fmt.Fprintf(b, "(%s) {\n", name)
	fmt.Fprintf(b, "\treverse_proxy %s {\n", strings.Join(upstreams, " "))
	if len(upstreams) > 1 {
		b.WriteString("\t\tlb_policy first\n")
	}
	b.WriteString("\t\theader_up Host {host}\n")
	if trustedProxy {
		b.WriteString("\t\theader_up X-Real-IP {client_ip}\n")
		b.WriteString("\t\theader_up X-Forwarded-For {client_ip}\n")
	} else {
		b.WriteString("\t\theader_up X-Real-IP {remote_host}\n")
		b.WriteString("\t\theader_up X-Forwarded-For {remote_host}\n")
		b.WriteString("\t\theader_up -CF-Connecting-IP\n")
	}
	b.WriteString("\t\theader_up X-Forwarded-Proto https\n")
	b.WriteString("\t\tstream_close_delay 10m\n")
	b.WriteString("\t\tflush_interval -1\n")
	b.WriteString("\t\tlb_try_duration 5s\n")
	b.WriteString("\t\tlb_try_interval 250ms\n")
	b.WriteString("\t\ttransport http {\n")
	if caMissing {
		b.WriteString("\t\t\ttls_insecure_skip_verify\n")
	} else {
		b.WriteString("\t\t\ttls_trust_pool file " + pemPath + "\n")
	}
	b.WriteString("\t\t\ttls_server_name {host}\n")
	if h2 {
		b.WriteString("\t\t\tversions h2 1.1\n")
	} else {
		b.WriteString("\t\t\tversions 1.1\n")
	}
	b.WriteString("\t\t\tdial_timeout 10s\n")
	b.WriteString("\t\t\tresponse_header_timeout 600s\n")
	b.WriteString("\t\t\tkeepalive 90s\n")
	b.WriteString("\t\t\tkeepalive_idle_conns 256\n")
	b.WriteString("\t\t\tkeepalive_idle_conns_per_host 64\n")
	b.WriteString("\t\t}\n")
	b.WriteString("\t}\n")
	b.WriteString("}\n")
}

func sanitizeSingleLine(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x21 && c < 0x7f && c != '{' && c != '}' && c != ';' && c != '"' && c != '\'' {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func sanitizeCommentValue(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x20 && r != 0x7f && r != '"' && r != '\\' {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func sanitizeTokenValue(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' {
			b.WriteByte(c)
		}
	}
	return b.String()
}
