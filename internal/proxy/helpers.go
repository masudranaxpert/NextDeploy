package proxy

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// GeneratedCompose is the filename of panel-generated compose overrides in app workspaces.
	GeneratedCompose = ".nextdeploy.generated.compose.yml"
	// PanelNetworkName is the external Docker bridge network shared by proxy and app containers.
	PanelNetworkName = "NextDeploy"
	// PanelNetworkKey is the Compose service network key referencing PanelNetworkName.
	PanelNetworkKey = "nextdeploy"
)

// AppLabeler computes engine-specific Docker container labels for a Compose service
// serving sites via up. File-rendered drivers that use DNS instead of Docker labels return nil.
type AppLabeler func(sites []Site, up UpstreamRef) map[string]string

var (
	wildcardDotIPv4Re  = regexp.MustCompile(`(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})`)
	wildcardDashIPv4Re = regexp.MustCompile(`(\d{1,3}-\d{1,3}-\d{1,3}-\d{1,3})`)
	composeServiceRe   = regexp.MustCompile(`^[ \t]+([a-zA-Z0-9_\-]+)\s*:`)
)

// ShouldUseInternalTLS reports whether domain is a local/dev or private wildcard
// hostname that requires an internal CA certificate rather than public ACME.
func ShouldUseInternalTLS(domain string) bool {
	domain = strings.TrimSpace(strings.ToLower(CleanQuotedValue(domain)))
	if domain == "" {
		return false
	}
	if !strings.Contains(domain, ".") {
		return true
	}
	for _, suffix := range []string{".local", ".localhost", ".internal", ".test", ".example", ".invalid"} {
		if strings.HasSuffix(domain, suffix) {
			return true
		}
	}
	if ip := ExtractWildcardDomainIP(domain); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
			return true
		}
	}
	return false
}

// ExtractWildcardDomainIP extracts the embedded IPv4 address from a sslip.io or nip.io hostname.
func ExtractWildcardDomainIP(domain string) net.IP {
	d := strings.TrimSpace(strings.ToLower(domain))
	if !strings.HasSuffix(d, ".sslip.io") && !strings.HasSuffix(d, ".nip.io") {
		return nil
	}
	if matches := wildcardDashIPv4Re.FindAllString(d, -1); len(matches) > 0 {
		if ip := net.ParseIP(strings.ReplaceAll(matches[len(matches)-1], "-", ".")); ip != nil {
			return ip
		}
	}
	if matches := wildcardDotIPv4Re.FindAllString(d, -1); len(matches) > 0 {
		if ip := net.ParseIP(matches[len(matches)-1]); ip != nil {
			return ip
		}
	}
	return nil
}

// ShortPanelID returns a 6-character lowercase alphanumeric identifier derived from a panel UUID.
func ShortPanelID(panelID string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(panelID)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			if b.Len() == 6 {
				break
			}
		}
	}
	return b.String()
}

// FormatWildcardDomain builds a public sslip.io or nip.io hostname using dashed IPv4 notation.
func FormatWildcardDomain(slug, ipStr, provider string, panelID ...string) string {
	slug = strings.Trim(strings.ToLower(strings.TrimSpace(slug)), "-.")
	if slug == "" {
		slug = "app"
	}
	prefix := slug
	if len(panelID) > 0 {
		if pid6 := ShortPanelID(panelID[0]); pid6 != "" {
			prefix = slug + "-" + pid6
		}
	}
	if last := prefix[len(prefix)-1]; last >= '0' && last <= '9' {
		prefix += "-app"
	}
	ipStr = strings.TrimSpace(ipStr)
	if ipStr == "" {
		ipStr = "127.0.0.1"
	}
	dashedIP := strings.ReplaceAll(ipStr, ".", "-")
	provider = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(provider)), ".")
	if provider != "nip.io" {
		provider = "sslip.io"
	}
	return fmt.Sprintf("%s-%s.%s", prefix, dashedIP, provider)
}

// SlotPreviewDomain derives a slot-specific preview hostname from a primary domain.
func SlotPreviewDomain(domain, slot string) string {
	d := strings.TrimSpace(strings.ToLower(CleanQuotedValue(domain)))
	slot = strings.TrimSpace(strings.ToLower(slot))
	if d == "" || slot == "" {
		return ""
	}
	if strings.HasPrefix(d, slot+"-") || strings.HasPrefix(d, slot+".") {
		return d
	}
	if strings.HasSuffix(d, ".sslip.io") || strings.HasSuffix(d, ".nip.io") {
		return slot + "-" + d
	}
	return slot + "." + d
}

// SanitizeUpstreamToken normalizes an identifier segment into a lowercase [a-z0-9_]+ token.
func SanitizeUpstreamToken(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return "default"
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore && b.Len() > 0 {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "default"
	}
	return out
}

// UpstreamName constructs the canonical proxy upstream identifier for an app service port and optional slot.
func UpstreamName(appID, service string, port int, slot string) string {
	base := fmt.Sprintf("nd_%s_%s_%d", SanitizeUpstreamToken(appID), SanitizeUpstreamToken(service), port)
	if slot = SanitizeUpstreamToken(slot); slot != "" && slot != "default" {
		return base + "_" + slot
	}
	return base
}

// LabelsToYAML converts a container labels map to a deterministic YAML snippet for display.
func LabelsToYAML(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("labels:\n")
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := labels[k]
		if v == "" {
			sb.WriteString("  " + k + ": \"\"\n")
		} else {
			sb.WriteString("  " + k + ": \"" + strings.ReplaceAll(v, `"`, `\"`) + "\"\n")
		}
	}
	return sb.String()
}

// ParseComposeServiceNames extracts sorted service names from Compose YAML bytes.
func ParseComposeServiceNames(data []byte) []string {
	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err == nil {
		if rawServices, ok := doc["services"]; ok {
			if services, ok := rawServices.(map[string]interface{}); ok && len(services) > 0 {
				names := make([]string, 0, len(services))
				for name := range services {
					names = append(names, name)
				}
				sort.Strings(names)
				return names
			}
		}
	}
	lines := strings.Split(string(data), "\n")
	inServices := false
	var names []string
	seen := map[string]bool{}
	for _, line := range lines {
		if strings.TrimSpace(line) == "services:" {
			inServices = true
			continue
		}
		if inServices {
			if len(line) > 0 && line[0] != ' ' && line[0] != '\t' && line[0] != '#' {
				inServices = false
				continue
			}
			if m := composeServiceRe.FindStringSubmatch(line); len(m) == 2 {
				name := m[1]
				if !seen[name] {
					seen[name] = true
					names = append(names, name)
				}
			}
		}
	}
	sort.Strings(names)
	return names
}
