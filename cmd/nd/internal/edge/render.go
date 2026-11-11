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

// RenderGlobalOpts renders /etc/caddy/nd/global.opts (§5.2).
func RenderGlobalOpts(cfRanges []string) string {
	ranges := make([]string, 0, len(cfRanges))
	for _, r := range cfRanges {
		if p, err := netip.ParsePrefix(strings.TrimSpace(r)); err == nil {
			ranges = append(ranges, p.Masked().String())
		}
	}
	sort.Strings(ranges)
	if len(ranges) == 0 {
		ranges = append([]string(nil), DefaultCloudflareRanges...)
		sort.Strings(ranges)
	}

	var b strings.Builder
	b.WriteString("servers {\n")
	b.WriteString("\ttrusted_proxies static " + strings.Join(ranges, " ") + "\n")
	b.WriteString("\tclient_ip_headers CF-Connecting-IP X-Forwarded-For\n")
	b.WriteString("}\n")
	return b.String()
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
	needH1 := false
	needH2 := false
	for _, r := range in.Routes {
		if len(r.Hosts) == 0 {
			continue
		}
		if err := ValidateRouteID(r.ID); err != nil {
			return "", err
		}
		hosts := make([]string, len(r.Hosts))
		copy(hosts, r.Hosts)
		sort.Strings(hosts)
		rCopy := r
		rCopy.Hosts = hosts
		routes = append(routes, rCopy)
		if rCopy.Options.UpstreamH2 {
			needH2 = true
		} else {
			needH1 = true
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

	if needH1 {
		writeUpstreamSnippet(&b, "nd_up_"+pid8, upstreams, pemPath, caMissing, false)
		b.WriteByte('\n')
	}
	if needH2 {
		writeUpstreamSnippet(&b, "nd_up_"+pid8+"_h2", upstreams, pemPath, caMissing, true)
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
		if r.Options.UpstreamH2 {
			b.WriteString("\timport nd_up_" + pid8 + "_h2\n")
		} else {
			b.WriteString("\timport nd_up_" + pid8 + "\n")
		}
		b.WriteString("}\n")
	}

	return b.String(), nil
}

func writeUpstreamSnippet(b *strings.Builder, name string, upstreams []string, pemPath string, caMissing, h2 bool) {
	fmt.Fprintf(b, "(%s) {\n", name)
	fmt.Fprintf(b, "\treverse_proxy %s {\n", strings.Join(upstreams, " "))
	if len(upstreams) > 1 {
		b.WriteString("\t\tlb_policy first\n")
	}
	b.WriteString("\t\theader_up Host {host}\n")
	b.WriteString("\t\theader_up X-Real-IP {client_ip}\n")
	b.WriteString("\t\theader_up X-Forwarded-For {client_ip}\n")
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
