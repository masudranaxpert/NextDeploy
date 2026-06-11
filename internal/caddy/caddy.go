// Package caddy provides helpers for managing caddy-docker-proxy and
// generating Docker labels for automatic routing + TLS.
package caddy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"panel/internal/db"

	"gopkg.in/yaml.v3"
)

const (
	DefaultAdminAPI    = "http://caddy:2019"
	CaddyContainerName = "caddy"
	CaddyImage         = "lucaslorentz/caddy-docker-proxy:ci-alpine"
	CaddyNetwork       = "NextDeploy"
	PanelNetworkName   = "NextDeploy"
	PanelNetworkKey    = "nextdeploy"
	GeneratedCompose   = ".nextdeploy.generated.compose.yml"
	// ndSharedVolPrefix is the compose-level volume key prefix for Docker volumes
	// injected by NextDeploy so file_server roots can be resolved inside Caddy.
	ndSharedVolPrefix = "nd_shared_"
)

// NDSharedMount binds an existing Docker named volume into the Caddy service.
type NDSharedMount struct {
	VolumeName string
	Target     string
}

func ndSharedComposeKey(volName string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(volName)))
	return ndSharedVolPrefix + hex.EncodeToString(h[:8])
}

// ValidNDSharedTarget checks that a mount path is safe for the Caddy container (absolute Unix path, no "..").
func ValidNDSharedTarget(target string) bool {
	t := strings.TrimSpace(target)
	if t == "" || t[0] != '/' {
		return false
	}
	for _, seg := range strings.Split(t, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

func stripNDSharedVolumeMounts(caddySvc map[string]interface{}) {
	raw := caddySvc["volumes"]
	switch v := raw.(type) {
	case nil:
		return
	case []interface{}:
		out := make([]interface{}, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				out = append(out, item)
				continue
			}
			src := strings.TrimSpace(strings.SplitN(s, ":", 2)[0])
			if strings.HasPrefix(src, ndSharedVolPrefix) {
				continue
			}
			out = append(out, item)
		}
		if len(out) == 0 {
			delete(caddySvc, "volumes")
			return
		}
		caddySvc["volumes"] = out
	default:
		return
	}
}

func stripNDSharedTopLevelVolumes(doc map[string]interface{}) {
	rawVolumes, ok := toStringMap(doc["volumes"])
	if !ok || len(rawVolumes) == 0 {
		return
	}
	for k := range rawVolumes {
		if strings.HasPrefix(k, ndSharedVolPrefix) {
			delete(rawVolumes, k)
		}
	}
	if len(rawVolumes) == 0 {
		delete(doc, "volumes")
	} else {
		doc["volumes"] = rawVolumes
	}
}

func appendCaddyVolumeMount(caddySvc map[string]interface{}, mountLine string) {
	mountLine = strings.TrimSpace(mountLine)
	if mountLine == "" {
		return
	}
	var list []interface{}
	switch raw := caddySvc["volumes"].(type) {
	case nil:
	case []interface{}:
		list = raw
	default:
		list = []interface{}{raw}
	}
	for _, item := range list {
		if s, ok := item.(string); ok && strings.TrimSpace(s) == mountLine {
			return
		}
	}
	list = append(list, mountLine)
	caddySvc["volumes"] = list
}

func mergeNDSharedMountsIntoDoc(doc map[string]interface{}, caddySvc map[string]interface{}, mounts []NDSharedMount) {
	if len(mounts) == 0 {
		return
	}
	seen := map[string]struct{}{}
	rawVolumes, ok := toStringMap(doc["volumes"])
	if !ok {
		rawVolumes = map[string]interface{}{}
	}
	for _, m := range mounts {
		vol := strings.TrimSpace(m.VolumeName)
		tgt := strings.TrimSpace(m.Target)
		if vol == "" || !ValidNDSharedTarget(tgt) {
			continue
		}
		key := ndSharedComposeKey(vol)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		rawVolumes[key] = map[string]interface{}{
			"external": true,
			"name":     vol,
		}
		appendCaddyVolumeMount(caddySvc, key+":"+tgt+":ro")
	}
	if len(rawVolumes) > 0 {
		doc["volumes"] = rawVolumes
	}
}

// CleanQuotedValue strips surrounding quotes (single, double, backtick) from a string.
func CleanQuotedValue(v string) string {
	v = strings.TrimSpace(v)
	for len(v) >= 2 {
		first := v[0]
		last := v[len(v)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') || (first == '`' && last == '`') {
			if unquoted, err := strconv.Unquote(v); err == nil {
				v = strings.TrimSpace(unquoted)
				continue
			}
			v = strings.TrimSpace(v[1 : len(v)-1])
			continue
		}
		break
	}
	return v
}

func cleanQuotedValue(v string) string {
	return CleanQuotedValue(v)
}

// AdminStatus calls the Caddy admin /config/ endpoint to check if Caddy is up.
func AdminStatus(ctx context.Context, adminAPI string) (bool, string) {
	if adminAPI == "" {
		adminAPI = DefaultAdminAPI
	}
	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, adminAPI+"/config/", nil)
	if err != nil {
		return false, err.Error()
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err.Error()
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, string(body)
	}
	return false, fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))
}

