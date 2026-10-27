package caddy

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"

	"panel/internal/db"

	"gopkg.in/yaml.v3"
)

// HostPortBinding represents an exposed host port detected in a compose service.
type HostPortBinding struct {
	HostPort      int
	ContainerPort int
	BindIP        string
	Protocol      string
	Service       string
}

// ExtractAndStripHostPorts scans services for host port mappings, converts them
// to container-only expose entries, and returns the list of detected host ports.
func ExtractAndStripHostPorts(services map[string]interface{}) ([]HostPortBinding, error) {
	var extracted []HostPortBinding

	for svcName, rawSvc := range services {
		svc, ok := toStringMap(rawSvc)
		if !ok {
			continue
		}
		rawPorts, hasPorts := svc["ports"]
		if !hasPorts || rawPorts == nil {
			continue
		}

		var keptPorts []interface{}
		var newExpose []string

		switch portsList := rawPorts.(type) {
		case []interface{}:
			for _, p := range portsList {
				binding, isHostPub, err := parsePortEntry(svcName, p)
				if err != nil {
					return nil, err
				}
				if isHostPub {
					extracted = append(extracted, binding)
					newExpose = append(newExpose, strconv.Itoa(binding.ContainerPort))
				} else {
					keptPorts = append(keptPorts, p)
				}
			}
		case string:
			binding, isHostPub, err := parsePortString(svcName, portsList)
			if err != nil {
				return nil, err
			}
			if isHostPub {
				extracted = append(extracted, binding)
				newExpose = append(newExpose, strconv.Itoa(binding.ContainerPort))
			} else {
				keptPorts = append(keptPorts, portsList)
			}
		}

		if len(keptPorts) > 0 {
			svc["ports"] = keptPorts
		} else {
			delete(svc, "ports")
		}

		if len(newExpose) > 0 {
			mergeExpose(svc, newExpose)
		}
		services[svcName] = svc
	}

	return extracted, nil
}

func parsePortEntry(svcName string, p interface{}) (HostPortBinding, bool, error) {
	switch v := p.(type) {
	case string:
		return parsePortString(svcName, v)
	case int:
		// Standalone port number like 3000 -> container port only
		return HostPortBinding{}, false, nil
	case map[string]interface{}:
		return parsePortMap(svcName, v)
	case map[interface{}]interface{}:
		m := make(map[string]interface{}, len(v))
		for k, val := range v {
			m[fmt.Sprint(k)] = val
		}
		return parsePortMap(svcName, m)
	default:
		return HostPortBinding{}, false, nil
	}
}

// expandComposeEnv resolves common docker-compose variable patterns:
// ${VAR:-default}, ${VAR-default}, ${VAR}, and $VAR.
// If an environment variable is not defined in the environment, the default is used.
func expandComposeEnv(s string) string {
	if !strings.Contains(s, "$") {
		return s
	}

	// First handle ${VAR:-default} and ${VAR-default}
	bracePattern := regexp.MustCompile(`\$\{([a-zA-Z_][a-zA-Z0-9_]*)(?::-?([^}]*))?\}`)
	s = bracePattern.ReplaceAllStringFunc(s, func(match string) string {
		sub := bracePattern.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		varName := sub[1]
		defaultVal := ""
		if len(sub) >= 3 {
			defaultVal = sub[2]
		}
		if val, exists := os.LookupEnv(varName); exists && val != "" {
			return val
		}
		return defaultVal
	})

	// Next handle simple $VAR
	simplePattern := regexp.MustCompile(`\$([a-zA-Z_][a-zA-Z0-9_]*)`)
	s = simplePattern.ReplaceAllStringFunc(s, func(match string) string {
		sub := simplePattern.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		varName := sub[1]
		if val, exists := os.LookupEnv(varName); exists && val != "" {
			return val
		}
		return ""
	})

	return s
}

