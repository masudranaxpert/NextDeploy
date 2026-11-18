package caddy

import (
	"fmt"
	"strconv"
	"strings"

	"panel/internal/proxy"
)

func applySitePostOverrides(labels map[string]string, metas []siteDomainMeta, up proxy.UpstreamRef, isSlot bool) {
	for i, m := range metas {
		prefix := "caddy"
		if isSlot {
			if i > 0 {
				prefix = "caddy_" + strconv.Itoa(i)
			}
		} else if len(metas) > 1 {
			prefix = "caddy_" + strconv.Itoa(i)
		}
		applySingleSitePostOverrides(labels, prefix, m.site, up, m.extraHosts, isSlot)
	}
}

func applySingleSitePostOverrides(labels map[string]string, prefix string, s proxy.Site, up proxy.UpstreamRef, extraHosts []string, isSlot bool) {
	if len(extraHosts) > 0 {
		formatted := make([]string, 0, len(extraHosts))
		https := s.TLS != proxy.TLSNone || s.BehindShield
		for _, h := range extraHosts {
			if https || strings.HasPrefix(h, "http://") || strings.HasPrefix(h, "https://") {
				formatted = append(formatted, h)
			} else {
				formatted = append(formatted, "http://"+h)
			}
		}
		labels[prefix] = strings.Join(formatted, ", ")
	}
	if s.TLS == proxy.TLSInternal && !s.BehindShield {
		labels[prefix+".tls"] = "internal"
	}
	if rpKey := findReverseProxyKey(labels, prefix); rpKey != "" {
		reqHeaders := resolveRequestHeaders(s, up)
		if (s.TrustedProxy || s.BehindShield) && len(reqHeaders) > 0 {
			applyCustomRequestHeaders(labels, rpKey, reqHeaders)
		}
		if effUp := resolveEffectiveUpstream(s, up); effUp != nil {
			if strings.EqualFold(effUp.Scheme, "https") && strings.TrimSpace(effUp.External) == "" &&
				!strings.HasPrefix(strings.ToLower(labels[rpKey]), "https://") {
				applyProxyTLSTransport(labels, rpKey, effUp.SkipVerify)
			} else if effUp.SkipVerify && strings.HasPrefix(strings.ToLower(labels[rpKey]), "https://") {
				applyProxySkipVerifyOnly(labels, rpKey)
			}
		}
		if isSlot {
			if tryDur := resolveLBTryDuration(s, up); tryDur != "" {
				labels[rpKey+".lb_try_duration"] = tryDur
			}
			if failDur := resolveFailDuration(s, up); failDur != "" {
				labels[rpKey+".fail_duration"] = failDur
			}
		}
	}
	for _, op := range s.Headers {
		applySiteHeaderOp(labels, prefix, op)
	}
}

func resolveEffectiveUpstream(s proxy.Site, up proxy.UpstreamRef) *proxy.UpstreamRef {
	if s.Upstream != nil {
		return s.Upstream
	}
	if s.Default != nil && s.Default.Upstream != nil {
		return s.Default.Upstream
	}
	return &up
}

func findReverseProxyKey(labels map[string]string, prefix string) string {
	direct := prefix + ".reverse_proxy"
	if _, ok := labels[direct]; ok {
		return direct
	}
	head := prefix + "."
	bestNum := -1
	bestKey := ""
	for k := range labels {
		if !strings.HasPrefix(k, head) || !strings.HasSuffix(k, "_reverse_proxy") {
			continue
		}
		mid := strings.TrimSuffix(strings.TrimPrefix(k, head), "_reverse_proxy")
		if n, err := strconv.Atoi(mid); err == nil && n > bestNum {
			bestNum = n
			bestKey = k
		}
	}
	return bestKey
}

func applyCustomRequestHeaders(labels map[string]string, rpBase string, ops []proxy.HeaderOp) {
	for i, op := range ops {
		labels[fmt.Sprintf("%s.header_up_%d", rpBase, i)] = formatHeaderUp(op)
	}
}

