package edge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RenderManual renders enabled manual domains into Angie configuration (zz-manual.conf).
func (e *AngieEngine) RenderManual(entries []ManualDomainEntry, opts ManualRenderOptions) (ManualRenderResult, error) {
	active := prepareManualEntriesForRender(entries)

	var b strings.Builder
	b.WriteString("# Managed by nd edge (manual domains). DO NOT EDIT.\n")

	if len(active) == 0 {
		return ManualRenderResult{
			FileName: ManualConfFileName,
			Content:  b.String(),
		}, nil
	}

	now := opts.Now
	if now.IsZero() {
		if e.nowFn != nil {
			now = e.nowFn()
		} else {
			now = time.Now().UTC()
		}
	}
	certsDir := opts.CertsDir
	if strings.TrimSpace(certsDir) == "" {
		certsDir = e.paths.CertsDir
	}
	if strings.TrimSpace(certsDir) == "" {
		certsDir = DefaultAngieCertsDir
	}
	manualCertsDir := opts.ManualCertsDir
	if strings.TrimSpace(manualCertsDir) == "" {
		manualCertsDir = e.paths.ManualCertsDir
	}
	acmeEmail := strings.TrimSpace(opts.ACMEEmail)
	if acmeEmail == "" {
		acmeEmail = e.acmeEmail
	}
	acmeEmail = sanitizeSingleLine(acmeEmail)
	accountKeyPath := filepath.ToSlash(filepath.Join(certsDir, "nd_account.key"))
	renewWindow := e.manualRenewWindow
	if renewWindow <= 0 {
		renewWindow = DefaultManualCertRenewWindow
	}
	systemCA := detectSystemCABundle()

	anyTrusted := false
	activeHostCount := 0

	for _, eEntry := range active {
		serveHosts := filterManualHostsByDNS(eEntry.PrimaryServedHosts(), eEntry.TLS, opts.DNSBlockedHosts)
		redirFrom, redirTo, hasRedir := eEntry.RedirectPair()
		if hasRedir && eEntry.TLS == ManualTLSAuto && opts.DNSBlockedHosts[redirFrom] {
			hasRedir = false
		}
		if len(serveHosts) == 0 && !hasRedir {
			continue
		}

		trustCF := manualEntryTrustsCF(eEntry, opts)
		if trustCF {
			anyTrusted = true
		}

		zoneBase := "nd_" + strings.TrimPrefix(eEntry.ID, "m_")
		upName := "nd_up_" + strings.TrimPrefix(eEntry.ID, "m_")

		if len(serveHosts) > 0 {
			writeAngieManualUpstream(&b, upName, eEntry)
			b.WriteByte('\n')
		}

		if hasRedir {
			activeHostCount++
			writeAngieManualRedirect(&b, eEntry, zoneBase+"_redir", redirFrom, redirTo, acmeEmail, accountKeyPath)
			b.WriteByte('\n')
		}

		if len(serveHosts) == 0 {
			continue
		}
		activeHostCount += len(serveHosts)

		switch eEntry.TLS {
		case ManualTLSNone:
			writeAngieManualHTTPServer(&b, eEntry, zoneBase, serveHosts, upName, trustCF, systemCA, false)
			b.WriteByte('\n')
		case ManualTLSManual:
			writeAngieManualTLSGroup(&b, eEntry, zoneBase, serveHosts, upName, trustCF, systemCA, acmeEmail, accountKeyPath, filepath.ToSlash(eEntry.CertFile), filepath.ToSlash(eEntry.KeyFile))
		default: // ManualTLSAuto
			groups := partitionAngieRouteTLSGroups(manualCertsDir, serveHosts, now, renewWindow)
			for idx, grp := range groups {
				gid := zoneBase
				if len(groups) > 1 {
					gid = fmt.Sprintf("%s_%d", zoneBase, idx)
				}
				writeAngieManualTLSGroup(&b, eEntry, gid, grp.hosts, upName, trustCF, systemCA, acmeEmail, accountKeyPath, grp.manualCert, grp.manualKey)
			}
		}
	}

	return ManualRenderResult{
		FileName:        ManualConfFileName,
		Content:         b.String(),
		AnyTrustedProxy: anyTrusted,
		ActiveHostCount: activeHostCount,
	}, nil
}

