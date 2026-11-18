package ferron

import (
	"fmt"
	"strings"

	"panel/internal/proxy"
)

type phpRedirMatcher struct {
	matcher string
	regex   string
	target  string
	code    int
}

func (d *Driver) renderPHPSite(b *strings.Builder, slug string, s proxy.Site, acmeEmail string) error {
	hostLine, err := formatHostsHeader(s)
	if err != nil {
		return err
	}
	spec := s.PHP
	if spec == nil {
		return fmt.Errorf("ferron: site %q missing PHP/static spec", s.ID)
	}
	docRoot := strings.TrimSpace(spec.DocRoot)
	if docRoot == "" {
		return fmt.Errorf("ferron: site %q missing doc_root", s.ID)
	}
	isPHP := s.Kind == proxy.SiteKindPHP && strings.TrimSpace(spec.Socket) != ""

	var hideMatch, hideRe string
	if hideRe = buildHideRegex(spec.HidePatterns, true); hideRe != "" {
		hideMatch = fmt.Sprintf("nd_%s_hide", slug)
		fmt.Fprintf(b, "match %s {\n    request.uri.path !~ r\"^/\\.well-known(?:/|$)\"\n    request.uri.path ~ %s\n}\n\n", hideMatch, rawString(hideRe))
	}

	redirMatchers, rewrites, ignoredComments, err := d.prepareRoutingRules(slug, s.ID, spec.Routing)
	if err != nil {
		return err
	}
	for _, rm := range redirMatchers {
		fmt.Fprintf(b, "match %s {\n", rm.matcher)
		b.WriteString("    request.uri.path !~ r\"\\.php/\"\n")
		fmt.Fprintf(b, "    request.uri.path ~ %s\n", rawString(rm.regex))
		if strings.Contains(rm.target, "{{request.uri.query}}") {
			b.WriteString("    request.uri.query != \"\"\n")
		} else if strings.HasSuffix(rm.matcher, "_noq") {
			b.WriteString("    request.uri.query == \"\"\n")
		}
		b.WriteString("}\n\n")
	}

	fmt.Fprintf(b, "%s {\n", hostLine)
	if err := writeTLSBlock(b, s, d, acmeEmail); err != nil {
		return err
	}
	fmt.Fprintf(b, "    root %s\n", quoteString(docRoot))
	b.WriteString("    disable_symlinks false\n")
	if isPHP {
		b.WriteString("    index \"index.php\" \"index.html\" \"index.htm\"\n")
	} else {
		b.WriteString("    index \"index.html\" \"index.htm\"\n")
	}
	if wantsCompression(spec.Compression) {
		b.WriteString("    compressed true\n")
		b.WriteString("    dynamic_compressed true\n")
	}
	if len(spec.Precompressed) > 0 || wantsCompression(spec.Compression) {
		b.WriteString("    precompressed true\n")
	}

	writePHPSecurity(b, s, s.Security)
	writeHeaderOps(b, "    ", s.Headers)

	b.WriteString("    if ND_HIDDEN_FILES {\n        status 404\n    }\n")
	if hideMatch != "" {
		fmt.Fprintf(b, "    if %s {\n        status 404\n    }\n", hideMatch)
	}
	if spec.Cache != nil {
		if cache := strings.TrimSpace(spec.Cache.MediaCacheControl); cache != "" {
			fmt.Fprintf(b, "    if ND_MEDIA_EXT {\n        header \"Cache-Control\" %s\n    }\n", quoteString(cache))
		}
		if cache := strings.TrimSpace(spec.Cache.CodeCacheControl); cache != "" {
			fmt.Fprintf(b, "    if ND_CODE_EXT {\n        header \"Cache-Control\" %s\n    }\n", quoteString(cache))
		}
	}

	if isPHP {
		sock := strings.TrimSpace(spec.Socket)
		if !strings.HasPrefix(sock, "unix://") {
			sock = "unix://" + sock
		}
		b.WriteString("    fcgi {\n")
		fmt.Fprintf(b, "        backend %s\n", quoteString(sock))
		b.WriteString("        extension \".php\"\n")
		b.WriteString("        pass false\n")
		b.WriteString("        keepalive true\n")
		if s.TLS != proxy.TLSNone || s.BehindShield {
			b.WriteString("        environment \"HTTPS\" \"on\"\n")
		}
		if !s.TrustedProxy && !s.BehindShield {
			b.WriteString("        environment \"HTTP_X_REAL_IP\" \"{{remote.ip}}\"\n")
			b.WriteString("        environment \"HTTP_X_FORWARDED_FOR\" \"{{remote.ip}}\"\n")
			b.WriteString("        environment \"HTTP_CF_CONNECTING_IP\" \"\"\n")
		}
		b.WriteString("    }\n")
	}

	for _, rm := range redirMatchers {
		fmt.Fprintf(b, "    if %s {\n", rm.matcher)
		fmt.Fprintf(b, "        status %d {\n", rm.code)
		if strings.Contains(rm.target, "${") || strings.Contains(rm.target, "$") {
			fmt.Fprintf(b, "            regex %s\n", rawString(rm.regex))
		}
		fmt.Fprintf(b, "            location %s\n", quoteString(rm.target))
		b.WriteString("        }\n")
		b.WriteString("    }\n")
	}

	for _, c := range ignoredComments {
		fmt.Fprintf(b, "    # %s\n", c)
	}

	fallbackLine := resolvePHPFallbackRewrite(spec.FallbackMode, spec.FallbackEntry, isPHP)
	if len(rewrites) > 0 || fallbackLine != "" {
		b.WriteString("    if_not ND_PHP_PATHINFO {\n")
		for _, rw := range rewrites {
			b.WriteString(rw)
		}
		if fallbackLine != "" {
			b.WriteString(fallbackLine)
		}
		b.WriteString("    }\n")
	} else if strings.EqualFold(strings.TrimSpace(spec.FallbackMode), "strict_404") {
		b.WriteString("    # fallback: strict_404 (no front-controller rewrite)\n")
	}

	b.WriteString("}\n")
	return nil
}

