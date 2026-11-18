package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// ComputeGeneration returns a deterministic SHA-256 hex digest of the canonical
// JSON representation of s. It never mutates s and ignores any existing
// s.Generation value so repeated builds of equivalent state yield the same hash.
func (s *State) ComputeGeneration() string {
	canon := canonicalState(s)
	data, _ := json.Marshal(canon)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func canonicalState(s *State) State {
	if s == nil {
		return State{}
	}
	out := State{
		Global:       canonicalGlobalOptions(s.Global),
		ACMEEmail:    s.ACMEEmail,
		Panel:        canonicalSitePtr(s.Panel),
		Sites:        nilIfEmptySites(s.Sites),
		Gateways:     nilIfEmptyGateways(s.Gateways),
		SharedMounts: nilIfEmptyMounts(s.SharedMounts),
	}
	sort.Slice(out.Sites, func(i, j int) bool {
		if out.Sites[i].ID != out.Sites[j].ID {
			return out.Sites[i].ID < out.Sites[j].ID
		}
		hi := strings.Join(out.Sites[i].Hosts, ",")
		hj := strings.Join(out.Sites[j].Hosts, ",")
		if hi != hj {
			return hi < hj
		}
		if out.Sites[i].Kind != out.Sites[j].Kind {
			return out.Sites[i].Kind < out.Sites[j].Kind
		}
		return out.Sites[i].Slot < out.Sites[j].Slot
	})
	sort.Slice(out.Gateways, func(i, j int) bool {
		a, b := out.Gateways[i], out.Gateways[j]
		if a.Port != b.Port {
			return a.Port < b.Port
		}
		if a.BindIP != b.BindIP {
			return a.BindIP < b.BindIP
		}
		if a.Protocol != b.Protocol {
			return a.Protocol < b.Protocol
		}
		if a.AppID != b.AppID {
			return a.AppID < b.AppID
		}
		if a.ContainerPort != b.ContainerPort {
			return a.ContainerPort < b.ContainerPort
		}
		return !a.Direct && b.Direct
	})
	sort.Slice(out.SharedMounts, func(i, j int) bool {
		a, b := out.SharedMounts[i], out.SharedMounts[j]
		if a.VolumeName != b.VolumeName {
			return a.VolumeName < b.VolumeName
		}
		if a.Target != b.Target {
			return a.Target < b.Target
		}
		return !a.ReadOnly && b.ReadOnly
	})
	return out
}

func canonicalGlobalOptions(g GlobalOptions) GlobalOptions {
	g.TrustedProxies = sortedStringCopy(g.TrustedProxies)
	g.ShieldProxies = sortedStringCopy(g.ShieldProxies)
	return g
}

func canonicalSitePtr(in *Site) *Site {
	if in == nil {
		return nil
	}
	cp := canonicalSite(*in)
	return &cp
}

func canonicalSite(s Site) Site {
	s.Hosts = copyStringsOrNil(s.Hosts)
	s.Headers = canonicalHeaders(s.Headers)
	s.Paths = canonicalPathRoutes(s.Paths)
	s.Default = canonicalHandlerPtr(s.Default)
	s.PHP = canonicalPHPSpec(s.PHP)
	s.Security = canonicalSecuritySpec(s.Security)
	s.Redirect = canonicalRedirectSpec(s.Redirect)
	s.Upstream = canonicalUpstreamPtr(s.Upstream)
	return s
}

func canonicalHeaders(in []HeaderOp) []HeaderOp {
	if len(in) == 0 {
		return nil
	}
	out := append([]HeaderOp(nil), in...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		if out[i].Action != out[j].Action {
			return out[i].Action < out[j].Action
		}
		return out[i].Value < out[j].Value
	})
	return out
}

func canonicalPathRoutes(in []PathRoute) []PathRoute {
	if len(in) == 0 {
		return nil
	}
	// Preserve caller-supplied PathRoute order (priority/longest-prefix semantics).
	out := make([]PathRoute, len(in))
	for i, pr := range in {
		pr.Handler = canonicalHandler(pr.Handler)
		out[i] = pr
	}
	return out
}

func canonicalHandlerPtr(in *Handler) *Handler {
	if in == nil {
		return nil
	}
	cp := canonicalHandler(*in)
	return &cp
}

func canonicalHandler(h Handler) Handler {
	if h.Static != nil {
		st := *h.Static
		st.Hide = sortedStringCopy(st.Hide)
		st.Precompressed = sortedStringCopy(st.Precompressed)
		h.Static = &st
	}
	h.Upstream = canonicalUpstreamPtr(h.Upstream)
	h.PHP = canonicalPHPSpec(h.PHP)
	h.Redirect = canonicalRedirectSpec(h.Redirect)
	return h
}

func canonicalUpstreamPtr(in *UpstreamRef) *UpstreamRef {
	if in == nil {
		return nil
	}
	cp := *in
	cp.RequestHeaders = canonicalHeaders(cp.RequestHeaders)
	return &cp
}

func canonicalPHPSpec(in *PHPSpec) *PHPSpec {
	if in == nil {
		return nil
	}
	cp := *in
	if len(in.Routing) == 0 {
		cp.Routing = nil
	} else {
		// Preserve ordered routing rule sequence.
		cp.Routing = append([]RoutingRule(nil), in.Routing...)
	}
	cp.Compression = sortedStringCopy(in.Compression)
	cp.Precompressed = sortedStringCopy(in.Precompressed)
	cp.HidePatterns = sortedStringCopy(in.HidePatterns)
	if in.Cache != nil {
		cacheCopy := *in.Cache
		cp.Cache = &cacheCopy
	}
	return &cp
}

func canonicalSecuritySpec(in *SecuritySpec) *SecuritySpec {
	if in == nil {
		return nil
	}
	cp := *in
	cp.BlockedIPs = sortedStringCopy(in.BlockedIPs)
	cp.AllowOnly = sortedStringCopy(in.AllowOnly)
	return &cp
}

func canonicalRedirectSpec(in *RedirectSpec) *RedirectSpec {
	if in == nil {
		return nil
	}
	cp := *in
	return &cp
}

func nilIfEmptySites(in []Site) []Site {
	if len(in) == 0 {
		return nil
	}
	out := make([]Site, len(in))
	for i, s := range in {
		out[i] = canonicalSite(s)
	}
	return out
}

func nilIfEmptyGateways(in []GatewayPort) []GatewayPort {
	if len(in) == 0 {
		return nil
	}
	out := make([]GatewayPort, len(in))
	for i, g := range in {
		g.Upstream = canonicalUpstreamPtr(g.Upstream)
		out[i] = g
	}
	return out
}

func nilIfEmptyMounts(in []SharedMount) []SharedMount {
	if len(in) == 0 {
		return nil
	}
	return append([]SharedMount(nil), in...)
}

func copyStringsOrNil(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	return append([]string(nil), in...)
}

func sortedStringCopy(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
