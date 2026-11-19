package edge

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// RenderManual renders enabled manual domains into Caddy syntax (zz-manual.caddy).
func (e *CaddyEngine) RenderManual(entries []ManualDomainEntry, opts ManualRenderOptions) (ManualRenderResult, error) {
	if strings.TrimSpace(opts.ACMEEmail) == "" {
		opts.ACMEEmail = e.acmeEmail
	}
	return RenderManualCaddy(entries, opts)
}

// RenderManualCaddy renders enabled manual domains into deterministic Caddyfile blocks.
func RenderManualCaddy(entries []ManualDomainEntry, opts ManualRenderOptions) (ManualRenderResult, error) {
	active := prepareManualEntriesForRender(entries)

	var b strings.Builder
	b.WriteString("# Managed by nd edge (manual domains). DO NOT EDIT.\n")

	if len(active) == 0 {
		return ManualRenderResult{
			FileName: ManualCaddyFileName,
			Content:  b.String(),
		}, nil
	}

	anyTrusted := false
	activeHostCount := 0

	for _, e := range active {
		serveHosts := filterManualHostsByDNS(e.PrimaryServedHosts(), e.TLS, opts.DNSBlockedHosts)
		redirFrom, redirTo, hasRedir := e.RedirectPair()
		if hasRedir && e.TLS == ManualTLSAuto && opts.DNSBlockedHosts[redirFrom] {
			hasRedir = false
		}
		if len(serveHosts) == 0 && !hasRedir {
			continue
		}

		trustCF := manualEntryTrustsCF(e, opts)
		if trustCF {
			anyTrusted = true
		}

		if hasRedir {
			activeHostCount++
			writeCaddyManualRedirectBlock(&b, e, redirFrom, redirTo)
			b.WriteByte('\n')
		}

		if len(serveHosts) == 0 {
			continue
		}
		activeHostCount += len(serveHosts)

		switch {
		case e.TLS == ManualTLSNone:
			addrs := make([]string, len(serveHosts))
			for i, h := range serveHosts {
				addrs[i] = "http://" + h
			}
			b.WriteString(strings.Join(addrs, ", ") + " {\n")
			writeCaddyManualProxyDirectives(&b, e, trustCF)
			b.WriteString("}\n\n")

		case !e.EffectiveForceHTTPS():
			snippetName := "nd_" + e.ID
			fmt.Fprintf(&b, "(%s) {\n", snippetName)
			writeCaddyManualProxyDirectives(&b, e, trustCF)
			b.WriteString("}\n\n")

			httpAddrs := make([]string, len(serveHosts))
			for i, h := range serveHosts {
				httpAddrs[i] = "http://" + h
			}
			b.WriteString(strings.Join(httpAddrs, ", ") + " {\n")
			b.WriteString("\timport " + snippetName + "\n")
			b.WriteString("}\n\n")

			b.WriteString(strings.Join(serveHosts, ", ") + " {\n")
			if e.TLS == ManualTLSManual {
				fmt.Fprintf(&b, "\ttls %s %s\n", filepath.ToSlash(e.CertFile), filepath.ToSlash(e.KeyFile))
			}
			b.WriteString("\timport " + snippetName + "\n")
			b.WriteString("}\n\n")

		default:
			b.WriteString(strings.Join(serveHosts, ", ") + " {\n")
			if e.TLS == ManualTLSManual {
				fmt.Fprintf(&b, "\ttls %s %s\n", filepath.ToSlash(e.CertFile), filepath.ToSlash(e.KeyFile))
			}
			writeCaddyManualProxyDirectives(&b, e, trustCF)
			b.WriteString("}\n\n")
		}
	}

	return ManualRenderResult{
		FileName:        ManualCaddyFileName,
		Content:         b.String(),
		AnyTrustedProxy: anyTrusted,
		ActiveHostCount: activeHostCount,
	}, nil
}

