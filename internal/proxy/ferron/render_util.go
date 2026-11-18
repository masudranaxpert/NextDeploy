package ferron

import (
	"fmt"
	"net"
	"net/url"
	"path"
	"regexp"
	"strings"

	"panel/internal/proxy"
	"panel/internal/proxy/pki"
)

func quoteString(s string) string {
	clean := strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == 0 {
			return -1
		}
		return r
	}, s)
	var b strings.Builder
	b.Grow(len(clean) + 2)
	b.WriteByte('"')
	for i := 0; i < len(clean); i++ {
		switch clean[i] {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		default:
			b.WriteByte(clean[i])
		}
	}
	b.WriteByte('"')
	return b.String()
}

func rawString(pattern string) string {
	if strings.Contains(pattern, `"`) {
		return quoteString(pattern)
	}
	return `r"` + pattern + `"`
}

func siteSlug(id string, idx int) string {
	raw := strings.ToLower(strings.TrimSpace(id))
	var b strings.Builder
	b.Grow(len(raw) + 8)
	prevUnderscore := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteByte(c)
			prevUnderscore = false
		} else if !prevUnderscore && b.Len() > 0 {
			b.WriteByte('_')
			prevUnderscore = true
		}
	}
	slug := strings.Trim(b.String(), "_")
	if slug == "" {
		return fmt.Sprintf("site_%d", idx)
	}
	return fmt.Sprintf("%03d_%s", idx, slug)
}

func siteFilename(id string, idx int) string {
	return "sites/" + strings.ReplaceAll(siteSlug(id, idx), "_", "-") + ".conf"
}

