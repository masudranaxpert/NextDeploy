package caddy

import (
	"bytes"
	"fmt"
	"strings"

	"panel/internal/db"
	"panel/internal/proxy"
	"panel/internal/runutil"

	"gopkg.in/yaml.v3"
)

// HostPortBinding represents an exposed host port detected in a compose service.
type HostPortBinding = proxy.HostPortBinding

// ParseComposeEnv parses dotenv-formatted text blocks in priority order (later blocks override earlier ones).
func ParseComposeEnv(blocks ...string) map[string]string {
	return proxy.ParseComposeEnv(blocks...)
}

// ExtractAndStripHostPorts scans services for host port mappings, converts them
// to container-only expose entries, and returns the list of detected host ports.
func ExtractAndStripHostPorts(services map[string]interface{}, envMaps ...map[string]string) ([]HostPortBinding, error) {
	return proxy.ExtractAndStripHostPorts(services, envMaps...)
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

	// 2. Process Host Port Gateways (e.g. :3000, :4013) — skipped in preview-only mode or for direct L4/UDP ports
	if !previewOnly {
		for _, hp := range hostPorts {
			if hp.Direct {
				continue
			}
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
			labels[prefix+".reverse_proxy.stream_close_delay"] = "10m"
			labels[prefix+".reverse_proxy.lb_try_duration"] = "5s"
			labels[prefix+".reverse_proxy.fail_duration"] = "5s"
		}
	}

	return labels
}

// SlotPreviewDomain derives a slot-specific preview hostname from a primary domain.
// For sslip.io / nip.io dashed domains it prefixes "<slot>-", keeping a single subdomain level for Let's Encrypt.
func SlotPreviewDomain(domain, slot string) string {
	return proxy.SlotPreviewDomain(domain, slot)
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
			labels[base+".1_file_server.hide"] = ".git .env* *.sql *.db *.sqlite *.key *.sock"
			order++
			continue
		}
		base := fmt.Sprintf("%s.%d_reverse_proxy", prefix, order)
		labels[base] = path + " " + upstreams
		labels[base+".stream_close_delay"] = "10m"
		labels[base+".lb_try_duration"] = "30s"
		if !d.TrustedProxy && d.ShieldNodeID == nil {
			applyUntrustedDirectProxyHeaders(labels, base)
		}
		order++
	}

	if order == 1 {
		rpBase := prefix + ".reverse_proxy"
		labels[rpBase] = upstreams
		labels[rpBase+".stream_close_delay"] = "10m"
		labels[rpBase+".lb_try_duration"] = "5s"
		labels[rpBase+".fail_duration"] = "5s"
		if !d.TrustedProxy && d.ShieldNodeID == nil {
			applyUntrustedDirectProxyHeaders(labels, rpBase)
		}
	} else {
		base := fmt.Sprintf("%s.%d_reverse_proxy", prefix, order)
		labels[base] = upstreams
		labels[base+".stream_close_delay"] = "10m"
		labels[base+".lb_try_duration"] = "5s"
		labels[base+".fail_duration"] = "5s"
		if !d.TrustedProxy && d.ShieldNodeID == nil {
			applyUntrustedDirectProxyHeaders(labels, base)
		}
	}
}

// GenerateSlotMergedCompose produces a compose file for a specific Blue/Green slot.
// It strips HTTP host port publications for Caddy proxying, preserves non-HTTP/UDP ports directly on services,
// assigns slot-safe container names and volumes, and attaches Caddy reverse-proxy labels.
func GenerateSlotMergedCompose(base []byte, projectName, slot string, domains []db.AppDomain, panelEnv, cgroupParent string, preview *db.AppDomain, previewOnly ...bool) ([]byte, []HostPortBinding, error) {
	return GenerateSlotMergedComposeWithEnv(base, projectName, slot, domains, panelEnv, "", cgroupParent, preview, previewOnly...)
}

// GenerateSlotMergedComposeWithEnv is like GenerateSlotMergedCompose but also incorporates dotenv blocks
// (such as .env files in the workspace) when resolving port expressions like ${APP_PORT:-8080}.
func GenerateSlotMergedComposeWithEnv(base []byte, projectName, slot string, domains []db.AppDomain, panelEnv, dotenvEnv, cgroupParent string, preview *db.AppDomain, previewOnly ...bool) ([]byte, []HostPortBinding, error) {
	return GenerateSlotMergedComposeWithEnvAndLabeler(base, projectName, slot, "", domains, panelEnv, dotenvEnv, cgroupParent, preview, nil, previewOnly...)
}

// GenerateSlotMergedComposeWithEnvAndLabeler merges a slot compose override using labeler
// (or legacy Caddy labels when labeler is nil) while returning partitioned host port bindings.
func GenerateSlotMergedComposeWithEnvAndLabeler(base []byte, projectName, slot, appID string, domains []db.AppDomain, panelEnv, dotenvEnv, cgroupParent string, preview *db.AppDomain, labeler AppLabeler, previewOnly ...bool) ([]byte, []HostPortBinding, error) {
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

	// Build merged env lookup: dotenv lower priority than panelEnv (matching container .env precedence)
	envMap := ParseComposeEnv(dotenvEnv, panelEnv)

	// 1. Extract host ports: HTTP ports move to Caddy gateway; direct L4/UDP ports stay on service (unless preview-only standby)
	hostPorts, err := proxy.ExtractAndPartitionHostPorts(services, !isPreviewOnly, envMap)
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

	// Size-capped container logs unless the app's compose sets its own logging.
	for svcKey, rawSvc := range services {
		if svc, ok := toStringMap(rawSvc); ok {
			runutil.SetDefaultComposeLogging(svc)
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

	// 7. Group domains and Caddy-proxied (non-direct) host ports by service
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
		if hp.Direct {
			continue
		}
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
			var labels map[string]string
			if labeler != nil {
				labels = buildServiceLabelsWithLabeler(appID, projectName, slot, svcName, sDomains, sPorts, svcPreview, isPreviewOnly, labeler)
			} else {
				labels = GenerateSlotLabels(sDomains, sPorts, svcPreview, isPreviewOnly)
			}
			if len(labels) > 0 {
				mergeLabels(svc, labels)
			}
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