func writePHPSecurity(b *strings.Builder, s proxy.Site, sec *proxy.SecuritySpec) {
	if sec == nil {
		return
	}
	if len(sec.BlockedIPs) > 0 {
		b.WriteString("    block")
		for _, ip := range sec.BlockedIPs {
			fmt.Fprintf(b, " %s", quoteString(strings.TrimSpace(ip)))
		}
		b.WriteString("\n")
	}
	if len(sec.AllowOnly) > 0 {
		b.WriteString("    allow")
		for _, ip := range sec.AllowOnly {
			fmt.Fprintf(b, " %s", quoteString(strings.TrimSpace(ip)))
		}
		b.WriteString("\n")
	}
	if user := strings.TrimSpace(sec.BasicAuthUser); user != "" && strings.TrimSpace(sec.BasicAuthHash) != "" {
		b.WriteString("    basic_auth {\n")
		b.WriteString("        realm \"Restricted\"\n")
		b.WriteString("        users {\n")
		fmt.Fprintf(b, "            %s %s\n", formatBasicAuthUser(user), quoteString(strings.TrimSpace(sec.BasicAuthHash)))
		b.WriteString("        }\n")
		b.WriteString("    }\n")
	}
	if sec.BlockXMLRPC {
		b.WriteString("    if ND_XMLRPC {\n        status 403\n    }\n")
	}
	if sec.BlockUploadPHP {
		b.WriteString("    if ND_UPLOAD_PHP {\n        status 403\n    }\n")
	}
	if sec.BlockScanners {
		b.WriteString("    if ND_SCANNER_UA {\n        status 403\n    }\n")
	}
	if sec.HSTS && (s.TLS != proxy.TLSNone || s.BehindShield) && !hasHeaderName(s.Headers, "Strict-Transport-Security") {
		b.WriteString("    header \"Strict-Transport-Security\" \"max-age=31536000; includeSubDomains\"\n")
	}
	if sec.FrameProtection && !hasHeaderName(s.Headers, "X-Frame-Options") {
		b.WriteString("    header \"X-Frame-Options\" \"SAMEORIGIN\"\n")
	}
}

func formatBasicAuthUser(user string) string {
	for i := 0; i < len(user); i++ {
		c := user[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.' {
			continue
		}
		return quoteString(user)
	}
	return user
}

func wantsCompression(encode []string) bool {
	if len(encode) == 0 {
		return true
	}
	for _, alg := range encode {
		if strings.EqualFold(strings.TrimSpace(alg), "none") || strings.EqualFold(strings.TrimSpace(alg), "off") {
			return false
		}
	}
	return true
}