// AdminConfigGet fetches the full running Caddy JSON config.
func AdminConfigGet(ctx context.Context, adminAPI string) (string, error) {
	if adminAPI == "" {
		adminAPI = DefaultAdminAPI
	}
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, adminAPI+"/config/", nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var buf bytes.Buffer
	if err := json.Indent(&buf, body, "", "  "); err != nil {
		return string(body), nil
	}
	return buf.String(), nil
}

// AdminConfigPost replaces the Caddy config via admin API.
func AdminConfigPost(ctx context.Context, adminAPI, jsonConfig string) error {
	if adminAPI == "" {
		adminAPI = DefaultAdminAPI
	}
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, adminAPI+"/load",
		strings.NewReader(jsonConfig))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("caddy admin error %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// GenerateLabels builds the Docker labels map for a single site preview.
func GenerateLabels(d db.AppDomain) map[string]string {
	labels := map[string]string{}
	appendSiteLabels(labels, "caddy", d)
	return labels
}

// GenerateServiceLabels builds the merged labels for all domains of one service.
func GenerateServiceLabels(domains []db.AppDomain) map[string]string {
	out := map[string]string{}
	domains = sortedDomains(domains)
	if len(domains) == 1 {
		appendSiteLabels(out, "caddy", domains[0])
		return out
	}
	for i, d := range domains {
		appendSiteLabels(out, "caddy_"+strconv.Itoa(i), d)
	}
	return out
}

func normalizedRoutes(d db.AppDomain) []db.AppDomainRoute {
	routes := d.EffectiveRouteRules()
	if len(routes) == 0 {
		return nil
	}
	out := make([]db.AppDomainRoute, 0, len(routes))
	for _, route := range routes {
		path := strings.TrimSpace(route.Path)
		root := strings.TrimSpace(route.Root)
		if path == "" {
			continue
		}
		direct := route.EffectiveDirect()
		if direct && root == "" {
			continue
		}
		if route.Priority <= 0 {
			route.Priority = 1
		}
		route.Path = path
		route.Root = root
		route.Direct = direct
		out = append(out, route)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Root < out[j].Root
	})
	return out
}

// GenerateMergedCompose returns a merged compose YAML with normalized volumes, Caddy labels,
// and NextDeploy network. When panelEnv is non-empty, ".env" is added to every service's
// env_file so panel-managed variables reach containers regardless of source type.
// When cgroupParent is non-empty it is forced onto every service so all of the owner's
// containers run under a single cgroup with a shared, kernel-enforced resource limit.
func GenerateMergedCompose(base []byte, projectName string, domains []db.AppDomain, panelEnv string, cgroupParent string) ([]byte, error) {
	var doc map[string]interface{}
	if err := yaml.Unmarshal(base, &doc); err != nil {
		return nil, err
	}
	if doc == nil {
		doc = map[string]interface{}{}
	}

	services, ok := toStringMap(doc["services"])
	if !ok || len(services) == 0 {
		return nil, fmt.Errorf("compose file has no services block")
	}
	normalizeNamedVolumes(doc, projectName)
	normalizeServiceContainerNames(services, projectName)

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

	byService := map[string][]db.AppDomain{}
	for _, d := range sortedDomains(domains) {
		service := cleanQuotedValue(d.Service)
		d.Domain = cleanQuotedValue(d.Domain)
		d.Service = service
		if service == "" || strings.TrimSpace(d.Domain) == "" {
			continue
		}
		byService[service] = append(byService[service], d)
	}

	if len(byService) > 0 {
		networks, _ := toStringMap(doc["networks"])
		if networks == nil {
			networks = map[string]interface{}{}
		}
		networks[PanelNetworkKey] = map[string]interface{}{
			"external": true,
			"name":     PanelNetworkName,
		}
		doc["networks"] = networks

		for service, serviceDomains := range byService {
			rawSvc, exists := services[service]
			if !exists {
				return nil, fmt.Errorf("service %q not found in compose", service)
			}
			svc, ok := toStringMap(rawSvc)
			if !ok {
				return nil, fmt.Errorf("service %q has invalid compose structure", service)
			}
			attachNetwork(svc, PanelNetworkKey)
			mergeLabels(svc, GenerateServiceLabels(serviceDomains))
			services[service] = svc
		}
	}

	doc["services"] = services

	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString("# Auto-generated by NextDeploy. Do not edit manually.\n")
	buf.Write(out)
	return buf.Bytes(), nil
}

