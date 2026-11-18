package ferron

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"panel/internal/proxy"
)

func (d *Driver) renderGatewaysFile(gateways []proxy.GatewayPort) (string, bool, error) {
	type gwEntry struct {
		selector string
		up       proxy.UpstreamRef
	}
	seen := make(map[string]bool)
	var entries []gwEntry

	sorted := make([]proxy.GatewayPort, len(gateways))
	copy(sorted, gateways)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Port != sorted[j].Port {
			return sorted[i].Port < sorted[j].Port
		}
		if sorted[i].BindIP != sorted[j].BindIP {
			return sorted[i].BindIP < sorted[j].BindIP
		}
		return sorted[i].AppID < sorted[j].AppID
	})

	for _, gw := range sorted {
		if gw.Direct || strings.EqualFold(strings.TrimSpace(gw.Protocol), "udp") {
			continue
		}
		if gw.Port <= 0 || gw.Port > 65535 {
			return "", false, fmt.Errorf("ferron: invalid gateway port %d", gw.Port)
		}
		bindIP := strings.TrimSpace(gw.BindIP)
		selector := "*:" + strconv.Itoa(gw.Port)
		if bindIP != "" && bindIP != "0.0.0.0" {
			hostPart := bindIP
			if strings.Contains(bindIP, ":") && !strings.HasPrefix(bindIP, "[") {
				hostPart = "[" + bindIP + "]"
			}
			selector = "http " + hostPart + ":" + strconv.Itoa(gw.Port)
		}
		if seen[selector] {
			continue
		}
		seen[selector] = true

		var up proxy.UpstreamRef
		if gw.Upstream != nil {
			up = *gw.Upstream
		}
		if up.Port <= 0 && gw.ContainerPort > 0 {
			up.Port = gw.ContainerPort
		}
		if strings.TrimSpace(up.Container) == "" && strings.TrimSpace(up.Service) == "" && strings.TrimSpace(up.External) == "" {
			if strings.TrimSpace(gw.AppID) != "" {
				up.Container = gw.AppID
			}
		}
		entries = append(entries, gwEntry{selector: selector, up: up})
	}
	if len(entries) == 0 {
		return "", false, nil
	}

	var b strings.Builder
	b.WriteString("# Allocated HTTP gateway ports.\n\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "%s {\n", e.selector)
		if err := writeProxyBlock(&b, "    ", proxy.Site{TrustedProxy: true}, e.up); err != nil {
			return "", false, err
		}
		b.WriteString("}\n\n")
	}
	return b.String(), true, nil
}

func writeProxyBlock(b *strings.Builder, indent string, s proxy.Site, up proxy.UpstreamRef) error {
	targetURL, err := formatUpstreamURL(up)
	if err != nil {
		return err
	}
	fmt.Fprintf(b, "%sproxy {\n", indent)
	subIndent := indent + "    "
	fmt.Fprintf(b, "%supstream %s", subIndent, quoteString(targetURL))
	if isHostnameUpstream(targetURL) {
		b.WriteString(" {\n")
		fmt.Fprintf(b, "%s    logical_dns true\n", subIndent)
		fmt.Fprintf(b, "%s}\n", subIndent)
	} else {
		b.WriteString("\n")
	}
	fmt.Fprintf(b, "%skeepalive true\n", subIndent)
	if up.SkipVerify {
		fmt.Fprintf(b, "%sno_verification true\n", subIndent)
	}
	if strings.TrimSpace(up.LBTryDuration) != "" {
		fmt.Fprintf(b, "%sretry_connection true\n", subIndent)
		fmt.Fprintf(b, "%sretry_interval \"0.25s\"\n", subIndent)
	}
	if !s.TrustedProxy && !s.BehindShield {
		fmt.Fprintf(b, "%srequest_header \"X-Real-IP\" \"{{remote.ip}}\"\n", subIndent)
		fmt.Fprintf(b, "%srequest_header \"X-Forwarded-For\" \"{{remote.ip}}\"\n", subIndent)
		fmt.Fprintf(b, "%srequest_header \"-CF-Connecting-IP\"\n", subIndent)
	}
	if (s.TrustedProxy || s.BehindShield) && len(up.RequestHeaders) > 0 {
		for _, op := range up.RequestHeaders {
			name := strings.TrimSpace(op.Name)
			if name == "" {
				continue
			}
			switch op.Action {
			case proxy.HeaderDelete:
				fmt.Fprintf(b, "%srequest_header %s\n", subIndent, quoteString("-"+name))
			case proxy.HeaderAdd:
				fmt.Fprintf(b, "%srequest_header %s %s\n", subIndent, quoteString("+"+name), quoteString(op.Value))
			default:
				fmt.Fprintf(b, "%srequest_header %s %s\n", subIndent, quoteString(name), quoteString(op.Value))
			}
		}
	}
	fmt.Fprintf(b, "%s}\n", indent)
	return nil
}
