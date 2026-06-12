package sandbox

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func parseMemoryToMB(val string) (int, error) {
	val = strings.TrimSpace(strings.ToUpper(val))
	if val == "" {
		return 0, nil
	}
	re := regexp.MustCompile(`^([0-9.]+)\s*([KMG]B?|B)?$`)
	matches := re.FindStringSubmatch(val)
	if len(matches) < 2 {
		return 0, errors.New("invalid memory format")
	}
	num, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, err
	}
	unit := ""
	if len(matches) >= 3 {
		unit = matches[2]
	}
	switch {
	case strings.HasPrefix(unit, "G"):
		return int(num * 1024), nil
	case strings.HasPrefix(unit, "M"):
		return int(num), nil
	case strings.HasPrefix(unit, "K"):
		return int(num / 1024), nil
	default:
		return int(num / (1024 * 1024)), nil
	}
}

func parseCPUs(val interface{}) (float64, error) {
	if val == nil {
		return 0, nil
	}
	switch v := val.(type) {
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return 0, nil
		}
		return strconv.ParseFloat(v, 64)
	default:
		return 0, fmt.Errorf("invalid cpu format: %v", val)
	}
}

func isHostBindPath(src string) bool {
	src = strings.TrimSpace(src)
	if src == "" {
		return false
	}
	if strings.Contains(src, "..") {
		return true
	}
	if filepath.IsAbs(src) {
		return true
	}
	if strings.HasPrefix(src, "/") || strings.HasPrefix(src, "\\") {
		return true
	}
	if len(src) >= 2 && src[1] == ':' {
		return true
	}
	return false
}

func ValidateComposeSecurity(composeBytes []byte) ([]byte, error) {
	return validateCompose(composeBytes, false, 0, 0)
}

func CheckComposeSecurity(composeBytes []byte) error {
	_, err := ValidateComposeSecurity(composeBytes)
	return err
}

func ApplyResourceLimits(composeBytes []byte, maxCPUs float64, maxMemoryMB int) ([]byte, error) {
	return validateCompose(composeBytes, true, maxCPUs, maxMemoryMB)
}

func ValidateAndClampCompose(composeBytes []byte, maxCPUs float64, maxMemoryMB int) ([]byte, error) {
	secured, err := ValidateComposeSecurity(composeBytes)
	if err != nil {
		return nil, err
	}
	return ApplyResourceLimits(secured, maxCPUs, maxMemoryMB)
}

