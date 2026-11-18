package angie

import (
	"fmt"
	"strings"

	"panel/internal/proxy"
)

func (d *Driver) renderRedirectSite(site proxy.Site, tlsInfo tlsResolution, cfGlobal bool) (string, error) {
	hosts := cleanHosts(site.Hosts)
	if len(hosts) == 0 {
		return "", nil
	}
	spec := site.Redirect
	if spec == nil && site.Default != nil {
		spec = site.Default.Redirect
	}
	if spec == nil {
		return "", fmt.Errorf("angie redirect site %q: missing redirect spec", site.ID)
	}

	target, err := formatRedirectTarget(spec)
	if err != nil {
		return "", fmt.Errorf("angie redirect site %q: %w", site.ID, err)
	}
	code := normalizeRedirectCode(spec.Code)

	var body strings.Builder
	if err := writeServerHeaders(&body, site, tlsInfo.Mode != proxy.TLSNone && d.enableQUIC); err != nil {
		return "", err
	}
	fmt.Fprintf(&body, "    return %d %s;\n", code, target)

	return d.wrapServerBlocks(site, hosts, tlsInfo, cfGlobal, body.String()), nil
}

func (d *Driver) renderExternalProxySite(site proxy.Site, tlsInfo tlsResolution, cfGlobal bool) (string, error) {
	hosts := cleanHosts(site.Hosts)
	if len(hosts) == 0 {
		return "", nil
	}
	up := site.Upstream
	if up == nil && site.Default != nil {
		up = site.Default.Upstream
	}
	if up == nil || strings.TrimSpace(up.External) == "" {
		return "", fmt.Errorf("angie external proxy site %q: missing external upstream URL", site.ID)
	}

	var body strings.Builder
	if err := writeServerHeaders(&body, site, tlsInfo.Mode != proxy.TLSNone && d.enableQUIC); err != nil {
		return "", err
	}
	body.WriteString("    location / {\n")
	if err := writeProxyPassBlock(&body, "        ", *up, site.TrustedProxy, site.BehindShield); err != nil {
		return "", err
	}
	body.WriteString("    }\n")

	return d.wrapServerBlocks(site, hosts, tlsInfo, cfGlobal, body.String()), nil
}

func formatRedirectTarget(spec *proxy.RedirectSpec) (string, error) {
	target := strings.TrimSpace(spec.Target)
	if target == "" || proxy.HasUnsafeLabelChars(target) {
		return "", fmt.Errorf("invalid redirect target %q", spec.Target)
	}
	low := strings.ToLower(target)
	if !strings.HasPrefix(target, "/") && !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") {
		return "", fmt.Errorf("unsupported redirect target scheme %q", spec.Target)
	}
	if spec.KeepPath {
		if target == "/" {
			return "$request_uri", nil
		}
		trimmed := strings.TrimRight(target, "/")
		return trimmed + "$request_uri", nil
	}
	return target, nil
}

func normalizeRedirectCode(code int) int {
	switch code {
	case 301, 302, 307, 308:
		return code
	default:
		return 301
	}
}