// injectEnvFile adds envFileName to the service env_file list if not already present.
// Handles all YAML forms: absent, bare string, list, and object {path:, required:}.
func injectEnvFile(service map[string]interface{}, envFileName string) {
	switch cur := service["env_file"].(type) {
	case nil:
		service["env_file"] = []interface{}{envFileName}

	case string:
		if strings.TrimSpace(cur) == envFileName {
			return
		}
		service["env_file"] = []interface{}{cur, envFileName}

	case []interface{}:
		for _, item := range cur {
			switch v := item.(type) {
			case string:
				if strings.TrimSpace(v) == envFileName {
					return
				}
			case map[string]interface{}:
				if p, ok := v["path"].(string); ok && strings.TrimSpace(p) == envFileName {
					return
				}
			case map[interface{}]interface{}:
				for k, val := range v {
					if ks, ok := k.(string); ok && ks == "path" {
						if ps, ok := val.(string); ok && strings.TrimSpace(ps) == envFileName {
							return
						}
					}
				}
			}
		}
		service["env_file"] = append(cur, envFileName)

	default:
		service["env_file"] = []interface{}{envFileName}
	}
}

func containerNameFromYAML(raw interface{}) string {
	if raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(cleanQuotedValue(v))
	case int:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	case float64:
		return strings.TrimSpace(cleanQuotedValue(fmt.Sprint(v)))
	default:
		return strings.TrimSpace(cleanQuotedValue(fmt.Sprint(v)))
	}
}

// normalizeServiceContainerNames prefixes explicit container_name values with the compose project name
// when missing, so custom names stay grep-friendly (e.g. flixbd_app -> myapp_a1b2c3d4_flixbd_app).
// Services without container_name keep Compose defaults (project_service_N).
func normalizeServiceContainerNames(services map[string]interface{}, projectName string) {
	projectName = strings.TrimSpace(projectName)
	if projectName == "" {
		return
	}
	prefix := projectName + "_"
	for svcKey, raw := range services {
		svc, ok := toStringMap(raw)
		if !ok {
			continue
		}
		cn := containerNameFromYAML(svc["container_name"])
		if cn == "" {
			continue
		}
		if strings.HasPrefix(cn, prefix) {
			continue
		}
		svc["container_name"] = prefix + cn
		services[svcKey] = svc
	}
}

func normalizeNamedVolumes(doc map[string]interface{}, projectName string) {
	projectName = strings.TrimSpace(projectName)
	if projectName == "" {
		return
	}
	rawVolumes, ok := toStringMap(doc["volumes"])
	if !ok || len(rawVolumes) == 0 {
		return
	}
	for volumeKey, raw := range rawVolumes {
		volume, ok := toStringMap(raw)
		if !ok {
			volume = map[string]interface{}{}
		}
		if ext, ok := volume["external"].(bool); ok && ext {
			rawVolumes[volumeKey] = volume
			continue
		}
		volume["name"] = projectName + "_" + volumeKey
		rawVolumes[volumeKey] = volume
	}
	doc["volumes"] = rawVolumes
}

