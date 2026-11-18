package proxy

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

func parsePortEntry(svcName string, p interface{}, envMaps ...map[string]string) (HostPortBinding, bool, error) {
	switch v := p.(type) {
	case string:
		return ParsePortString(svcName, v, envMaps...)
	case int:
		return HostPortBinding{}, false, nil
	case map[string]interface{}:
		return parsePortMap(svcName, v, envMaps...)
	case map[interface{}]interface{}:
		m := make(map[string]interface{}, len(v))
		for k, val := range v {
			m[fmt.Sprint(k)] = val
		}
		return parsePortMap(svcName, m, envMaps...)
	default:
		return HostPortBinding{}, false, nil
	}
}

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

// ParsePortString parses a single compose short-form port string (such as "8080:80"
// or "127.0.0.1:${APP_PORT:-3000}:3000/tcp") into a HostPortBinding. The boolean
// return value reports whether the specification publishes a host port.
func ParsePortString(svcName, s string, envMaps ...map[string]string) (HostPortBinding, bool, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return HostPortBinding{}, false, nil
	}

	proto := "tcp"
	if idx := strings.Index(s, "/"); idx != -1 {
		proto = strings.ToLower(strings.TrimSpace(expandComposeEnv(s[idx+1:], envMaps...)))
		if proto == "" {
			proto = "tcp"
		}
		s = s[:idx]
	}

	parts := splitPortString(s)
	for i := range parts {
		parts[i] = strings.TrimSpace(expandComposeEnv(parts[i], envMaps...))
	}

	switch len(parts) {
	case 1:
		return HostPortBinding{}, false, nil
	case 2:
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

func parsePortMap(svcName string, m map[string]interface{}, envMaps ...map[string]string) (HostPortBinding, bool, error) {
	pubRaw, hasPub := m["published"]
	targetRaw, hasTarget := m["target"]
	if !hasPub || !hasTarget {
		return HostPortBinding{}, false, nil
	}

	hostP, err := strconv.Atoi(strings.TrimSpace(expandComposeEnv(fmt.Sprint(pubRaw), envMaps...)))
	if err != nil {
		return HostPortBinding{}, false, fmt.Errorf("invalid published port: %w", err)
	}
	targetP, err := strconv.Atoi(strings.TrimSpace(expandComposeEnv(fmt.Sprint(targetRaw), envMaps...)))
	if err != nil {
		return HostPortBinding{}, false, fmt.Errorf("invalid target port: %w", err)
	}

	bindIP := "0.0.0.0"
	if hostIP, ok := m["host_ip"].(string); ok && strings.TrimSpace(hostIP) != "" {
		if expanded := strings.TrimSpace(expandComposeEnv(hostIP, envMaps...)); expanded != "" {
			bindIP = expanded
		}
	}

	proto := "tcp"
	if p, ok := m["protocol"].(string); ok && strings.TrimSpace(p) != "" {
		if expanded := strings.ToLower(strings.TrimSpace(expandComposeEnv(p, envMaps...))); expanded != "" {
			proto = expanded
		}
	}

	return HostPortBinding{
		HostPort:      hostP,
		ContainerPort: targetP,
		BindIP:        bindIP,
		Protocol:      proto,
		Service:       svcName,
	}, true, nil
}