func applyProxyTLSTransport(labels map[string]string, rpBase string, skipVerify bool) {
	labels[rpBase+".transport"] = "http"
	labels[rpBase+".transport.tls"] = `{{""}}`
	if skipVerify {
		labels[rpBase+".transport.tls_insecure_skip_verify"] = `{{""}}`
	}
}

func applyProxySkipVerifyOnly(labels map[string]string, rpBase string) {
	labels[rpBase+".transport"] = "http"
	labels[rpBase+".transport.tls_insecure_skip_verify"] = `{{""}}`
}

func formatHeaderUp(op proxy.HeaderOp) string {
	switch op.Action {
	case proxy.HeaderDelete:
		return "-" + op.Name
	case proxy.HeaderAdd:
		return "+" + op.Name + " " + op.Value
	default:
		return op.Name + " " + op.Value
	}
}

func applySiteHeaderOp(labels map[string]string, prefix string, op proxy.HeaderOp) {
	if strings.TrimSpace(op.Name) == "" {
		return
	}
	switch op.Action {
	case proxy.HeaderDelete:
		labels[prefix+".header.-"+op.Name] = `{{""}}`
	case proxy.HeaderAdd:
		labels[prefix+".header.+"+op.Name] = op.Value
	default:
		key := prefix + ".header." + op.Name
		if _, exists := labels[key]; !exists {
			labels[key] = op.Value
		}
	}
}

func applyUntrustedHeaders(labels map[string]string, base string) {
	labels[base+".header_up_0"] = "X-Real-IP {remote_host}"
	labels[base+".header_up_1"] = "X-Forwarded-For {remote_host}"
	labels[base+".header_up_2"] = "-CF-Connecting-IP"
}

func resolveUpstreamPort(s proxy.Site, up proxy.UpstreamRef) int {
	if s.Upstream != nil && s.Upstream.Port > 0 {
		return s.Upstream.Port
	}
	if s.Default != nil && s.Default.Upstream != nil && s.Default.Upstream.Port > 0 {
		return s.Default.Upstream.Port
	}
	return up.Port
}

func resolveUpstreamService(s proxy.Site, up proxy.UpstreamRef) string {
	if s.Upstream != nil && s.Upstream.Service != "" {
		return s.Upstream.Service
	}
	if s.Default != nil && s.Default.Upstream != nil && s.Default.Upstream.Service != "" {
		return s.Default.Upstream.Service
	}
	return up.Service
}

func resolveExternalTarget(s proxy.Site, up proxy.UpstreamRef) string {
	if s.Upstream != nil && s.Upstream.External != "" {
		return s.Upstream.External
	}
	if s.Default != nil && s.Default.Upstream != nil && s.Default.Upstream.External != "" {
		return s.Default.Upstream.External
	}
	return up.External
}

func resolveRedirectSpec(s proxy.Site) *proxy.RedirectSpec {
	if s.Redirect != nil {
		return s.Redirect
	}
	if s.Default != nil && s.Default.Redirect != nil {
		return s.Default.Redirect
	}
	return nil
}

func resolveRequestHeaders(s proxy.Site, up proxy.UpstreamRef) []proxy.HeaderOp {
	if s.Upstream != nil && len(s.Upstream.RequestHeaders) > 0 {
		return s.Upstream.RequestHeaders
	}
	if s.Default != nil && s.Default.Upstream != nil && len(s.Default.Upstream.RequestHeaders) > 0 {
		return s.Default.Upstream.RequestHeaders
	}
	return up.RequestHeaders
}

func resolveLBTryDuration(s proxy.Site, up proxy.UpstreamRef) string {
	if s.Upstream != nil && s.Upstream.LBTryDuration != "" {
		return s.Upstream.LBTryDuration
	}
	return up.LBTryDuration
}

func resolveFailDuration(s proxy.Site, up proxy.UpstreamRef) string {
	if s.Upstream != nil && s.Upstream.FailDuration != "" {
		return s.Upstream.FailDuration
	}
	return up.FailDuration
}