// GenerateRootStackCompose updates the root NextDeploy stack compose with
// Caddy admin settings and optional panel domain labels.
func GenerateRootStackCompose(base []byte, panelDomain string, enableHTTPS, enableWWW bool, email, caddyImage string, sharedMounts []NDSharedMount) ([]byte, error) {
	var doc map[string]interface{}
	if err := yaml.Unmarshal(base, &doc); err != nil {
		return nil, err
	}
	if doc == nil {
		doc = map[string]interface{}{}
	}
	services, ok := toStringMap(doc["services"])
	if !ok || len(services) == 0 {
		return nil, fmt.Errorf("compose file has no services block")
	}

	stripNDSharedTopLevelVolumes(doc)
	rawCaddyStrip, ok := services["caddy"]
	if ok {
		if caddySvcStrip, ok := toStringMap(rawCaddyStrip); ok {
			stripNDSharedVolumeMounts(caddySvcStrip)
			services["caddy"] = caddySvcStrip
		}
	}
	doc["services"] = services

	networks, _ := toStringMap(doc["networks"])
	if networks == nil {
		networks = map[string]interface{}{}
	}
	networks[PanelNetworkKey] = map[string]interface{}{
		"name": PanelNetworkName,
	}
	doc["networks"] = networks

	rawCaddy, ok := services["caddy"]
	if !ok {
		return nil, fmt.Errorf("compose file has no caddy service")
	}
	caddySvc, ok := toStringMap(rawCaddy)
	if !ok {
		return nil, fmt.Errorf("caddy service has invalid compose structure")
	}
	if strings.TrimSpace(caddyImage) != "" {
		caddySvc["image"] = strings.TrimSpace(caddyImage)
	}
	attachNetwork(caddySvc, PanelNetworkKey)
	mergeEnv(caddySvc, map[string]string{
		"CADDY_INGRESS_NETWORKS": PanelNetworkName,
		"CADDY_ADMIN":            "0.0.0.0:2019",
	})
	if strings.TrimSpace(email) != "" {
		mergeLabels(caddySvc, map[string]string{"caddy.email": strings.TrimSpace(email)})
	} else {
		removeLabels(caddySvc, func(key string) bool {
			return key == "caddy.email"
		})
	}
	mergeNDSharedMountsIntoDoc(doc, caddySvc, sharedMounts)
	services["caddy"] = caddySvc

	rawPanel, ok := services["panel"]
	if !ok {
		return nil, fmt.Errorf("compose file has no panel service")
	}
	panelSvc, ok := toStringMap(rawPanel)
	if !ok {
		return nil, fmt.Errorf("panel service has invalid compose structure")
	}
	attachNetwork(panelSvc, PanelNetworkKey)
	removeLabels(panelSvc, func(key string) bool {
		return strings.HasPrefix(key, "caddy")
	})
	panelDomain = strings.TrimSpace(panelDomain)
	if panelDomain != "" {
		mergeLabels(panelSvc, GenerateLabels(db.AppDomain{
			Domain:      panelDomain,
			Port:        8080,
			EnableHTTPS: enableHTTPS,
			EnableWWW:   enableWWW,
		}))
	}
	services["panel"] = panelSvc
	doc["services"] = services

	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func appendSiteLabels(labels map[string]string, prefix string, d db.AppDomain) {
	domain := cleanQuotedValue(d.Domain)
	if domain == "" {
		return
	}

	sites := make([]string, 0, 2)
	if d.EnableHTTPS {
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
	
	// Automatic security headers for all user apps
	labels[prefix+".header.X-Frame-Options"] = "SAMEORIGIN"
	labels[prefix+".header.X-Content-Type-Options"] = "nosniff"
	labels[prefix+".header.Referrer-Policy"] = "strict-origin-when-cross-origin"
	labels[prefix+".header.X-XSS-Protection"] = "1; mode=block"
	
	if d.EnableHTTPS && ShouldUseInternalTLS(domain) {
		labels[prefix+".tls"] = "internal"
	}

	port := d.Port
	if port <= 0 {
		port = 80
	}
	upstreams := "{{upstreams " + strconv.Itoa(port) + "}}"

	// handle_path automatically strips the matched prefix before passing to file_server,
	// which is the correct approach for serving static/media files at a URL sub-path.
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
			// handle_path strips the matched prefix automatically (no uri directive needed)
			base := fmt.Sprintf("%s.%d_handle_path", prefix, order)
			labels[base] = path
			labels[base+".0_root"] = "* " + root
			labels[base+".1_file_server"] = `{{""}}`
			order++
			continue
		}
		base := fmt.Sprintf("%s.%d_reverse_proxy", prefix, order)
		labels[base] = path + " " + upstreams
		order++
	}
	if order == 1 {
		labels[prefix+".reverse_proxy"] = upstreams
	} else {
		base := fmt.Sprintf("%s.%d_reverse_proxy", prefix, order)
		labels[base] = upstreams
	}
}

// ShouldUseInternalTLS checks if a domain should use internal/self-signed TLS.
func ShouldUseInternalTLS(domain string) bool {
	domain = strings.TrimSpace(strings.ToLower(CleanQuotedValue(domain)))
	if domain == "" {
		return false
	}
	if !strings.Contains(domain, ".") {
		return true
	}
	devSuffixes := []string{
		".local",
		".localhost",
		".internal",
		".test",
		".example",
		".invalid",
	}
	for _, suffix := range devSuffixes {
		if strings.HasSuffix(domain, suffix) {
			return true
		}
	}
	return false
}

// LabelsToYAML converts a labels map to a YAML snippet for display.
func LabelsToYAML(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("labels:\n")
	// sort keys for stable output
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
			sb.WriteString("  " + k + ": \"" + escapeYAML(v) + "\"\n")
		}
	}
	return sb.String()
}