// splitPortString splits a port spec by ":" only when the colon is outside "${...}".
func splitPortString(s string) []string {
	var parts []string
	var cur strings.Builder
	depth := 0

	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '$' && i+1 < len(s) && s[i+1] == '{' {
			depth++
			cur.WriteByte(ch)
			cur.WriteByte(s[i+1])
			i++
			continue
		}
		if ch == '}' && depth > 0 {
			depth--
			cur.WriteByte(ch)
			continue
		}
		if ch == ':' && depth == 0 {
			parts = append(parts, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(ch)
	}
	parts = append(parts, cur.String())
	return parts
}

func parsePortString(svcName, s string) (HostPortBinding, bool, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return HostPortBinding{}, false, nil
	}

	proto := "tcp"
	if idx := strings.Index(s, "/"); idx != -1 {
		proto = strings.ToLower(strings.TrimSpace(s[idx+1:]))
		s = s[:idx]
	}

	parts := splitPortString(s)
	// Expand variables in each part
	for i := range parts {
		parts[i] = strings.TrimSpace(expandComposeEnv(parts[i]))
	}

	switch len(parts) {
	case 1:
		// Single port: container port only, not host published
		return HostPortBinding{}, false, nil

	case 2:
		// host:container
		hostP, err := strconv.Atoi(parts[0])
		if err != nil {
			return HostPortBinding{}, false, fmt.Errorf("invalid host port %q: %w", parts[0], err)
		}
		containerP, err := strconv.Atoi(parts[1])
		if err != nil {
			return HostPortBinding{}, false, fmt.Errorf("invalid container port %q: %w", parts[1], err)
		}
		return HostPortBinding{
			HostPort:      hostP,
			ContainerPort: containerP,
			BindIP:        "0.0.0.0",
			Protocol:      proto,
			Service:       svcName,
		}, true, nil

	case 3:
		// ip:host:container
		bindIP := parts[0]
		if ip := net.ParseIP(bindIP); ip == nil && bindIP != "localhost" && bindIP != "" {
			return HostPortBinding{}, false, fmt.Errorf("invalid bind IP %q", bindIP)
		}
		if bindIP == "localhost" {
			bindIP = "127.0.0.1"
		}
		hostP, err := strconv.Atoi(parts[1])
		if err != nil {
			return HostPortBinding{}, false, fmt.Errorf("invalid host port %q: %w", parts[1], err)
		}
		containerP, err := strconv.Atoi(parts[2])
		if err != nil {
			return HostPortBinding{}, false, fmt.Errorf("invalid container port %q: %w", parts[2], err)
		}
		return HostPortBinding{
			HostPort:      hostP,
			ContainerPort: containerP,
			BindIP:        bindIP,
			Protocol:      proto,
			Service:       svcName,
		}, true, nil

	default:
		return HostPortBinding{}, false, fmt.Errorf("unrecognized port format %q", s)
	}
}

func parsePortMap(svcName string, m map[string]interface{}) (HostPortBinding, bool, error) {
	pubRaw, hasPub := m["published"]
	targetRaw, hasTarget := m["target"]
	if !hasPub || !hasTarget {
		return HostPortBinding{}, false, nil
	}

	hostP, err := strconv.Atoi(fmt.Sprint(pubRaw))
	if err != nil {
		return HostPortBinding{}, false, fmt.Errorf("invalid published port: %w", err)
	}
	targetP, err := strconv.Atoi(fmt.Sprint(targetRaw))
	if err != nil {
		return HostPortBinding{}, false, fmt.Errorf("invalid target port: %w", err)
	}

	bindIP := "0.0.0.0"
	if hostIP, ok := m["host_ip"].(string); ok && strings.TrimSpace(hostIP) != "" {
		bindIP = strings.TrimSpace(hostIP)
	}

	proto := "tcp"
	if p, ok := m["protocol"].(string); ok && strings.TrimSpace(p) != "" {
		proto = strings.ToLower(strings.TrimSpace(p))
	}

	return HostPortBinding{
		HostPort:      hostP,
		ContainerPort: targetP,
		BindIP:        bindIP,
		Protocol:      proto,
		Service:       svcName,
	}, true, nil
}

func mergeExpose(svc map[string]interface{}, ports []string) {
	var current []string
	if raw, ok := svc["expose"]; ok {
		switch v := raw.(type) {
		case []interface{}:
			for _, item := range v {
				current = append(current, fmt.Sprint(item))
			}
		case []string:
			current = append(current, v...)
		case string:
			current = append(current, v)
		}
	}

	seen := make(map[string]bool)
	for _, p := range current {
		seen[p] = true
	}
	for _, p := range ports {
		if !seen[p] {
			seen[p] = true
			current = append(current, p)
		}
	}

	list := make([]interface{}, len(current))
	for i, p := range current {
		list[i] = p
	}
	svc["expose"] = list
}

