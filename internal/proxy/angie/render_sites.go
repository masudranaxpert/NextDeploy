package angie

import (
	"fmt"
	"strings"

	"panel/internal/proxy"
)

func (d *Driver) renderAppProxySite(site proxy.Site, tlsInfo tlsResolution, cfGlobal bool) (string, error) {
	hosts := cleanHosts(site.Hosts)
	if len(hosts) == 0 {
		return "", nil
	}

	var body strings.Builder
	if err := writeServerHeaders(&body, site, tlsInfo.Mode != proxy.TLSNone && d.enableQUIC); err != nil {
		return "", err
	}

	if siteUsesDockerUpstream(site) {
		body.WriteString("    error_page 502 =503 @nd_down;\n")
		body.WriteString("    location @nd_down {\n")
		body.WriteString("        internal;\n")
		body.WriteString("        default_type text/plain;\n")
		body.WriteString("        return 503 \"Service temporarily unavailable\\n\";\n")
		body.WriteString("    }\n\n")
	}

	for _, pr := range site.Paths {
		if err := writeAppPathRoute(&body, pr, site); err != nil {
			return "", err
		}
	}

	if err := writeAppDefaultLocation(&body, site); err != nil {
		return "", err
	}

	return d.wrapServerBlocks(site, hosts, tlsInfo, cfGlobal, body.String()), nil
}

func siteUsesDockerUpstream(site proxy.Site) bool {
	isDocker := func(up *proxy.UpstreamRef) bool {
		return up != nil && strings.TrimSpace(up.External) == "" && SanitizeIdent(up.Name) != ""
	}
	if isDocker(site.Upstream) || (site.Default != nil && isDocker(site.Default.Upstream)) {
		return true
	}
	for _, pr := range site.Paths {
		if pr.Handler.Kind == proxy.HandlerProxy && isDocker(pr.Handler.Upstream) {
			return true
		}
	}
	return false
}

func writeAppPathRoute(b *strings.Builder, pr proxy.PathRoute, site proxy.Site) error {
	prefix := normalizeLocationPrefix(pr.Prefix)
	if prefix == "" {
		return nil
	}
	if proxy.HasUnsafeLabelChars(prefix) {
		return fmt.Errorf("angie site %q: unsafe path prefix %q", site.ID, pr.Prefix)
	}

	switch pr.Handler.Kind {
	case proxy.HandlerStaticDir:
		if pr.Handler.Static == nil || strings.TrimSpace(pr.Handler.Static.Root) == "" {
			return nil
		}
		root := strings.TrimRight(strings.TrimSpace(pr.Handler.Static.Root), "/")
		if root == "" {
			root = "/"
		}
		if proxy.HasUnsafeLabelChars(root) {
			return fmt.Errorf("angie site %q: unsafe static root %q", site.ID, root)
		}
		locPath := prefix
		if locPath != "/" {
			locPath = prefix + "/"
		}
		fmt.Fprintf(b, "    location ^~ %s {\n", locPath)
		if pr.StripPrefix && locPath != "/" {
			fmt.Fprintf(b, "        alias %s/;\n", root)
		} else {
			fmt.Fprintf(b, "        root %s;\n", root)
		}
		writePrecompressedDirectives(b, "        ", pr.Handler.Static.Precompressed)
		writeStaticHideBlock(b, "        ", pr.Handler.Static.Hide)
		b.WriteString("    }\n\n")
	case proxy.HandlerProxy:
		up := pr.Handler.Upstream
		if up == nil {
			up = site.Upstream
		}
		if up == nil {
			return fmt.Errorf("angie site %q path %q: missing upstream", site.ID, pr.Prefix)
		}
		fmt.Fprintf(b, "    location %s {\n", prefix)
		if pr.StripPrefix && prefix != "/" {
			fmt.Fprintf(b, "        rewrite ^%s(?:/(.*))?$ /$1 break;\n", escapeRegexMeta(prefix))
		}
		if err := writeProxyPassBlock(b, "        ", *up, site.TrustedProxy, site.BehindShield); err != nil {
			return err
		}
		b.WriteString("    }\n\n")
	}
	return nil
}