func validateCompose(composeBytes []byte, applyLimits bool, maxCPUs float64, maxMemoryMB int) ([]byte, error) {
	var doc map[string]interface{}
	if err := yaml.Unmarshal(composeBytes, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse compose file: %w", err)
	}
	if doc == nil {
		return nil, errors.New("compose file is empty")
	}

	servicesRaw, ok := doc["services"]
	if !ok {
		return nil, errors.New("compose file must contain a services block")
	}
	services, ok := servicesRaw.(map[string]interface{})
	if !ok {
		// Try map[interface{}]interface{}
		if rawMap, ok2 := servicesRaw.(map[interface{}]interface{}); ok2 {
			converted := make(map[string]interface{})
			for k, v := range rawMap {
				if ks, ok3 := k.(string); ok3 {
					converted[ks] = v
				}
			}
			services = converted
		} else {
			return nil, errors.New("invalid services block format")
		}
	}

	networksRaw, hasNetworks := doc["networks"]
	if hasNetworks {
		networks, ok := networksRaw.(map[string]interface{})
		if !ok {
			if rawMap, ok2 := networksRaw.(map[interface{}]interface{}); ok2 {
				converted := make(map[string]interface{})
				for k, v := range rawMap {
					if ks, ok3 := k.(string); ok3 {
						converted[ks] = v
					}
				}
				networks = converted
			}
		}
		if networks != nil {
			for netName, netConfig := range networks {
				if netConfig == nil {
					continue
				}
				if netMap, ok := netConfig.(map[string]interface{}); ok {
					if ext, ok2 := netMap["external"].(bool); ok2 && ext {
						if netName != "nextdeploy" && netName != "caddy" {
							return nil, fmt.Errorf("external network %q is blocked. Only 'nextdeploy' and 'caddy' networks are permitted", netName)
						}
					}
				} else if netMap2, ok := netConfig.(map[interface{}]interface{}); ok {
					if ext, ok2 := netMap2["external"].(bool); ok2 && ext {
						if netName != "nextdeploy" && netName != "caddy" {
							return nil, fmt.Errorf("external network %q is blocked. Only 'nextdeploy' and 'caddy' networks are permitted", netName)
						}
					}
				}
			}
		}
	}

	for svcName, svcRaw := range services {
		svc, ok := svcRaw.(map[string]interface{})
		if !ok {
			if rawMap, ok2 := svcRaw.(map[interface{}]interface{}); ok2 {
				converted := make(map[string]interface{})
				for k, v := range rawMap {
					if ks, ok3 := k.(string); ok3 {
						converted[ks] = v
					}
				}
				svc = converted
			} else {
				return nil, fmt.Errorf("invalid service format for %q", svcName)
			}
		}

		if priv, ok := svc["privileged"].(bool); ok && priv {
			return nil, fmt.Errorf("privileged mode is blocked on service %q for security reasons", svcName)
		}
		if _, ok := svc["privileged"]; ok {
			return nil, fmt.Errorf("privileged attribute is blocked on service %q for security reasons", svcName)
		}
		if _, ok := svc["cap_add"]; ok {
			return nil, fmt.Errorf("capabilities additions (cap_add) are blocked on service %q", svcName)
		}
		if _, ok := svc["cap_drop"]; ok {
			return nil, fmt.Errorf("capabilities attributes (cap_drop) are blocked on service %q", svcName)
		}
		// cgroup_parent is managed by NextDeploy (per-user resource group); a
		// user-supplied value could escape the owner's aggregate resource limits.
		if _, ok := svc["cgroup_parent"]; ok {
			return nil, fmt.Errorf("cgroup_parent is blocked on service %q; it is managed automatically by NextDeploy", svcName)
		}

		if netMode, ok := svc["network_mode"].(string); ok && netMode != "" {
			return nil, fmt.Errorf("custom network_mode %q is blocked on service %q", netMode, svcName)
		}

		var internalExposePorts []interface{}
		if portsRaw, ok := svc["ports"]; ok {
			if portsList, ok2 := portsRaw.([]interface{}); ok2 {
				for _, portItem := range portsList {
					switch pVal := portItem.(type) {
					case string:
						parts := strings.Split(pVal, ":")
						containerPortPart := parts[len(parts)-1]
						partsSlash := strings.Split(containerPortPart, "/")
						portNumStr := strings.TrimSpace(partsSlash[0])
						if portNum, err := strconv.Atoi(portNumStr); err == nil {
							internalExposePorts = append(internalExposePorts, portNum)
						} else if portNumStr != "" {
							internalExposePorts = append(internalExposePorts, portNumStr)
						}
					case int:
						internalExposePorts = append(internalExposePorts, pVal)
					case int64:
						internalExposePorts = append(internalExposePorts, pVal)
					case float64:
						internalExposePorts = append(internalExposePorts, int(pVal))
					case map[string]interface{}:
						if target, ok3 := pVal["target"]; ok3 {
							internalExposePorts = append(internalExposePorts, target)
						}
					case map[interface{}]interface{}:
						if target, ok3 := pVal["target"]; ok3 {
							internalExposePorts = append(internalExposePorts, target)
						}
					}
				}
			}
			delete(svc, "ports")
		}

		if len(internalExposePorts) > 0 {
			var existingExpose []interface{}
			if expRaw, ok := svc["expose"]; ok {
				if expList, ok2 := expRaw.([]interface{}); ok2 {
					existingExpose = expList
				}
			}
			seen := make(map[string]bool)
			var merged []interface{}
			for _, e := range existingExpose {
				eStr := fmt.Sprintf("%v", e)
				if !seen[eStr] {
					seen[eStr] = true
					merged = append(merged, e)
				}
			}
			for _, e := range internalExposePorts {
				eStr := fmt.Sprintf("%v", e)
				if !seen[eStr] {
					seen[eStr] = true
					merged = append(merged, e)
				}
			}
			svc["expose"] = merged
		}

		if volsRaw, ok := svc["volumes"]; ok {
			if volsList, ok2 := volsRaw.([]interface{}); ok2 {
				for _, volItem := range volsList {
					switch v := volItem.(type) {
					case string:
						parts := strings.SplitN(v, ":", 2)
						if len(parts) > 0 {
							src := parts[0]
							if isHostBindPath(src) {
								return nil, fmt.Errorf("host bind mount %q is blocked on service %q. Only named volumes or relative paths inside workspace are permitted", src, svcName)
							}
						}
					case map[string]interface{}:
						if t, ok3 := v["type"].(string); ok3 && t == "bind" {
							if src, ok4 := v["source"].(string); ok4 {
								if isHostBindPath(src) {
									return nil, fmt.Errorf("host bind mount %q is blocked on service %q. Only named volumes or relative paths inside workspace are permitted", src, svcName)
								}
							}
						}
					case map[interface{}]interface{}:
						tRaw, hasType := v["type"]
						srcRaw, hasSource := v["source"]
						if hasType && hasSource {
							if t, ok3 := tRaw.(string); ok3 && t == "bind" {
								if src, ok4 := srcRaw.(string); ok4 {
									if isHostBindPath(src) {
										return nil, fmt.Errorf("host bind mount %q is blocked on service %q. Only named volumes or relative paths inside workspace are permitted", src, svcName)
									}
								}
							}
						}
					}
				}
			}
		}

		if applyLimits {
			svc = applyServiceResourceLimits(svc, maxCPUs, maxMemoryMB)
		}
		services[svcName] = svc
	}

	doc["services"] = services

	clampedBytes, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal clamped compose: %w", err)
	}

	return clampedBytes, nil
}

