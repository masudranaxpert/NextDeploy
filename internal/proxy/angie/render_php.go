package angie

import (
	"fmt"
	"strings"

	"panel/internal/proxy"
)

func (d *Driver) renderPHPSite(site proxy.Site, tlsInfo tlsResolution, cfGlobal bool) (proxy.Files, error) {
	if site.PHP == nil {
		return nil, fmt.Errorf("angie site %q (%s): missing php spec", site.ID, site.Kind)
	}
	if site.PHP.Suspended {
		return proxy.Files{}, nil
	}
	hosts := cleanHosts(site.Hosts)
	if len(hosts) == 0 {
		return proxy.Files{}, nil
	}

	docRoot := strings.TrimRight(strings.TrimSpace(site.PHP.DocRoot), "/")
	if docRoot == "" || proxy.HasUnsafeLabelChars(docRoot) {
		return nil, fmt.Errorf("angie site %q: invalid doc_root %q", site.ID, site.PHP.DocRoot)
	}

	isPHP := site.Kind == proxy.SiteKindPHP
	socket := strings.TrimSpace(site.PHP.Socket)
	if isPHP {
		if socket == "" || proxy.HasUnsafeLabelChars(socket) {
			return nil, fmt.Errorf("angie php site %q: invalid fastcgi socket %q", site.ID, site.PHP.Socket)
		}
	}

	files := make(proxy.Files)
	stem := SafeFileStem(site.ID)

	var body strings.Builder
	fmt.Fprintf(&body, "    root %s;\n", docRoot)
	if isPHP {
		body.WriteString("    index index.php index.html;\n")
	} else {
		body.WriteString("    index index.html index.htm;\n")
	}
	body.WriteString("    disable_symlinks off;\n")
	maxBody := DefaultMaxBodySize
	if d != nil && strings.TrimSpace(d.maxBodySize) != "" {
		maxBody = strings.TrimSpace(d.maxBodySize)
	}
	fmt.Fprintf(&body, "    client_max_body_size %s;\n", maxBody)
	writePHPCompression(&body, site.PHP)
	body.WriteString("\n")

	if err := writePHPHeaders(&body, site, tlsInfo.Mode != proxy.TLSNone && d.enableQUIC); err != nil {
		return nil, err
	}
	if err := writePHPSecurity(&body, site, stem, files); err != nil {
		return nil, err
	}
	if err := writePHPRoutingRules(&body, site.PHP.Routing); err != nil {
		return nil, fmt.Errorf("angie site %q routing: %w", site.ID, err)
	}

	writePHPHideLocations(&body, site.PHP, site.Security)
	if isPHP {
		writeFastCGILocation(&body, site, socket, tlsInfo.Mode != proxy.TLSNone)
	}
	writePHPCacheLocations(&body, site.PHP)
	writePHPFallbackLocation(&body, site)

	files["sites/"+stem+".conf"] = d.wrapServerBlocks(site, hosts, tlsInfo, cfGlobal, body.String())
	return files, nil
}

func writePHPCompression(b *strings.Builder, spec *proxy.PHPSpec) {
	hasGzip, hasZstd := false, false
	for _, c := range spec.Compression {
		switch strings.ToLower(strings.TrimSpace(c)) {
		case "gzip":
			hasGzip = true
		case "zstd":
			hasZstd = true
		}
	}
	if hasGzip {
		b.WriteString("    gzip on;\n    gzip_vary on;\n    gzip_types text/plain text/css application/javascript application/json image/svg+xml application/xml;\n")
	}
	if hasZstd {
		b.WriteString("    zstd on;\n    zstd_types text/plain text/css application/javascript application/json image/svg+xml application/xml;\n")
	}
	writePrecompressedDirectives(b, "    ", spec.Precompressed)
}

func writePHPHeaders(b *strings.Builder, site proxy.Site, withAltSvc bool) error {
	if err := writeServerHeaders(b, site, false); err != nil {
		return err
	}
	hasFrame, hasHSTS := false, false
	for _, h := range site.Headers {
		if strings.EqualFold(strings.TrimSpace(h.Name), "X-Frame-Options") {
			hasFrame = true
		}
		if strings.EqualFold(strings.TrimSpace(h.Name), "Strict-Transport-Security") {
			hasHSTS = true
		}
	}
	wroteExtra := false
	if site.Security != nil {
		if site.Security.HSTS && !hasHSTS {
			b.WriteString("    more_set_headers \"Strict-Transport-Security: max-age=31536000; includeSubDomains\";\n")
			wroteExtra = true
		}
		if site.Security.FrameProtection && !hasFrame {
			b.WriteString("    more_set_headers \"X-Frame-Options: SAMEORIGIN\";\n")
			wroteExtra = true
		}
	}
	if withAltSvc {
		b.WriteString("    more_set_headers 'Alt-Svc: h3=\":443\"; ma=86400';\n")
		wroteExtra = true
	}
	if wroteExtra {
		b.WriteString("\n")
	}
	return nil
}

