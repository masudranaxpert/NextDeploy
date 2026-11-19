package edge

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// RenderManual renders enabled manual domains into Ferron 3 configuration (zz-manual.conf).
func (e *FerronEngine) RenderManual(entries []ManualDomainEntry, opts ManualRenderOptions) (ManualRenderResult, error) {
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
		certsDir = DefaultFerronCertsDir
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
	cacheDir := filepath.ToSlash(certsDir)
	renewWindow := e.manualRenewWindow
	if renewWindow <= 0 {
		renewWindow = DefaultManualCertRenewWindow
	}

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

		if hasRedir {
			activeHostCount++
			writeFerronManualRedirect(&b, eEntry, redirFrom, redirTo, acmeEmail, cacheDir)
			b.WriteByte('\n')
		}

		if len(serveHosts) == 0 {
			continue
		}
		activeHostCount += len(serveHosts)

		switch eEntry.TLS {
		case ManualTLSNone:
			writeFerronManualSiteBlock(&b, eEntry, serveHosts, trustCF, acmeEmail, cacheDir, "", "")
			b.WriteByte('\n')
		case ManualTLSManual:
			writeFerronManualSiteBlock(&b, eEntry, serveHosts, trustCF, acmeEmail, cacheDir, filepath.ToSlash(eEntry.CertFile), filepath.ToSlash(eEntry.KeyFile))
			b.WriteByte('\n')
		default: // ManualTLSAuto
			groups := partitionAngieRouteTLSGroups(manualCertsDir, serveHosts, now, renewWindow)
			for _, grp := range groups {
				writeFerronManualSiteBlock(&b, eEntry, grp.hosts, trustCF, acmeEmail, cacheDir, grp.manualCert, grp.manualKey)
				b.WriteByte('\n')
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

func writeFerronManualRedirect(b *strings.Builder, e ManualDomainEntry, fromHost, toHost, acmeEmail, cacheDir string) {
	if e.TLS == ManualTLSNone {
		fmt.Fprintf(b, "http %s:80 {\n", fromHost)
		b.WriteString("    status 308 {\n")
		fmt.Fprintf(b, "        location \"http://%s{{request.uri}}\"\n", toHost)
		b.WriteString("    }\n}\n")
		return
	}

	b.WriteString(fromHost + " {\n")
	writeFerronManualTLSBlock(b, e.TLS, acmeEmail, cacheDir, filepath.ToSlash(e.CertFile), filepath.ToSlash(e.KeyFile))
	b.WriteString("    status 308 {\n")
	fmt.Fprintf(b, "        location \"https://%s{{request.uri}}\"\n", toHost)
	b.WriteString("    }\n}\n")
}

func writeFerronManualTLSBlock(b *strings.Builder, tlsMode, acmeEmail, cacheDir, certPath, keyPath string) {
	if certPath != "" && keyPath != "" {
		b.WriteString("    tls {\n")
		b.WriteString("        provider manual\n")
		b.WriteString("        cert \"" + certPath + "\"\n")
		b.WriteString("        key \"" + keyPath + "\"\n")
		b.WriteString("    }\n")
		return
	}
	if tlsMode == ManualTLSNone {
		return
	}
	b.WriteString("    tls {\n")
	b.WriteString("        provider acme\n")
	b.WriteString("        challenge http-01\n")
	if acmeEmail != "" {
		b.WriteString("        contact \"" + acmeEmail + "\"\n")
	}
	b.WriteString("        cache \"" + cacheDir + "\"\n")
	b.WriteString("    }\n")
}

func writeFerronManualSiteBlock(b *strings.Builder, e ManualDomainEntry, hosts []string, trustCF bool, acmeEmail, cacheDir, certPath, keyPath string) {
	if e.TLS == ManualTLSNone {
		specs := make([]string, len(hosts))
		for i, h := range hosts {
			specs[i] = "http " + h + ":80"
		}
		b.WriteString(strings.Join(specs, ", ") + " {\n")
	} else {
		b.WriteString(strings.Join(hosts, ", ") + " {\n")
	}
	b.WriteString("    use nd_common\n")

	if e.TLS != ManualTLSNone {
		if !e.EffectiveForceHTTPS() {
			b.WriteString("    https_redirect false\n")
		}
		writeFerronManualTLSBlock(b, e.TLS, acmeEmail, cacheDir, certPath, keyPath)
	}

	fmt.Fprintf(b, "    http {\n        timeout \"%ds\"\n    }\n", e.EffectiveTimeoutSec())

	if e.MaxBodyBytes > 0 {
		fmt.Fprintf(b, "    # note: Ferron 3 does not enforce per-route max_body (%d bytes)\n", e.MaxBodyBytes)
	}
	for _, h := range e.HeadersDown {
		fmt.Fprintf(b, "    header %s \"%s\"\n", h.Name, escapeQuotedConfigValue(h.Value))
	}

	b.WriteString("    proxy {\n")
	for _, u := range e.Upstreams {
		b.WriteString("        upstream \"" + u.URL() + "\" {\n")
		b.WriteString("            limit 256\n")
		b.WriteString("            idle_timeout \"90s\"\n")
		fmt.Fprintf(b, "            connection_timeout \"%ds\"\n", e.EffectiveConnectTimeoutSec())
		b.WriteString("        }\n")
	}

	if len(e.Upstreams) > 1 {
		switch e.EffectiveLB() {
		case ManualLBLeastConn:
			b.WriteString("        algorithm least_conn\n")
		case ManualLBIPHash:
			b.WriteString("        affinity ip\n")
		default:
			b.WriteString("        algorithm round_robin\n")
		}
	}

	if e.EffectiveKeepalive() == 0 {
		b.WriteString("        keepalive false\n")
	} else {
		b.WriteString("        keepalive\n")
	}
	b.WriteString("        retry_connection\n")
	b.WriteString("        retry_interval \"0.25s\"\n")

	if manualUpstreamsUseHTTPS(e.Upstreams) {
		if e.Origin.NoVerify {
			b.WriteString("        no_verification\n")
		}
		if e.Origin.SNI != "" && e.Origin.SNI != e.Upstreams[0].Host {
			fmt.Fprintf(b, "        # note: Ferron 3 uses the upstream URL host for SNI (--sni %s requested)\n", e.Origin.SNI)
		}
	}

	switch e.EffectiveHostHeader() {
	case ManualHostHeaderOriginal:
		// Ferron preserves original Host header by default.
	case ManualHostHeaderUpstream:
		fmt.Fprintf(b, "        request_header Host \"%s\"\n", e.Upstreams[0].HostPort())
	default:
		fmt.Fprintf(b, "        request_header Host \"%s\"\n", escapeQuotedConfigValue(e.EffectiveHostHeader()))
	}

	if e.TLS == ManualTLSNone {
		b.WriteString("        request_header X-Forwarded-Proto \"http\"\n")
	} else {
		b.WriteString("        request_header X-Forwarded-Proto \"https\"\n")
	}

	if !trustCF {
		b.WriteString("        request_header X-Real-IP \"{{remote.ip}}\"\n")
		b.WriteString("        request_header X-Forwarded-For \"{{remote.ip}}\"\n")
		b.WriteString("        request_header -CF-Connecting-IP\n")
	}
	if !e.EffectiveWebsocket() {
		b.WriteString("        request_header -Upgrade\n")
		b.WriteString("        request_header -Connection\n")
	}
	for _, h := range e.HeadersUp {
		fmt.Fprintf(b, "        request_header %s \"%s\"\n", h.Name, escapeQuotedConfigValue(h.Value))
	}

	b.WriteString("    }\n")
	b.WriteString("}\n")
}