func detectSystemCABundle() string {
	candidates := []string{
		"/etc/ssl/certs/ca-certificates.crt",
		"/etc/pki/tls/certs/ca-bundle.crt",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return candidates[0]
}

func writeAngieManualUpstream(b *strings.Builder, name string, e ManualDomainEntry) {
	fmt.Fprintf(b, "upstream %s {\n", name)
	fmt.Fprintf(b, "\tzone %s 64k;\n", name)
	if len(e.Upstreams) > 1 {
		switch e.EffectiveLB() {
		case ManualLBLeastConn:
			b.WriteString("\tleast_conn;\n")
		case ManualLBIPHash:
			b.WriteString("\tip_hash;\n")
		}
	}
	for _, u := range e.Upstreams {
		fmt.Fprintf(b, "\tserver %s;\n", u.HostPort())
	}
	if ka := e.EffectiveKeepalive(); ka > 0 {
		fmt.Fprintf(b, "\tkeepalive %d;\n", ka)
	}
	b.WriteString("}\n")
}

func writeAngieManualRedirect(b *strings.Builder, e ManualDomainEntry, zoneID, fromHost, toHost, acmeEmail, accountKeyPath string) {
	if e.TLS == ManualTLSNone {
		b.WriteString("server {\n")
		b.WriteString("\tlisten 80;\n\tlisten [::]:80;\n")
		b.WriteString("\tserver_name " + fromHost + ";\n")
		b.WriteString("\tstatus_zone " + zoneID + "_http;\n")
		fmt.Fprintf(b, "\treturn 308 http://%s$request_uri;\n", toHost)
		b.WriteString("}\n")
		return
	}

	isManual := e.TLS == ManualTLSManual
	if !isManual {
		fmt.Fprintf(b, "acme_client %s %s", zoneID, DefaultACMEDirectoryURL)
		if acmeEmail != "" {
			fmt.Fprintf(b, " email=%s", acmeEmail)
		}
		fmt.Fprintf(b, " account_key=%s;\n\n", accountKeyPath)
	}

	b.WriteString("server {\n")
	b.WriteString("\tlisten 80;\n\tlisten [::]:80;\n")
	b.WriteString("\tserver_name " + fromHost + ";\n")
	b.WriteString("\tstatus_zone " + zoneID + "_http;\n")
	fmt.Fprintf(b, "\treturn 308 https://%s$request_uri;\n", toHost)
	b.WriteString("}\n\n")

	b.WriteString("server {\n")
	b.WriteString("\tlisten 443 ssl;\n\tlisten [::]:443 ssl;\n\tlisten 443 quic;\n\tlisten [::]:443 quic;\n\thttp2 on;\n")
	b.WriteString("\tserver_name " + fromHost + ";\n")
	b.WriteString("\tstatus_zone " + zoneID + ";\n")
	if isManual {
		fmt.Fprintf(b, "\tssl_certificate %s;\n\tssl_certificate_key %s;\n", filepath.ToSlash(e.CertFile), filepath.ToSlash(e.KeyFile))
	} else {
		fmt.Fprintf(b, "\tacme %s;\n\tssl_certificate $acme_cert_%s;\n\tssl_certificate_key $acme_cert_key_%s;\n", zoneID, zoneID, zoneID)
	}
	fmt.Fprintf(b, "\treturn 308 https://%s$request_uri;\n", toHost)
	b.WriteString("}\n")
}

func writeAngieManualTLSGroup(b *strings.Builder, e ManualDomainEntry, zoneID string, hosts []string, upName string, trustCF bool, systemCA, acmeEmail, accountKeyPath, certPath, keyPath string) {
	serverNames := strings.Join(hosts, " ")
	isManual := certPath != "" && keyPath != ""

	if !isManual {
		fmt.Fprintf(b, "acme_client %s %s", zoneID, DefaultACMEDirectoryURL)
		if acmeEmail != "" {
			fmt.Fprintf(b, " email=%s", acmeEmail)
		}
		fmt.Fprintf(b, " account_key=%s;\n\n", accountKeyPath)
	}

	if e.EffectiveForceHTTPS() {
		b.WriteString("server {\n")
		b.WriteString("\tlisten 80;\n\tlisten [::]:80;\n")
		b.WriteString("\tserver_name " + serverNames + ";\n")
		b.WriteString("\tstatus_zone " + zoneID + "_http;\n")
		b.WriteString("\treturn 308 https://$host$request_uri;\n")
		b.WriteString("}\n\n")
	} else {
		writeAngieManualHTTPServer(b, e, zoneID+"_http", hosts, upName, trustCF, systemCA, false)
		b.WriteByte('\n')
	}

	b.WriteString("server {\n")
	b.WriteString("\tlisten 443 ssl;\n\tlisten [::]:443 ssl;\n")
	b.WriteString("\tlisten 443 quic;\n\tlisten [::]:443 quic;\n")
	b.WriteString("\thttp2 on;\n")
	b.WriteString("\tserver_name " + serverNames + ";\n")
	b.WriteString("\tstatus_zone " + zoneID + ";\n\n")
	if isManual {
		b.WriteString("\tssl_certificate " + certPath + ";\n")
		b.WriteString("\tssl_certificate_key " + keyPath + ";\n\n")
	} else {
		b.WriteString("\tacme " + zoneID + ";\n")
		b.WriteString("\tssl_certificate $acme_cert_" + zoneID + ";\n")
		b.WriteString("\tssl_certificate_key $acme_cert_key_" + zoneID + ";\n\n")
	}
	writeAngieManualLocationBody(b, e, upName, trustCF, systemCA, true)
	b.WriteString("}\n\n")
}

func writeAngieManualHTTPServer(b *strings.Builder, e ManualDomainEntry, zoneID string, hosts []string, upName string, trustCF bool, systemCA string, withAltSvc bool) {
	b.WriteString("server {\n")
	b.WriteString("\tlisten 80;\n\tlisten [::]:80;\n")
	b.WriteString("\tserver_name " + strings.Join(hosts, " ") + ";\n")
	b.WriteString("\tstatus_zone " + zoneID + ";\n\n")
	writeAngieManualLocationBody(b, e, upName, trustCF, systemCA, withAltSvc)
	b.WriteString("}\n")
}

func writeAngieManualLocationBody(b *strings.Builder, e ManualDomainEntry, upName string, trustCF bool, systemCA string, withAltSvc bool) {
	fmt.Fprintf(b, "\tclient_max_body_size %d;\n\n", e.MaxBodyBytes)
	b.WriteString("\terror_page 502 503 504 =503 @nd_origin_unavailable;\n")
	b.WriteString("\tlocation @nd_origin_unavailable {\n")
	b.WriteString("\t\tadd_header Cache-Control \"no-store\" always;\n")
	b.WriteString("\t\treturn 503 \"Origin is temporarily unavailable. Please retry shortly.\\n\";\n")
	b.WriteString("\t}\n\n")

	scheme := e.Upstreams[0].Scheme
	b.WriteString("\tlocation / {\n")
	if withAltSvc {
		b.WriteString("\t\tadd_header Alt-Svc 'h3=\":443\"; ma=86400' always;\n")
	}
	for _, h := range e.HeadersDown {
		fmt.Fprintf(b, "\t\tadd_header %s \"%s\" always;\n", h.Name, escapeQuotedConfigValue(h.Value))
	}

	b.WriteString("\t\tproxy_pass " + scheme + "://" + upName + ";\n")
	b.WriteString("\t\tproxy_http_version 1.1;\n")
	if e.EffectiveWebsocket() {
		b.WriteString("\t\tproxy_set_header Connection $nd_connection_upgrade;\n")
		b.WriteString("\t\tproxy_set_header Upgrade $http_upgrade;\n")
	} else {
		b.WriteString("\t\tproxy_set_header Connection \"\";\n")
	}

	switch e.EffectiveHostHeader() {
	case ManualHostHeaderOriginal:
		b.WriteString("\t\tproxy_set_header Host $host;\n")
	case ManualHostHeaderUpstream:
		b.WriteString("\t\tproxy_set_header Host $proxy_host;\n")
	default:
		fmt.Fprintf(b, "\t\tproxy_set_header Host \"%s\";\n", escapeQuotedConfigValue(e.EffectiveHostHeader()))
	}

	if trustCF {
		b.WriteString("\t\tproxy_set_header X-Real-IP $remote_addr;\n")
		b.WriteString("\t\tproxy_set_header X-Forwarded-For $remote_addr;\n")
	} else {
		b.WriteString("\t\tproxy_set_header X-Real-IP $realip_remote_addr;\n")
		b.WriteString("\t\tproxy_set_header X-Forwarded-For $realip_remote_addr;\n")
		b.WriteString("\t\tproxy_set_header CF-Connecting-IP \"\";\n")
	}
	b.WriteString("\t\tproxy_set_header X-Forwarded-Proto $scheme;\n")
	for _, h := range e.HeadersUp {
		fmt.Fprintf(b, "\t\tproxy_set_header %s \"%s\";\n", h.Name, escapeQuotedConfigValue(h.Value))
	}

	fmt.Fprintf(b, "\t\tproxy_connect_timeout %ds;\n", e.EffectiveConnectTimeoutSec())
	fmt.Fprintf(b, "\t\tproxy_send_timeout %ds;\n", e.EffectiveTimeoutSec())
	fmt.Fprintf(b, "\t\tproxy_read_timeout %ds;\n", e.EffectiveTimeoutSec())
	b.WriteString("\t\tproxy_buffering off;\n")

	if scheme == "https" {
		b.WriteString("\t\tproxy_ssl_server_name on;\n")
		b.WriteString("\t\tproxy_ssl_name " + manualOriginSNI(e) + ";\n")
		if e.Origin.NoVerify {
			b.WriteString("\t\tproxy_ssl_verify off;\n")
		} else {
			b.WriteString("\t\tproxy_ssl_verify on;\n")
			b.WriteString("\t\tproxy_ssl_verify_depth 2;\n")
			b.WriteString("\t\tproxy_ssl_trusted_certificate " + systemCA + ";\n")
		}
	}
	b.WriteString("\t}\n")
}
