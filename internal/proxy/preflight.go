package proxy

import (
	"fmt"
	"sort"
	"strings"
)

// AffectedSite identifies a site in State impacted by a missing proxy capability.
type AffectedSite struct {
	SiteID string `json:"site_id"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
}

// PreflightIssue describes a single capability gap found when evaluating a State
// against a target engine's Capabilities.
type PreflightIssue struct {
	Capability    string         `json:"capability"`
	Message       string         `json:"message"`
	AffectedSites []AffectedSite `json:"affected_sites,omitempty"`
}

// PreflightDiff summarizes hard compatibility blocks and non-fatal warnings
// returned by EvaluateCapabilities.
type PreflightDiff struct {
	CanSwitch               bool             `json:"can_switch"`
	RequiresExperimentalAck bool             `json:"requires_experimental_ack"`
	HardBlocks              []PreflightIssue `json:"hard_blocks,omitempty"`
	SoftWarnings            []PreflightIssue `json:"soft_warnings,omitempty"`
}

type issueAccumulator struct {
	order  []string
	issues map[string]*PreflightIssue
	seen   map[string]map[string]bool
}

func newIssueAccumulator() *issueAccumulator {
	return &issueAccumulator{
		issues: make(map[string]*PreflightIssue),
		seen:   make(map[string]map[string]bool),
	}
}

func (a *issueAccumulator) add(capKey, msg string, site *AffectedSite) {
	iss, ok := a.issues[capKey]
	if !ok {
		iss = &PreflightIssue{Capability: capKey, Message: msg}
		a.issues[capKey] = iss
		a.seen[capKey] = make(map[string]bool)
		a.order = append(a.order, capKey)
	}
	if site == nil || site.SiteID == "" {
		return
	}
	if a.seen[capKey][site.SiteID] {
		return
	}
	a.seen[capKey][site.SiteID] = true
	iss.AffectedSites = append(iss.AffectedSites, *site)
}

func (a *issueAccumulator) list() []PreflightIssue {
	if len(a.order) == 0 {
		return nil
	}
	out := make([]PreflightIssue, 0, len(a.order))
	for _, k := range a.order {
		iss := *a.issues[k]
		if len(iss.AffectedSites) > 1 {
			sort.Slice(iss.AffectedSites, func(i, j int) bool {
				return iss.AffectedSites[i].SiteID < iss.AffectedSites[j].SiteID
			})
		}
		out = append(out, iss)
	}
	return out
}

func siteLabel(s Site) string {
	if len(s.Hosts) > 0 && strings.TrimSpace(s.Hosts[0]) != "" {
		return s.Hosts[0]
	}
	return s.ID
}

// EvaluateCapabilities checks whether st can be served by a target engine exposing
// target Capabilities. Unsupported features required by active sites or strict global
// security settings produce HardBlocks; degraded operational behavior or ignored
// optional limits produce SoftWarnings.
func EvaluateCapabilities(st *State, target Capabilities) PreflightDiff {
	if st == nil {
		st = &State{}
	}
	blocks := newIssueAccumulator()
	warnings := newIssueAccumulator()

	sites := make([]Site, 0, len(st.Sites)+1)
	if st.Panel != nil {
		sites = append(sites, *st.Panel)
	}
	sites = append(sites, st.Sites...)

	var trustedSites []AffectedSite
	for _, s := range sites {
		ref := AffectedSite{SiteID: s.ID, Label: siteLabel(s)}
		if s.TrustedProxy || s.BehindShield {
			trustedSites = append(trustedSites, ref)
		}

		switch s.TLS {
		case TLSAuto:
			if !target.AutoTLS {
				blocks.add("auto_tls", "Target proxy does not support automatic ACME TLS certificates.", &ref)
			}
		case TLSInternal:
			if !target.InternalTLS {
				blocks.add("internal_tls", "Target proxy does not support internal/private CA TLS certificates.", &ref)
			}
		}
		if s.OnDemandTLS && !target.OnDemandTLS {
			blocks.add("on_demand_tls", "Target proxy does not support on-demand SNI TLS issuance.", &ref)
		}

		if s.Kind == SiteKindPHP && !target.PHPFastCGI {
			blocks.add("php_fastcgi", "Target proxy does not support PHP-FPM FastCGI execution.", &ref)
		}

		if s.Security != nil && s.Security.BasicAuthUser != "" && s.Security.BasicAuthHash != "" && !target.BasicAuthBcrypt {
			blocks.add("basic_auth_bcrypt", "Target proxy does not support bcrypt HTTP basic authentication.", &ref)
		}

		if s.PHP != nil {
			for _, rule := range s.PHP.Routing {
				if !rule.Enabled {
					continue
				}
				if rule.MatchType == "regex" && !target.RegexRouting {
					rRef := ref
					rRef.Detail = fmt.Sprintf("rule %q (%s)", rule.ID, rule.Pattern)
					blocks.add("regex_routing", "Target proxy does not support regex URL routing rules.", &rRef)
				}
				if rule.RequireTargetFile && !target.RequireTargetFile {
					rRef := ref
					rRef.Detail = fmt.Sprintf("rule %q (%s -> %s)", rule.ID, rule.Pattern, rule.Target)
					blocks.add("require_target_file", "Target proxy does not support require_target_file (.php file-exists) rewrite conditions.", &rRef)
				}
			}
		}

		if !target.LongStreamsOnReload && siteHasLongStreams(s) {
			warnings.add("long_streams_on_reload", "Target proxy may close HTTP/2 streams on reload instead of draining long-lived connections.", &ref)
		}
	}

	evaluateGlobalCapabilities(st.Global, target, trustedSites, blocks, warnings)

	diff := PreflightDiff{
		RequiresExperimentalAck: target.Experimental,
		HardBlocks:              blocks.list(),
		SoftWarnings:            warnings.list(),
	}
	diff.CanSwitch = len(diff.HardBlocks) == 0
	return diff
}

func siteHasLongStreams(s Site) bool {
	if s.Upstream != nil && s.Upstream.LongStreams {
		return true
	}
	if s.Default != nil && s.Default.Upstream != nil && s.Default.Upstream.LongStreams {
		return true
	}
	for _, p := range s.Paths {
		if p.Handler.Upstream != nil && p.Handler.Upstream.LongStreams {
			return true
		}
	}
	return false
}

func evaluateGlobalCapabilities(g GlobalOptions, target Capabilities, trustedSites []AffectedSite, blocks, warnings *issueAccumulator) {
	if target.Experimental {
		warnings.add("experimental", "Target proxy engine is marked experimental and requires explicit acknowledgment.", nil)
	}

	if !target.H2IdleTimeout {
		msg := "Target proxy does not close idle HTTP/2 connections (Cloudflare closes idle origin connections after 900s)."
		if g.Idle != "" {
			msg = fmt.Sprintf("Configured keep-alive idle timeout (%s) applies only partially or is ignored: target proxy does not close idle HTTP/2 connections.", g.Idle)
		}
		warnings.add("h2_idle_timeout", msg, nil)
	}

	if !target.WriteTimeout {
		warnings.add("write_timeout", "Target proxy has no write/slow-reader response timeout.", nil)
	}

	if !target.ReadHeaderTimeout && g.ReadHeader != "" {
		warnings.add("read_header_timeout", fmt.Sprintf("Configured header read timeout (%s) will be ignored by the target proxy.", g.ReadHeader), nil)
	}

	if !target.ReadBodyTimeout && g.ReadBodyIdle != "" {
		warnings.add("read_body_timeout", fmt.Sprintf("Configured upload body idle timeout (%s) is not supported by the target proxy.", g.ReadBodyIdle), nil)
	}

	if !target.GracePeriod && g.GracePeriod != "" {
		warnings.add("grace_period", fmt.Sprintf("Configured reload grace period (%s) is not configurable on the target proxy.", g.GracePeriod), nil)
	}

	if !target.MaxHeaderSizeHTTP1 && g.MaxHeaderSize != "" {
		warnings.add("max_header_size_http1", fmt.Sprintf("Configured max_header_size (%s) applies to HTTP/2 only; HTTP/1.1 header limits remain fixed on the target proxy.", g.MaxHeaderSize), nil)
	}

	if !target.TrustedStrict {
		if g.TrustedStrict {
			addSitesOrGlobal(blocks, "trusted_strict", "Strict right-to-left X-Forwarded-For recursion is enabled, but the target proxy only reads the leftmost forwarded IP.", trustedSites)
		} else if g.RealIP == "cloudflare" || g.DirectCloudflare || g.RealIP == "custom" || len(g.ShieldProxies) > 0 {
			addSitesOrGlobal(warnings, "trusted_strict", "Target proxy reads the leftmost X-Forwarded-For IP and ignores CF-Connecting-IP; client IP headers can be spoofed if upstream proxies append rather than overwrite.", trustedSites)
		}
	}
}

func addSitesOrGlobal(acc *issueAccumulator, capKey, msg string, sites []AffectedSite) {
	if len(sites) == 0 {
		acc.add(capKey, msg, nil)
		return
	}
	for i := range sites {
		s := sites[i]
		acc.add(capKey, msg, &s)
	}
}
