package edge

import (
	"net"
	"sort"
	"strings"
	"time"
)

const (
	ManualPanelID       = "zz-manual"
	ManualCaddyFileName = "zz-manual.caddy"
	ManualConfFileName  = "zz-manual.conf"
)

// ManualRenderOptions supplies reconciler/runtime context for manual-domain rendering.
type ManualRenderOptions struct {
	Now             time.Time
	ACMEEmail       string
	ManualCertsDir  string
	CertsDir        string
	DNSBlockedHosts map[string]bool
	CFProxiedHosts  map[string]bool
	ShieldTrustsCF  bool
}

// ManualRenderResult captures the rendered zz-manual file and aggregate metadata.
type ManualRenderResult struct {
	FileName        string
	Content         string
	AnyTrustedProxy bool
	ActiveHostCount int
}

// ManualEngine is implemented by Engine backends supporting managed manual domains.
type ManualEngine interface {
	RenderManual(entries []ManualDomainEntry, opts ManualRenderOptions) (ManualRenderResult, error)
}

// RenderManualForEngine renders zz-manual using engine if it implements ManualEngine,
// or falls back cleanly to RenderManualCaddy.
func RenderManualForEngine(engine Engine, entries []ManualDomainEntry, opts ManualRenderOptions) (ManualRenderResult, error) {
	if me, ok := engine.(ManualEngine); ok {
		return me.RenderManual(entries, opts)
	}
	return RenderManualCaddy(entries, opts)
}

func prepareManualEntriesForRender(entries []ManualDomainEntry) []ManualDomainEntry {
	out := make([]ManualDomainEntry, 0, len(entries))
	for _, e := range entries {
		if !e.Enabled || len(e.Upstreams) == 0 {
			continue
		}
		cp := e
		cp.Aliases = append([]string(nil), e.Aliases...)
		sort.Strings(cp.Aliases)
		cp.HeadersUp = append([]ManualHeader(nil), e.HeadersUp...)
		sort.Slice(cp.HeadersUp, func(i, j int) bool {
			return cp.HeadersUp[i].Name < cp.HeadersUp[j].Name
		})
		cp.HeadersDown = append([]ManualHeader(nil), e.HeadersDown...)
		sort.Slice(cp.HeadersDown, func(i, j int) bool {
			return cp.HeadersDown[i].Name < cp.HeadersDown[j].Name
		})
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Domain < out[j].Domain
	})
	return out
}

func filterManualHostsByDNS(hosts []string, tlsMode string, blocked map[string]bool) []string {
	if tlsMode != ManualTLSAuto || len(blocked) == 0 {
		return hosts
	}
	kept := make([]string, 0, len(hosts))
	for _, h := range hosts {
		if !blocked[h] {
			kept = append(kept, h)
		}
	}
	return kept
}

func manualEntryTrustsCF(e ManualDomainEntry, opts ManualRenderOptions) bool {
	switch e.RealIP {
	case ManualRealIPCloudflare:
		return true
	case ManualRealIPNone:
		return false
	}
	if opts.ShieldTrustsCF {
		return true
	}
	for _, h := range e.AllHosts() {
		if opts.CFProxiedHosts[h] {
			return true
		}
	}
	return false
}

func manualUpstreamsUseHTTPS(ups []ManualUpstream) bool {
	for _, u := range ups {
		if u.Scheme == "https" {
			return true
		}
	}
	return false
}

func manualOriginSNI(e ManualDomainEntry) string {
	if e.Origin.SNI != "" {
		return e.Origin.SNI
	}
	if len(e.Upstreams) > 0 && net.ParseIP(e.Upstreams[0].Host) == nil {
		return e.Upstreams[0].Host
	}
	return "$host"
}

func escapeQuotedConfigValue(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x20 && r != 0x7f && r != '"' && r != '\\' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