func formatHostsHeader(s proxy.Site) (string, error) {
	if len(s.Hosts) == 0 {
		return "", fmt.Errorf("ferron: site %q has no hosts", s.ID)
	}
	parts := make([]string, 0, len(s.Hosts))
	seen := make(map[string]bool, len(s.Hosts))
	for _, raw := range s.Hosts {
		h := strings.TrimSpace(proxy.CleanQuotedValue(raw))
		if h == "" {
			continue
		}
		key := strings.ToLower(h)
		if seen[key] {
			continue
		}
		seen[key] = true
		if s.TLS == proxy.TLSNone {
			if strings.HasPrefix(key, "http ") {
				parts = append(parts, strings.TrimSpace(h[len("http "):]))
			} else {
				parts = append(parts, h+":80")
			}
		} else {
			// Explicit :443. A port-less host with an explicit `tls { … }` block makes Ferron
			// 3.0.0-rc.11 terminate TLS on the default HTTP port (80) too, which breaks every
			// plain-HTTP site and the ACME HTTP-01 challenge. Port 80 is handled by the
			// separate block from httpsRedirectBlock.
			parts = append(parts, hostWithPort(h, "443"))
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("ferron: site %q has only empty hosts", s.ID)
	}
	if s.TLS == proxy.TLSNone {
		return httpHostList(parts), nil
	}
	return strings.Join(parts, ", "), nil
}

// httpHostList joins plain-HTTP host labels. Ferron takes the protocol prefix once per block
// header: "http a:80, http b:80" parses an extra empty host named "http ", which then clashes
// across files under strict_hosts.
func httpHostList(parts []string) string {
	return "http " + strings.Join(parts, ", ")
}

// hostWithPort appends port to a host block label unless it already carries one.
func hostWithPort(h, port string) string {
	if strings.HasPrefix(h, "[") { // IPv6 literal
		if strings.Contains(h, "]:") {
			return h
		}
		return h + ":" + port
	}
	if strings.Contains(h, ":") {
		return h
	}
	return h + ":" + port
}

// httpsRedirectBlock renders the plain-HTTP (:80) companion block of a TLS site. Ferron answers
// ACME HTTP-01 challenges before any other stage, so the redirect does not interfere with
// certificate issuance; every other request is sent to HTTPS with a 308.
func httpsRedirectBlock(s proxy.Site) string {
	if s.TLS == proxy.TLSNone {
		return ""
	}
	parts := make([]string, 0, len(s.Hosts))
	seen := map[string]bool{}
	for _, raw := range s.Hosts {
		h := strings.TrimSpace(proxy.CleanQuotedValue(raw))
		key := strings.ToLower(h)
		if h == "" || seen[key] || strings.HasPrefix(key, "http ") {
			continue
		}
		seen[key] = true
		if strings.HasPrefix(h, "[") || !strings.Contains(h, ":") {
			parts = append(parts, hostWithPort(h, "80"))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n" + httpHostList(parts) + " {\n")
	b.WriteString("    status 308 {\n")
	b.WriteString("        location \"https://{{request.host}}{{request.uri}}\"\n")
	b.WriteString("    }\n")
	b.WriteString("}\n")
	return b.String()
}

func writeTLSBlock(b *strings.Builder, s proxy.Site, d *Driver, acmeEmail string) error {
	if s.TLS == proxy.TLSNone {
		return nil
	}
	if mc, ok := d.manualCertFor(s.ID, s.Hosts); ok {
		b.WriteString("    tls {\n")
		b.WriteString("        provider manual\n")
		fmt.Fprintf(b, "        cert %s\n", quoteString(mc.CertPath))
		fmt.Fprintf(b, "        key %s\n", quoteString(mc.KeyPath))
		b.WriteString("    }\n")
		return nil
	}
	if s.TLS == proxy.TLSInternal {
		stem, err := pki.SafeSiteStem(s.ID)
		if err != nil {
			stem = "site_" + siteSlug(s.ID, 0)
		}
		b.WriteString("    tls {\n")
		b.WriteString("        provider manual\n")
		fmt.Fprintf(b, "        cert %s\n", quoteString(containerPKISitesDir+"/"+stem+".crt"))
		fmt.Fprintf(b, "        key %s\n", quoteString(containerPKISitesDir+"/"+stem+".key"))
		b.WriteString("    }\n")
		return nil
	}

	b.WriteString("    tls {\n")
	b.WriteString("        provider acme\n")
	b.WriteString("        challenge http-01\n")
	if email := strings.TrimSpace(acmeEmail); email != "" {
		fmt.Fprintf(b, "        contact %s\n", quoteString(email))
	}
	if d.acmeDirectory != "" && d.acmeDirectory != DefaultACMEDirectory {
		fmt.Fprintf(b, "        directory %s\n", quoteString(d.acmeDirectory))
	}
	fmt.Fprintf(b, "        cache %s\n", quoteString(d.acmeCacheDir))
	if s.OnDemandTLS {
		b.WriteString("        on_demand true\n")
		if ask := strings.TrimSpace(d.onDemandAskURL); ask != "" {
			fmt.Fprintf(b, "        on_demand_ask %s\n", quoteString(ask))
		}
	}
	b.WriteString("    }\n")
	return nil
}

func resolveSiteUpstream(s proxy.Site) *proxy.UpstreamRef {
	if s.Upstream != nil {
		return s.Upstream
	}
	if s.Default != nil && s.Default.Upstream != nil {
		return s.Default.Upstream
	}
	return nil
}

func formatUpstreamURL(up proxy.UpstreamRef) (string, error) {
	if ext := strings.TrimSpace(up.External); ext != "" {
		u, err := url.Parse(ext)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return "", fmt.Errorf("ferron: invalid external upstream URL %q", ext)
		}
		return ext, nil
	}
	scheme := strings.ToLower(strings.TrimSpace(up.Scheme))
	if scheme == "" {
		scheme = "http"
	}
	host := strings.TrimSpace(up.Container)
	if host == "" {
		host = strings.TrimSpace(up.Service)
	}
	if host == "" {
		host = strings.TrimSpace(up.Name)
	}
	if host == "" {
		return "", fmt.Errorf("ferron: upstream has empty target host")
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	if up.Port > 0 {
		return fmt.Sprintf("%s://%s:%d", scheme, host, up.Port), nil
	}
	return fmt.Sprintf("%s://%s", scheme, host), nil
}

// isHostnameUpstream reports whether rawURL targets a DNS/container name rather than an IP literal.
func isHostnameUpstream(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	host := u.Hostname()
	if host == "" {
		return false
	}
	return net.ParseIP(host) == nil
}

func writeHeaderOps(b *strings.Builder, indent string, ops []proxy.HeaderOp) {
	for _, op := range ops {
		name := strings.TrimSpace(op.Name)
		if name == "" {
			continue
		}
		switch op.Action {
		case proxy.HeaderDelete:
			fmt.Fprintf(b, "%sheader %s\n", indent, quoteString("-"+name))
		case proxy.HeaderAdd:
			fmt.Fprintf(b, "%sheader %s %s\n", indent, quoteString("+"+name), quoteString(op.Value))
		default:
			fmt.Fprintf(b, "%sheader %s %s\n", indent, quoteString(name), quoteString(op.Value))
		}
	}
}

func normalizePrefix(raw string) string {
	// Path prefixes may arrive in Caddy matcher form ("/static/*"); the glob tail is implied.
	raw = strings.TrimSpace(raw)
	for strings.HasSuffix(raw, "*") {
		raw = strings.TrimSuffix(raw, "*")
	}
	clean := path.Clean("/" + raw)
	if clean == "/" {
		return "/"
	}
	return strings.TrimSuffix(clean, "/")
}

func prefixMatchRegex(prefix string) string {
	p := normalizePrefix(prefix)
	if p == "/" {
		return "^/"
	}
	return "^" + regexp.QuoteMeta(p) + `(?:/|$)`
}

func hasHeaderName(ops []proxy.HeaderOp, target string) bool {
	for _, op := range ops {
		if strings.EqualFold(strings.TrimSpace(op.Name), target) {
			return true
		}
	}
	return false
}

// globToRegex quotes a shell-style path glob for a regex: "*" matches within one path segment
// and "?" one character (".env*" hides .env, .env.local, .env.production …).
func globToRegex(glob string) string {
	q := regexp.QuoteMeta(glob)
	q = strings.ReplaceAll(q, `\*`, `[^/]*`)
	return strings.ReplaceAll(q, `\?`, `[^/]`)
}

func buildHideRegex(patterns []string, skipStandard bool) string {
	var alts []string
	for _, raw := range patterns {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		if skipStandard {
			if p == ".*" || strings.HasPrefix(p, "*.bak") || strings.HasPrefix(p, "*.sql") || strings.HasPrefix(p, "*.log") ||
				p == ".git" || p == ".env" || p == ".htaccess" || p == ".htpasswd" || p == "wp-config.php" {
				continue
			}
		}
		if strings.HasPrefix(p, "*.") {
			alts = append(alts, `\.`+globToRegex(strings.TrimPrefix(p, "*."))+`$`)
		} else {
			alts = append(alts, `(?:^|/)`+globToRegex(strings.TrimLeft(p, "/"))+`(?:/|$)`)
		}
	}
	if len(alts) == 0 {
		return ""
	}
	return "(?i)(?:" + strings.Join(alts, "|") + ")"
}