func escapeYAML(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}

func sortedDomains(domains []db.AppDomain) []db.AppDomain {
	out := append([]db.AppDomain(nil), domains...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			li := out[j-1]
			ri := out[j]
			swap := false
			if li.ID > 0 && ri.ID > 0 {
				swap = ri.ID < li.ID
			} else {
				swap = strings.ToLower(ri.Domain) < strings.ToLower(li.Domain)
			}
			if !swap {
				break
			}
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func toStringMap(v interface{}) (map[string]interface{}, bool) {
	switch m := v.(type) {
	case nil:
		return map[string]interface{}{}, true
	case map[string]interface{}:
		return m, true
	case map[interface{}]interface{}:
		out := map[string]interface{}{}
		for k, val := range m {
			ks, ok := k.(string)
			if !ok {
				return nil, false
			}
			out[ks] = val
		}
		return out, true
	default:
		return nil, false
	}
}

func attachNetwork(service map[string]interface{}, network string) {
	raw := service["networks"]
	switch n := raw.(type) {
	case nil:
		service["networks"] = []interface{}{network}
	case []interface{}:
		for _, item := range n {
			switch it := item.(type) {
			case string:
				if it == network {
					return
				}
			case map[string]interface{}:
				if _, ok := it[network]; ok {
					return
				}
			case map[interface{}]interface{}:
				for k := range it {
					if ks, ok := k.(string); ok && ks == network {
						return
					}
				}
			}
		}
		service["networks"] = append(n, network)
	case map[string]interface{}:
		if _, ok := n[network]; !ok {
			n[network] = map[string]interface{}{}
		}
		service["networks"] = n
	case map[interface{}]interface{}:
		m, ok := toStringMap(n)
		if !ok {
			service["networks"] = []interface{}{network}
			return
		}
		if _, ok := m[network]; !ok {
			m[network] = map[string]interface{}{}
		}
		service["networks"] = m
	default:
		service["networks"] = []interface{}{network}
	}
}

func mergeLabels(service map[string]interface{}, added map[string]string) {
	existing := map[string]interface{}{}
	switch cur := service["labels"].(type) {
	case nil:
	case map[string]interface{}:
		existing = cur
	case map[interface{}]interface{}:
		if m, ok := toStringMap(cur); ok {
			existing = m
		}
	case []interface{}:
		for _, item := range cur {
			s, ok := item.(string)
			if !ok {
				continue
			}
			parts := strings.SplitN(s, "=", 2)
			if len(parts) == 2 {
				existing[parts[0]] = parts[1]
			} else if len(parts) == 1 {
				existing[parts[0]] = ""
			}
		}
	}
	for k, v := range added {
		existing[k] = v
	}
	service["labels"] = existing
}

func removeLabels(service map[string]interface{}, drop func(string) bool) {
	existing := map[string]interface{}{}
	switch cur := service["labels"].(type) {
	case nil:
	case map[string]interface{}:
		for k, v := range cur {
			if !drop(k) {
				existing[k] = v
			}
		}
	case map[interface{}]interface{}:
		if m, ok := toStringMap(cur); ok {
			for k, v := range m {
				if !drop(k) {
					existing[k] = v
				}
			}
		}
	case []interface{}:
		for _, item := range cur {
			s, ok := item.(string)
			if !ok {
				continue
			}
			parts := strings.SplitN(s, "=", 2)
			key := parts[0]
			if drop(key) {
				continue
			}
			if len(parts) == 2 {
				existing[key] = parts[1]
			} else {
				existing[key] = ""
			}
		}
	}
	if len(existing) == 0 {
		delete(service, "labels")
		return
	}
	service["labels"] = existing
}

func mergeEnv(service map[string]interface{}, added map[string]string) {
	existing := map[string]string{}
	switch cur := service["environment"].(type) {
	case nil:
	case map[string]interface{}:
		for k, v := range cur {
			existing[k] = fmt.Sprint(v)
		}
	case map[interface{}]interface{}:
		for k, v := range cur {
			ks, ok := k.(string)
			if !ok {
				continue
			}
			existing[ks] = fmt.Sprint(v)
		}
	case []interface{}:
		for _, item := range cur {
			s, ok := item.(string)
			if !ok {
				continue
			}
			parts := strings.SplitN(s, "=", 2)
			if len(parts) == 2 {
				existing[parts[0]] = parts[1]
			}
		}
	}
	for k, v := range added {
		existing[k] = v
	}
	envList := make([]interface{}, 0, len(existing))
	keys := make([]string, 0, len(existing))
	for k := range existing {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		envList = append(envList, k+"="+existing[k])
	}
	service["environment"] = envList
}