func applyServiceResourceLimits(svc map[string]interface{}, maxCPUs float64, maxMemoryMB int) map[string]interface{} {
	deployRaw, hasDeploy := svc["deploy"]
	var deploy map[string]interface{}
	if hasDeploy {
		if m, ok2 := deployRaw.(map[string]interface{}); ok2 {
			deploy = m
		} else if m2, ok2 := deployRaw.(map[interface{}]interface{}); ok2 {
			deploy = make(map[string]interface{})
			for k, v := range m2 {
				if ks, ok3 := k.(string); ok3 {
					deploy[ks] = v
				}
			}
		}
	}
	if deploy == nil {
		deploy = make(map[string]interface{})
	}

	resourcesRaw, hasResources := deploy["resources"]
	var resources map[string]interface{}
	if hasResources {
		if m, ok2 := resourcesRaw.(map[string]interface{}); ok2 {
			resources = m
		} else if m2, ok2 := resourcesRaw.(map[interface{}]interface{}); ok2 {
			resources = make(map[string]interface{})
			for k, v := range m2 {
				if ks, ok3 := k.(string); ok3 {
					resources[ks] = v
				}
			}
		}
	}
	if resources == nil {
		resources = make(map[string]interface{})
	}

	limitsRaw, hasLimits := resources["limits"]
	var limits map[string]interface{}
	if hasLimits {
		if m, ok2 := limitsRaw.(map[string]interface{}); ok2 {
			limits = m
		} else if m2, ok2 := limitsRaw.(map[interface{}]interface{}); ok2 {
			limits = make(map[string]interface{})
			for k, v := range m2 {
				if ks, ok3 := k.(string); ok3 {
					limits[ks] = v
				}
			}
		}
	}
	if limits == nil {
		limits = make(map[string]interface{})
	}

	clampedCPUs := maxCPUs
	if cpuRaw, exists := limits["cpus"]; exists {
		userCPUs, err := parseCPUs(cpuRaw)
		if err == nil && userCPUs > 0 && userCPUs < clampedCPUs {
			clampedCPUs = userCPUs
		}
	}
	limits["cpus"] = fmt.Sprintf("%.2f", clampedCPUs)

	clampedMem := maxMemoryMB
	if memRaw, exists := limits["memory"]; exists {
		if memStr, ok3 := memRaw.(string); ok3 {
			userMem, err := parseMemoryToMB(memStr)
			if err == nil && userMem > 0 && userMem < clampedMem {
				clampedMem = userMem
			}
		}
	}
	limits["memory"] = fmt.Sprintf("%dM", clampedMem)

	resources["limits"] = limits
	deploy["resources"] = resources
	svc["deploy"] = deploy
	return svc
}