func writeFastCGILocation(b *strings.Builder, site proxy.Site, socket string, hasTLS bool) {
	spec := site.PHP
	dialTimeout := formatAngieDuration(spec.FastCGIDialTimeout, "5s")
	readTimeout := formatAngieDuration(spec.FastCGIReadTimeout, "120s")
	writeTimeout := formatAngieDuration(spec.FastCGIWriteTimeout, "120s")

	b.WriteString("    location ~ ^(.+\\.php)(/.*)?$ {\n")
	b.WriteString("        fastcgi_split_path_info ^(.+\\.php)(/.*)$;\n")
	b.WriteString("        set $nd_path_info $fastcgi_path_info;\n")
	b.WriteString("        try_files $fastcgi_script_name =404;\n")
	b.WriteString("        include /etc/angie/fastcgi.conf;\n")
	b.WriteString("        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;\n")
	b.WriteString("        fastcgi_param PATH_INFO $nd_path_info;\n")
	if spec.HTTPS || hasTLS || site.BehindShield {
		b.WriteString("        fastcgi_param HTTPS on;\n")
	}
	if !site.TrustedProxy && !site.BehindShield {
		b.WriteString("        fastcgi_param HTTP_X_REAL_IP $remote_addr;\n")
		b.WriteString("        fastcgi_param HTTP_X_FORWARDED_FOR $remote_addr;\n")
		b.WriteString("        fastcgi_param HTTP_CF_CONNECTING_IP \"\";\n")
	}
	fmt.Fprintf(b, "        fastcgi_connect_timeout %s;\n", dialTimeout)
	fmt.Fprintf(b, "        fastcgi_read_timeout %s;\n", readTimeout)
	fmt.Fprintf(b, "        fastcgi_send_timeout %s;\n", writeTimeout)
	fmt.Fprintf(b, "        fastcgi_pass unix:%s;\n", socket)
	b.WriteString("    }\n\n")
}

func writePHPCacheLocations(b *strings.Builder, spec *proxy.PHPSpec) {
	mediaCC := "public, max-age=2592000, immutable"
	codeCC := "public, max-age=604800, stale-while-revalidate=86400"
	if spec.Cache != nil {
		if v := sanitizeHeaderValue(spec.Cache.MediaCacheControl); v != "" {
			mediaCC = v
		}
		if v := sanitizeHeaderValue(spec.Cache.CodeCacheControl); v != "" {
			codeCC = v
		}
	}
	b.WriteString("    location ~* \\.(ico|gif|jpe?g|png|svg|woff2?|webp|avif|ttf|eot)$ {\n")
	fmt.Fprintf(b, "        add_header Cache-Control \"%s\";\n", escapeNginxString(mediaCC))
	b.WriteString("        try_files $uri =404;\n")
	b.WriteString("    }\n")
	b.WriteString("    location ~* \\.(css|js)$ {\n")
	fmt.Fprintf(b, "        add_header Cache-Control \"%s\";\n", escapeNginxString(codeCC))
	b.WriteString("        try_files $uri =404;\n")
	b.WriteString("    }\n\n")
}

func writePHPFallbackLocation(b *strings.Builder, site proxy.Site) {
	spec := site.PHP
	var parts []string
	if site.Kind == proxy.SiteKindPHP && spec.Cache != nil && spec.Cache.WordPressPageCache {
		parts = append(parts, "$wp_c1", "$wp_c2", "$wp_c3", "$wp_c4", "$wp_c5", "$wp_c6", "$wp_c7", "$wp_c8")
	}
	parts = append(parts, "$uri", "$uri/")

	entry := strings.TrimPrefix(strings.TrimSpace(spec.FallbackEntry), "/")
	switch strings.ToLower(strings.TrimSpace(spec.FallbackMode)) {
	case proxy.FallbackStrict404:
		parts = append(parts, "=404")
	case proxy.FallbackCustom:
		if entry != "" && !proxy.HasUnsafeLabelChars(entry) {
			if site.Kind == proxy.SiteKindPHP {
				parts = append(parts, "/"+entry+"?$args")
			} else {
				parts = append(parts, "/"+entry)
			}
		} else if site.Kind == proxy.SiteKindPHP {
			parts = append(parts, "/index.php?$args")
		} else {
			parts = append(parts, "=404")
		}
	default:
		if site.Kind == proxy.SiteKindPHP {
			parts = append(parts, "/index.php?$args")
		} else {
			parts = append(parts, "=404")
		}
	}

	b.WriteString("    location / {\n")
	fmt.Fprintf(b, "        try_files %s;\n", strings.Join(parts, " "))
	b.WriteString("    }\n")
}