func writeAppDefaultLocation(b *strings.Builder, site proxy.Site) error {
	if site.Default != nil {
		switch site.Default.Kind {
		case proxy.HandlerRedirect:
			if site.Default.Redirect != nil {
				target, err := formatRedirectTarget(site.Default.Redirect)
				if err != nil {
					return err
				}
				code := normalizeRedirectCode(site.Default.Redirect.Code)
				b.WriteString("    location / {\n")
				fmt.Fprintf(b, "        return %d %s;\n", code, target)
				b.WriteString("    }\n")
				return nil
			}
		case proxy.HandlerStaticDir:
			if site.Default.Static != nil && strings.TrimSpace(site.Default.Static.Root) != "" {
				root := strings.TrimRight(strings.TrimSpace(site.Default.Static.Root), "/")
				if proxy.HasUnsafeLabelChars(root) {
					return fmt.Errorf("angie site %q: unsafe static root %q", site.ID, root)
				}
				b.WriteString("    location / {\n")
				fmt.Fprintf(b, "        root %s;\n", root)
				writePrecompressedDirectives(b, "        ", site.Default.Static.Precompressed)
				writeStaticHideBlock(b, "        ", site.Default.Static.Hide)
				b.WriteString("        try_files $uri $uri/ =404;\n")
				b.WriteString("    }\n")
				return nil
			}
		case proxy.HandlerProxy:
			if site.Default.Upstream != nil {
				b.WriteString("    location / {\n")
				if err := writeProxyPassBlock(b, "        ", *site.Default.Upstream, site.TrustedProxy, site.BehindShield); err != nil {
					return err
				}
				b.WriteString("    }\n")
				return nil
			}
		}
	}

	if site.Upstream == nil {
		return fmt.Errorf("angie site %q: missing upstream", site.ID)
	}
	b.WriteString("    location / {\n")
	if err := writeProxyPassBlock(b, "        ", *site.Upstream, site.TrustedProxy, site.BehindShield); err != nil {
		return err
	}
	b.WriteString("    }\n")
	return nil
}

func writeProxyPassBlock(b *strings.Builder, indent string, up proxy.UpstreamRef, trustedProxy, behindShield bool) error {
	ext := strings.TrimSpace(up.External)
	if ext != "" {
		if proxy.HasUnsafeLabelChars(ext) {
			return fmt.Errorf("angie upstream %q: unsafe external target %q", up.Name, ext)
		}
		target := strings.TrimRight(ext, "/")
		fmt.Fprintf(b, "%sset $nd_target \"%s\";\n", indent, escapeNginxString(target))
		fmt.Fprintf(b, "%sproxy_pass $nd_target;\n", indent)
		if strings.HasPrefix(strings.ToLower(target), "https://") || strings.EqualFold(up.Scheme, "https") {
			fmt.Fprintf(b, "%sproxy_ssl_server_name on;\n", indent)
			if up.SkipVerify {
				fmt.Fprintf(b, "%sproxy_ssl_verify off;\n", indent)
			} else {
				fmt.Fprintf(b, "%sproxy_ssl_verify on;\n", indent)
				fmt.Fprintf(b, "%sproxy_ssl_trusted_certificate /etc/ssl/certs/ca-certificates.crt;\n", indent)
				fmt.Fprintf(b, "%sproxy_ssl_verify_depth 2;\n", indent)
			}
		}
	} else {
		name := SanitizeIdent(up.Name)
		if name == "" {
			return fmt.Errorf("angie upstream missing name")
		}
		scheme := strings.ToLower(strings.TrimSpace(up.Scheme))
		if scheme != "https" {
			scheme = "http"
		}
		fmt.Fprintf(b, "%sproxy_pass %s://%s;\n", indent, scheme, name)
	}

	fmt.Fprintf(b, "%sinclude snippets/proxy.conf;\n", indent)
	writeForwardedHeaders(b, indent, up.RequestHeaders, trustedProxy, behindShield)
	return nil
}

func writeForwardedHeaders(b *strings.Builder, indent string, reqHeaders []proxy.HeaderOp, trustedProxy, behindShield bool) {
	if !trustedProxy && !behindShield {
		fmt.Fprintf(b, "%sproxy_set_header X-Real-IP $remote_addr;\n", indent)
		fmt.Fprintf(b, "%sproxy_set_header X-Forwarded-For $remote_addr;\n", indent)
		fmt.Fprintf(b, "%sproxy_set_header CF-Connecting-IP \"\";\n", indent)
		return
	}

	hasXRealIP, hasXFF := false, false
	for _, op := range reqHeaders {
		name := strings.TrimSpace(op.Name)
		if name == "" || proxy.HasUnsafeLabelChars(name) {
			continue
		}
		if strings.EqualFold(name, "X-Real-IP") {
			hasXRealIP = true
		}
		if strings.EqualFold(name, "X-Forwarded-For") {
			hasXFF = true
		}
	}
	if !hasXRealIP {
		fmt.Fprintf(b, "%sproxy_set_header X-Real-IP $remote_addr;\n", indent)
	}
	if !hasXFF {
		fmt.Fprintf(b, "%sproxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n", indent)
	}
	for _, op := range reqHeaders {
		name := strings.TrimSpace(op.Name)
		if name == "" || proxy.HasUnsafeLabelChars(name) {
			continue
		}
		if op.Action == proxy.HeaderDelete {
			fmt.Fprintf(b, "%sproxy_set_header %s \"\";\n", indent, name)
			continue
		}
		val := translateProxyHeaderValue(op.Value)
		fmt.Fprintf(b, "%sproxy_set_header %s \"%s\";\n", indent, name, escapeNginxString(val))
	}
}

func translateProxyHeaderValue(raw string) string {
	switch strings.TrimSpace(raw) {
	case "{client_ip}", "{remote_host}":
		return "$remote_addr"
	case "{host}":
		return "$host"
	case "{scheme}":
		return "$scheme"
	default:
		return sanitizeHeaderValue(raw)
	}
}