// GetComposeResources parses the compose YAML and returns the sum of memory limits (in MB) and CPU limits of all services.
func GetComposeResources(composeBytes []byte) (int, float64, error) {
	var doc map[string]interface{}
	if err := yaml.Unmarshal(composeBytes, &doc); err != nil {
		return 0, 0, err
	}
	if doc == nil {
		return 0, 0, nil
	}
	servicesRaw, ok := doc["services"]
	if !ok {
		return 0, 0, nil
	}
	services, ok := servicesRaw.(map[string]interface{})
	if !ok {
		if rawMap, ok2 := servicesRaw.(map[interface{}]interface{}); ok2 {
			converted := make(map[string]interface{})
			for k, v := range rawMap {
				if ks, ok3 := k.(string); ok3 {
					converted[ks] = v
				}
			}
			services = converted
		} else {
			return 0, 0, nil
		}
	}
	var totalMem int
	var totalCPU float64
	for _, svcRaw := range services {
		svc, ok := svcRaw.(map[string]interface{})
		if !ok {
			if rawMap, ok2 := svcRaw.(map[interface{}]interface{}); ok2 {
				converted := make(map[string]interface{})
				for k, v := range rawMap {
					if ks, ok3 := k.(string); ok3 {
						converted[ks] = v
					}
				}
				svc = converted
			} else {
				continue
			}
		}
		deployRaw, hasDeploy := svc["deploy"]
		var deploy map[string]interface{}
		if hasDeploy {
			if m, ok2 := deployRaw.(map[string]interface{}); ok2 {
				deploy = m
			} else if m2, ok2 := deployRaw.(map[interface{}]interface{}); ok2 {
				deploy = make(map[string]interface{})
				for k, v := range m2 {
					if ks, ok3 := k.(string); ok3 {
						deploy[ks] = v
					}
				}
			}
		}
		if deploy == nil {
			continue
		}
		resourcesRaw, hasResources := deploy["resources"]
		var resources map[string]interface{}
		if hasResources {
			if m, ok2 := resourcesRaw.(map[string]interface{}); ok2 {
				resources = m
			} else if m2, ok2 := resourcesRaw.(map[interface{}]interface{}); ok2 {
				resources = make(map[string]interface{})
				for k, v := range m2 {
					if ks, ok3 := k.(string); ok3 {
						resources[ks] = v
					}
				}
			}
		}
		if resources == nil {
			continue
		}
		limitsRaw, hasLimits := resources["limits"]
		var limits map[string]interface{}
		if hasLimits {
			if m, ok2 := limitsRaw.(map[string]interface{}); ok2 {
				limits = m
			} else if m2, ok2 := limitsRaw.(map[interface{}]interface{}); ok2 {
				limits = make(map[string]interface{})
				for k, v := range m2 {
					if ks, ok3 := k.(string); ok3 {
						limits[ks] = v
					}
				}
			}
		}
		if limits == nil {
			continue
		}
		if cpuRaw, exists := limits["cpus"]; exists {
			userCPUs, err := parseCPUs(cpuRaw)
			if err == nil {
				totalCPU += userCPUs
			}
		}
		if memRaw, exists := limits["memory"]; exists {
			if memStr, ok3 := memRaw.(string); ok3 {
				userMem, err := parseMemoryToMB(memStr)
				if err == nil {
					totalMem += userMem
				}
			}
		}
	}
	return totalMem, totalCPU, nil
}