// GenerateSlotLabels creates Caddy labels for production domains and custom host port gateways.
// When previewOnly is true, only the explicit preview site (if any) is emitted so the pinned
// production slot keeps exclusive production traffic and host ports.
func GenerateSlotLabels(domains []db.AppDomain, hostPorts []HostPortBinding, preview *db.AppDomain, previewOnly bool) map[string]string {
	labels := make(map[string]string)

	// 1. Production domains, or the single preview site for a pinned standby slot
	siteIndex := 0
	if previewOnly {
		if preview != nil {
			appendSlotSiteLabels(labels, "caddy", *preview)
			siteIndex++
		}
	} else {
		for _, d := range sortedDomains(domains) {
			prefix := "caddy"
			if siteIndex > 0 {
				prefix = fmt.Sprintf("caddy_%d", siteIndex)
			}
			appendSlotSiteLabels(labels, prefix, d)
			siteIndex++
		}
	}

	// 2. Process Host Port Gateways (e.g. :3000, :4013) — skipped in preview-only mode so production keeps exclusive host ports
	if !previewOnly {
		for _, hp := range hostPorts {
			prefix := "caddy"
			if siteIndex > 0 {
				prefix = fmt.Sprintf("caddy_%d", siteIndex)
			}
			siteIndex++

			siteAddr := fmt.Sprintf(":%d", hp.HostPort)
			if hp.BindIP != "" && hp.BindIP != "0.0.0.0" {
				siteAddr = fmt.Sprintf("http://%s:%d", hp.BindIP, hp.HostPort)
			}

			labels[prefix] = siteAddr
			upstreams := fmt.Sprintf("{{upstreams %d}}", hp.ContainerPort)
			labels[prefix+".reverse_proxy"] = upstreams
			labels[prefix+".reverse_proxy.lb_try_duration"] = "5s"
			labels[prefix+".reverse_proxy.fail_duration"] = "5s"
		}
	}

	return labels
}

// SlotPreviewDomain derives a slot-specific preview hostname from a primary domain.
// For sslip.io / nip.io dashed domains it prefixes "<slot>-", keeping a single subdomain level for Let's Encrypt.
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

func appendSlotSiteLabels(labels map[string]string, prefix string, d db.AppDomain) {
	domain := cleanQuotedValue(d.Domain)
	if domain == "" {
		return
	}

	sites := make([]string, 0, 2)
	if d.EnableHTTPS || d.ShieldNodeID != nil {
		sites = append(sites, domain)
		if d.EnableWWW {
			sites = append(sites, "www."+domain)
		}
	} else {
		sites = append(sites, "http://"+domain)
		if d.EnableWWW {
			sites = append(sites, "http://www."+domain)
		}
	}

	labels[prefix] = strings.Join(sites, ", ")

	// Automatic security headers
	labels[prefix+".header.X-Frame-Options"] = "SAMEORIGIN"
	labels[prefix+".header.X-Content-Type-Options"] = "nosniff"
	labels[prefix+".header.Referrer-Policy"] = "strict-origin-when-cross-origin"
	labels[prefix+".header.X-XSS-Protection"] = "1; mode=block"

	if (d.EnableHTTPS || d.ShieldNodeID != nil) && (d.ShieldNodeID != nil || ShouldUseInternalTLS(domain)) {
		labels[prefix+".tls"] = "internal"
	}

	port := d.Port
	if port <= 0 {
		port = 80
	}
	upstreams := fmt.Sprintf("{{upstreams %d}}", port)

	routes := normalizedRoutes(d)
	order := 1
	for _, route := range routes {
		path := strings.TrimSpace(route.Path)
		if path == "" {
			continue
		}
		if route.Direct {
			root := strings.TrimSpace(route.Root)
			if root == "" {
				continue
			}
			base := fmt.Sprintf("%s.%d_handle_path", prefix, order)
			labels[base] = path
			labels[base+".0_root"] = "* " + root
			labels[base+".1_file_server"] = `{{""}}`
			order++
			continue
		}
		base := fmt.Sprintf("%s.%d_reverse_proxy", prefix, order)
		labels[base] = path + " " + upstreams
		labels[base+".lb_try_duration"] = "30s"
		order++
	}

	if order == 1 {
		labels[prefix+".reverse_proxy"] = upstreams
		labels[prefix+".reverse_proxy.lb_try_duration"] = "5s"
		labels[prefix+".reverse_proxy.fail_duration"] = "5s"
	} else {
		base := fmt.Sprintf("%s.%d_reverse_proxy", prefix, order)
		labels[base] = upstreams
		labels[base+".lb_try_duration"] = "5s"
		labels[base+".fail_duration"] = "5s"
	}
}