func writeCaddyManualRedirectBlock(b *strings.Builder, e ManualDomainEntry, fromHost, toHost string) {
	if e.TLS == ManualTLSNone {
		fmt.Fprintf(b, "http://%s {\n\tredir http://%s{uri} 308\n}\n", fromHost, toHost)
		return
	}
	b.WriteString(fromHost + " {\n")
	if e.TLS == ManualTLSManual {
		fmt.Fprintf(b, "\ttls %s %s\n", filepath.ToSlash(e.CertFile), filepath.ToSlash(e.KeyFile))
	}
	fmt.Fprintf(b, "\tredir https://%s{uri} 308\n", toHost)
	b.WriteString("}\n")
}

func writeCaddyManualProxyDirectives(b *strings.Builder, e ManualDomainEntry, trustCF bool) {
	b.WriteString("\timport nd_common\n")
	if e.MaxBodyBytes > 0 {
		fmt.Fprintf(b, "\trequest_body {\n\t\tmax_size %d\n\t}\n", e.MaxBodyBytes)
	}
	for _, h := range e.HeadersDown {
		fmt.Fprintf(b, "\theader %s \"%s\"\n", h.Name, escapeQuotedConfigValue(h.Value))
	}

	targets := make([]string, len(e.Upstreams))
	for i, u := range e.Upstreams {
		targets[i] = u.URL()
	}
	fmt.Fprintf(b, "\treverse_proxy %s {\n", strings.Join(targets, " "))
	if len(targets) > 1 {
		b.WriteString("\t\tlb_policy " + e.EffectiveLB() + "\n")
	}

	switch e.EffectiveHostHeader() {
	case ManualHostHeaderOriginal:
		b.WriteString("\t\theader_up Host {host}\n")
	case ManualHostHeaderUpstream:
		b.WriteString("\t\theader_up Host {upstream_hostport}\n")
	default:
		fmt.Fprintf(b, "\t\theader_up Host \"%s\"\n", escapeQuotedConfigValue(e.EffectiveHostHeader()))
	}

	if trustCF {
		b.WriteString("\t\theader_up X-Real-IP {client_ip}\n")
		b.WriteString("\t\theader_up X-Forwarded-For {client_ip}\n")
	} else {
		b.WriteString("\t\theader_up X-Real-IP {remote_host}\n")
		b.WriteString("\t\theader_up X-Forwarded-For {remote_host}\n")
		b.WriteString("\t\theader_up -CF-Connecting-IP\n")
	}
	b.WriteString("\t\theader_up X-Forwarded-Proto {scheme}\n")

	if !e.EffectiveWebsocket() {
		b.WriteString("\t\theader_up -Upgrade\n")
		b.WriteString("\t\theader_up -Connection\n")
	}
	for _, h := range e.HeadersUp {
		fmt.Fprintf(b, "\t\theader_up %s \"%s\"\n", h.Name, escapeQuotedConfigValue(h.Value))
	}

	b.WriteString("\t\tflush_interval -1\n")
	b.WriteString("\t\ttransport http {\n")
	if manualUpstreamsUseHTTPS(e.Upstreams) {
		b.WriteString("\t\t\ttls\n")
		if e.Origin.NoVerify {
			b.WriteString("\t\t\ttls_insecure_skip_verify\n")
		}
		if sni := manualOriginSNI(e); sni == "$host" {
			b.WriteString("\t\t\ttls_server_name {host}\n")
		} else if sni != "" {
			b.WriteString("\t\t\ttls_server_name " + sni + "\n")
		}
	}
	fmt.Fprintf(b, "\t\t\tdial_timeout %ds\n", e.EffectiveConnectTimeoutSec())
	fmt.Fprintf(b, "\t\t\tresponse_header_timeout %ds\n", e.EffectiveTimeoutSec())
	ka := e.EffectiveKeepalive()
	if ka == 0 {
		b.WriteString("\t\t\tkeepalive off\n")
	} else {
		b.WriteString("\t\t\tkeepalive 90s\n")
		idleTotal := 256
		if ka > idleTotal {
			idleTotal = ka
		}
		b.WriteString("\t\t\tkeepalive_idle_conns " + strconv.Itoa(idleTotal) + "\n")
		b.WriteString("\t\t\tkeepalive_idle_conns_per_host " + strconv.Itoa(ka) + "\n")
	}
	b.WriteString("\t\t}\n")
	b.WriteString("\t}\n")
}
