package caddy

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// knownProxyServiceKeys are root-stack service keys that may hold a reverse proxy.
var knownProxyServiceKeys = []string{CaddyContainerName, "nd-proxy", "proxy"}

// GenerateRootStackComposeForEngine rewrites the root NextDeploy stack so that serviceKey runs
// the given proxy service (as produced by a non-Caddy proxy.Driver.StackService). Any other
// proxy service (for example the old "caddy" one) is removed, top-level volumes are merged,
// the panel service keeps its own config but drops Caddy labels (the panel domain is served
// from the rendered engine config instead), and environment values already present on the
// previous proxy service (such as generated admin tokens) are preserved.
func GenerateRootStackComposeForEngine(base []byte, serviceKey string, svc map[string]any, volumes map[string]any) ([]byte, error) {
	serviceKey = strings.TrimSpace(serviceKey)
	if serviceKey == "" {
		return nil, fmt.Errorf("proxy service key is empty")
	}
	if svc == nil {
		return nil, fmt.Errorf("proxy service definition is empty")
	}
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
	if _, ok := services["panel"]; !ok {
		return nil, fmt.Errorf("compose file has no panel service")
	}

	// Preserve environment values of the previous proxy service (same engine re-render).
	prevEnv := map[string]string{}
	if raw, ok := services[serviceKey]; ok {
		if prev, ok := toStringMap(raw); ok {
			prevEnv = envToMap(prev["environment"])
		}
	}
	for _, k := range knownProxyServiceKeys {
		if k != serviceKey {
			delete(services, k)
		}
	}

	newSvc := make(map[string]interface{}, len(svc))
	for k, v := range svc {
		newSvc[k] = v
	}
	if len(prevEnv) > 0 {
		env := envToMap(newSvc["environment"])
		for k, v := range prevEnv {
			if strings.TrimSpace(v) == "" || strings.HasPrefix(strings.TrimSpace(v), "${") {
				continue
			}
			// Keep a real previous value over an empty value or an unresolved "${VAR…}" placeholder.
			if cur, ok := env[k]; !ok || strings.TrimSpace(cur) == "" || strings.HasPrefix(strings.TrimSpace(cur), "${") {
				env[k] = v
			}
		}
		newSvc["environment"] = env
	}
	attachNetwork(newSvc, PanelNetworkKey)
	services[serviceKey] = newSvc

	panelSvc, ok := toStringMap(services["panel"])
	if !ok {
		return nil, fmt.Errorf("panel service has invalid compose structure")
	}
	attachNetwork(panelSvc, PanelNetworkKey)
	removeLabels(panelSvc, func(key string) bool { return strings.HasPrefix(key, "caddy") })
	if deps, ok := panelSvc["depends_on"].([]interface{}); ok {
		kept := deps[:0]
		for _, d := range deps {
			if s, _ := d.(string); s == CaddyContainerName && serviceKey != CaddyContainerName {
				continue
			}
			kept = append(kept, d)
		}
		if len(kept) == 0 {
			delete(panelSvc, "depends_on")
		} else {
			panelSvc["depends_on"] = kept
		}
	}
	services["panel"] = panelSvc
	doc["services"] = services

	networks, _ := toStringMap(doc["networks"])
	if networks == nil {
		networks = map[string]interface{}{}
	}
	networks[PanelNetworkKey] = map[string]interface{}{"name": PanelNetworkName}
	doc["networks"] = networks

	stripNDSharedTopLevelVolumes(doc)
	if len(volumes) > 0 {
		topVols, _ := toStringMap(doc["volumes"])
		if topVols == nil {
			topVols = map[string]interface{}{}
		}
		for k, v := range volumes {
			if v == nil {
				v = map[string]interface{}{}
			}
			topVols[k] = v
		}
		doc["volumes"] = topVols
	}
	return yaml.Marshal(doc)
}

// envToMap normalizes a compose environment block (map or KEY=VALUE list) into a map.
func envToMap(raw interface{}) map[string]string {
	out := map[string]string{}
	switch v := raw.(type) {
	case map[string]interface{}:
		for k, val := range v {
			if val == nil {
				out[k] = ""
			} else {
				out[k] = fmt.Sprint(val)
			}
		}
	case map[string]string:
		for k, val := range v {
			out[k] = val
		}
	case []interface{}:
		for _, item := range v {
			s := fmt.Sprint(item)
			if k, val, ok := strings.Cut(s, "="); ok {
				out[k] = val
			} else if s != "" {
				out[s] = ""
			}
		}
	}
	return out
}
