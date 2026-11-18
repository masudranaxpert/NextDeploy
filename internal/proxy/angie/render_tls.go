package angie

import (
	"fmt"
	"strings"

	"panel/internal/proxy"
)

func (d *Driver) wrapServerBlocks(site proxy.Site, hosts []string, tlsInfo tlsResolution, cfGlobal bool, body string) string {
	serverNames := strings.Join(hosts, " ")
	zoneBase := SanitizeIdent("nd_" + site.ID)

	var b strings.Builder
	if tlsInfo.Mode == proxy.TLSNone {
		b.WriteString("server {\n")
		b.WriteString("    listen 80;\n")
		fmt.Fprintf(&b, "    server_name %s;\n", serverNames)
		fmt.Fprintf(&b, "    status_zone %s;\n", zoneBase)
		writeServerRealIPOverride(&b, site, cfGlobal)
		b.WriteString("\n")
		b.WriteString(body)
		b.WriteString("}\n")
		return b.String()
	}

	b.WriteString("server {\n")
	b.WriteString("    listen 80;\n")
	fmt.Fprintf(&b, "    server_name %s;\n", serverNames)
	fmt.Fprintf(&b, "    status_zone %s;\n", zoneBase)
	b.WriteString("    return 308 https://$host$request_uri;\n")
	b.WriteString("}\n\n")

	b.WriteString("server {\n")
	b.WriteString("    listen 443 ssl;\n")
	if d != nil && d.enableQUIC {
		b.WriteString("    listen 443 quic;\n")
	}
	b.WriteString("    http2 on;\n")
	fmt.Fprintf(&b, "    server_name %s;\n", serverNames)
	fmt.Fprintf(&b, "    status_zone %s;\n", zoneBase)
	writeServerRealIPOverride(&b, site, cfGlobal)
	b.WriteString("\n")
	writeTLSBlock(&b, tlsInfo)
	b.WriteString("\n")
	b.WriteString(body)
	b.WriteString("}\n")
	return b.String()
}

func writeServerRealIPOverride(b *strings.Builder, site proxy.Site, cfGlobal bool) {
	if site.BehindShield && !site.TrustedProxy && cfGlobal {
		b.WriteString("    real_ip_header X-Forwarded-For;\n")
	}
}

func writeTLSBlock(b *strings.Builder, tlsInfo tlsResolution) {
	switch {
	case tlsInfo.ManualCert != nil:
		fmt.Fprintf(b, "    ssl_certificate %s;\n", tlsInfo.ManualCert.CertPath)
		fmt.Fprintf(b, "    ssl_certificate_key %s;\n", tlsInfo.ManualCert.KeyPath)
	case tlsInfo.Mode == proxy.TLSInternal:
		fmt.Fprintf(b, "    ssl_certificate /etc/nd-pki/sites/%s.crt;\n", tlsInfo.InternalStem)
		fmt.Fprintf(b, "    ssl_certificate_key /etc/nd-pki/sites/%s.key;\n", tlsInfo.InternalStem)
	case tlsInfo.Mode == proxy.TLSAuto && tlsInfo.ACMEClient != "":
		fmt.Fprintf(b, "    acme %s;\n", tlsInfo.ACMEClient)
		fmt.Fprintf(b, "    ssl_certificate $acme_cert_%s;\n", tlsInfo.ACMEClient)
		fmt.Fprintf(b, "    ssl_certificate_key $acme_cert_key_%s;\n", tlsInfo.ACMEClient)
	}
}

func writeServerHeaders(b *strings.Builder, site proxy.Site, withAltSvc bool) error {
	wrote := false
	for _, op := range site.Headers {
		name := strings.TrimSpace(op.Name)
		if name == "" || proxy.HasUnsafeLabelChars(name) {
			continue
		}
		switch op.Action {
		case proxy.HeaderDelete:
			fmt.Fprintf(b, "    more_clear_headers \"%s\";\n", escapeNginxString(name))
			wrote = true
		case proxy.HeaderAdd:
			val := sanitizeHeaderValue(op.Value)
			fmt.Fprintf(b, "    add_header %s \"%s\" always;\n", name, escapeNginxString(val))
			wrote = true
		default:
			val := sanitizeHeaderValue(op.Value)
			fmt.Fprintf(b, "    more_set_headers \"%s: %s\";\n", escapeNginxString(name), escapeNginxString(val))
			wrote = true
		}
	}
	if withAltSvc {
		b.WriteString("    more_set_headers 'Alt-Svc: h3=\":443\"; ma=86400';\n")
		wrote = true
	}
	if wrote {
		b.WriteString("\n")
	}
	return nil
}

func renderGatewayServers(gateways []proxy.GatewayPort) string {
	if len(gateways) == 0 {
		return ""
	}
	var b strings.Builder
	seen := make(map[string]bool)
	for _, gw := range gateways {
		if gw.Direct || gw.Port <= 0 || gw.Upstream == nil {
			continue
		}
		upName := SanitizeIdent(gw.Upstream.Name)
		if upName == "" {
			continue
		}
		bindIP := strings.TrimSpace(gw.BindIP)
		listenAddr := fmt.Sprintf("%d", gw.Port)
		if bindIP != "" && bindIP != "0.0.0.0" {
			if strings.Contains(bindIP, ":") && !strings.HasPrefix(bindIP, "[") {
				listenAddr = fmt.Sprintf("[%s]:%d", bindIP, gw.Port)
			} else {
				listenAddr = fmt.Sprintf("%s:%d", bindIP, gw.Port)
			}
		} else {
			bindIP = "0.0.0.0"
		}
		dedupKey := fmt.Sprintf("%s:%d", bindIP, gw.Port)
		if seen[dedupKey] {
			continue
		}
		seen[dedupKey] = true

		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("server {\n")
		fmt.Fprintf(&b, "    listen %s;\n", listenAddr)
		b.WriteString("    server_name _;\n")
		fmt.Fprintf(&b, "    status_zone nd_gw_%d;\n\n", gw.Port)
		b.WriteString("    error_page 502 =503 @nd_down;\n")
		b.WriteString("    location @nd_down {\n")
		b.WriteString("        internal;\n")
		b.WriteString("        default_type text/plain;\n")
		b.WriteString("        return 503 \"Service temporarily unavailable\\n\";\n")
		b.WriteString("    }\n\n")
		b.WriteString("    location / {\n")
		fmt.Fprintf(&b, "        proxy_pass http://%s;\n", upName)
		b.WriteString("        include snippets/proxy.conf;\n")
		b.WriteString("        proxy_set_header X-Real-IP $remote_addr;\n")
		b.WriteString("        proxy_set_header X-Forwarded-For $remote_addr;\n")
		b.WriteString("        proxy_set_header CF-Connecting-IP \"\";\n")
		b.WriteString("    }\n")
		b.WriteString("}\n")
	}
	return b.String()
}