// GenerateSlotMergedCompose produces a compose file for a specific Blue/Green slot.
// It strips host port publications, registers internal exposes, assigns slot-safe container
// names and volumes, and attaches Caddy reverse-proxy labels.
func GenerateSlotMergedCompose(base []byte, projectName, slot string, domains []db.AppDomain, panelEnv, cgroupParent string, preview *db.AppDomain, previewOnly ...bool) ([]byte, []HostPortBinding, error) {
	isPreviewOnly := len(previewOnly) > 0 && previewOnly[0]
	var doc map[string]interface{}
	if err := yaml.Unmarshal(base, &doc); err != nil {
		return nil, nil, err
	}
	if doc == nil {
		doc = map[string]interface{}{}
	}

	services, ok := toStringMap(doc["services"])
	if !ok || len(services) == 0 {
		return nil, nil, fmt.Errorf("compose file has no services block")
	}

	// 1. Extract & strip host ports to avoid port collision
	hostPorts, err := ExtractAndStripHostPorts(services)
	if err != nil {
		return nil, nil, fmt.Errorf("extract host ports: %w", err)
	}

	// 2. Normalize named volumes with canonical app prefix so both slots share persistent storage
	volumeBase := strings.TrimSuffix(strings.TrimSuffix(projectName, "-blue"), "-green")
	normalizeNamedVolumes(doc, volumeBase)

	// 3. Normalize container names so Blue and Green never collide
	normalizeServiceContainerNames(services, projectName)

	// 4. Inject .env if panelEnv is configured
	if strings.TrimSpace(panelEnv) != "" {
		for svcKey, rawSvc := range services {
			svc, ok := toStringMap(rawSvc)
			if !ok {
				continue
			}
			injectEnvFile(svc, ".env")
			services[svcKey] = svc
		}
	}

	// 5. Apply cgroup limits if configured
	if cg := strings.TrimSpace(cgroupParent); cg != "" {
		for svcKey, rawSvc := range services {
			svc, ok := toStringMap(rawSvc)
			if !ok {
				continue
			}
			svc["cgroup_parent"] = cg
			services[svcKey] = svc
		}
	}

	// 6. Connect to NextDeploy Docker network
	networks, _ := toStringMap(doc["networks"])
	if networks == nil {
		networks = map[string]interface{}{}
	}
	networks[PanelNetworkKey] = map[string]interface{}{
		"external": true,
		"name":     PanelNetworkName,
	}
	doc["networks"] = networks

	// 7. Group domains and host ports by service
	byServiceDomains := map[string][]db.AppDomain{}
	for _, d := range sortedDomains(domains) {
		svc := cleanQuotedValue(d.Service)
		d.Domain = cleanQuotedValue(d.Domain)
		d.Service = svc
		if svc == "" || strings.TrimSpace(d.Domain) == "" {
			continue
		}
		byServiceDomains[svc] = append(byServiceDomains[svc], d)
	}

	byServicePorts := map[string][]HostPortBinding{}
	for _, hp := range hostPorts {
		byServicePorts[hp.Service] = append(byServicePorts[hp.Service], hp)
	}

	// Attach labels to targeted services
	for svcName, rawSvc := range services {
		svc, ok := toStringMap(rawSvc)
		if !ok {
			continue
		}

		sDomains := byServiceDomains[svcName]
		sPorts := byServicePorts[svcName]

		var svcPreview *db.AppDomain
		if isPreviewOnly && preview != nil && preview.Service == svcName {
			svcPreview = preview
		}

		if len(sDomains) > 0 || len(sPorts) > 0 || svcPreview != nil {
			attachNetwork(svc, PanelNetworkKey)
			labels := GenerateSlotLabels(sDomains, sPorts, svcPreview, isPreviewOnly)
			mergeLabels(svc, labels)
			services[svcName] = svc
		}
	}

	doc["services"] = services

	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, nil, err
	}

	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("# NextDeploy Zero-Downtime Slot Compose [%s]. Do not edit manually.\n", slot))
	buf.Write(out)
	return buf.Bytes(), hostPorts, nil
}
