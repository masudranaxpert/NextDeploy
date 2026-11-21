package proxy

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// HostPortBinding represents an exposed host port detected in a compose service.
type HostPortBinding struct {
	HostPort      int
	ContainerPort int
	BindIP        string
	Protocol      string
	Service       string
	Direct        bool
}

// DirectPortsLabel lets a Compose service opt specific ports (e.g. "21115,21116,21117" or
// "21115-21117"), all ports ("true"/"all"), or no TCP ports ("false") into direct L4 host binding
// instead of HTTP reverse-proxying through Caddy/Angie/Ferron.
const DirectPortsLabel = "nextdeploy.ports.direct"

var nonHTTPContainerPorts = map[int]bool{
	21:    true, // FTP
	22:    true, // SSH
	25:    true, // SMTP
	53:    true, // DNS
	110:   true, // POP3
	143:   true, // IMAP
	465:   true, // SMTPS
	587:   true, // SMTP submission
	993:   true, // IMAPS
	995:   true, // POP3S
	1433:  true, // MSSQL
	1521:  true, // Oracle
	1883:  true, // MQTT
	3306:  true, // MySQL/MariaDB
	3389:  true, // RDP
	3478:  true, // STUN/TURN
	4222:  true, // NATS
	5349:  true, // STUN/TURN TLS
	5432:  true, // PostgreSQL
	5672:  true, // RabbitMQ AMQP
	6379:  true, // Redis/Valkey
	6380:  true, // Redis TLS
	8883:  true, // MQTT TLS
	9042:  true, // Cassandra
	9092:  true, // Kafka
	11211: true, // Memcached
	21115: true, // RustDesk hbbs NAT type test
	21116: true, // RustDesk hbbs ID/heartbeat
	21117: true, // RustDesk hbbr relay
	25565: true, // Minecraft
	26257: true, // CockroachDB
	27017: true, // MongoDB
	27018: true, // MongoDB
	27019: true, // MongoDB
	33060: true, // MySQL X
}

func serviceLabelValue(svc map[string]interface{}, key string) (string, bool) {
	raw, ok := svc["labels"]
	if !ok || raw == nil {
		return "", false
	}
	switch labels := raw.(type) {
	case map[string]interface{}:
		if v, exists := labels[key]; exists && v != nil {
			return strings.TrimSpace(fmt.Sprint(v)), true
		}
	case map[interface{}]interface{}:
		for k, v := range labels {
			if ks, ok := k.(string); ok && ks == key && v != nil {
				return strings.TrimSpace(fmt.Sprint(v)), true
			}
		}
	case map[string]string:
		if v, exists := labels[key]; exists {
			return strings.TrimSpace(v), true
		}
	case []interface{}:
		for _, item := range labels {
			s := strings.TrimSpace(fmt.Sprint(item))
			k, v, hasEq := strings.Cut(s, "=")
			if hasEq && strings.TrimSpace(k) == key {
				return strings.TrimSpace(v), true
			}
		}
	case []string:
		for _, item := range labels {
			s := strings.TrimSpace(item)
			k, v, hasEq := strings.Cut(s, "=")
			if hasEq && strings.TrimSpace(k) == key {
				return strings.TrimSpace(v), true
			}
		}
	}
	return "", false
}

func directPortLabelDecision(svc map[string]interface{}, b HostPortBinding) (direct bool, matched bool) {
	val, ok := serviceLabelValue(svc, DirectPortsLabel)
	if !ok || val == "" {
		return false, false
	}
	lower := strings.ToLower(val)
	switch lower {
	case "true", "1", "yes", "all", "*":
		return true, true
	case "false", "0", "no", "none":
		return false, true
	}
	for _, tok := range strings.FieldsFunc(lower, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == ';'
	}) {
		tok = strings.TrimSuffix(strings.TrimSuffix(tok, "/tcp"), "/udp")
		if startStr, endStr, isRange := strings.Cut(tok, "-"); isRange {
			startPort, err1 := strconv.Atoi(strings.TrimSpace(startStr))
			endPort, err2 := strconv.Atoi(strings.TrimSpace(endStr))
			if err1 == nil && err2 == nil && startPort > 0 && endPort >= startPort {
				if (b.ContainerPort >= startPort && b.ContainerPort <= endPort) ||
					(b.HostPort >= startPort && b.HostPort <= endPort) {
					return true, true
				}
			}
			continue
		}
		if portNum, err := strconv.Atoi(tok); err == nil && portNum > 0 {
			if b.ContainerPort == portNum || b.HostPort == portNum {
				return true, true
			}
		}
	}
	return false, false
}

func isDirectHostPort(b HostPortBinding, svc map[string]interface{}) bool {
	if strings.EqualFold(b.Protocol, "udp") {
		return true
	}
	if direct, matched := directPortLabelDecision(svc, b); matched {
		return direct
	}
	if nonHTTPContainerPorts[b.ContainerPort] {
		return true
	}
	if imgRaw, ok := svc["image"].(string); ok {
		img := strings.ToLower(strings.TrimSpace(imgRaw))
		if !strings.Contains(img, "admin") && !strings.Contains(img, "express") &&
			!strings.Contains(img, "insight") && !strings.Contains(img, "exporter") &&
			!strings.Contains(img, "ui") {
			for _, dbToken := range []string{"postgres", "postgis", "timescaledb", "mariadb", "mysql", "percona", "redis", "valkey", "keydb", "mongo", "memcached"} {
				if strings.Contains(img, dbToken) {
					return true
				}
			}
		}
	}
	return false
}

func formatDirectPortEntry(b HostPortBinding) string {
	base := fmt.Sprintf("%d:%d", b.HostPort, b.ContainerPort)
	if b.BindIP != "" && b.BindIP != "0.0.0.0" {
		base = fmt.Sprintf("%s:%d:%d", b.BindIP, b.HostPort, b.ContainerPort)
	}
	if strings.EqualFold(b.Protocol, "udp") {
		base += "/udp"
	}
	return base
}

func hasServiceProfiles(svc map[string]interface{}) bool {
	raw, ok := svc["profiles"]
	if !ok || raw == nil {
		return false
	}
	switch v := raw.(type) {
	case []interface{}:
		return len(v) > 0
	case []string:
		return len(v) > 0
	case string:
		return strings.TrimSpace(v) != ""
	default:
		return false
	}
}

// ExtractAndStripHostPorts scans compose services for host port mappings, converts
// them to container-only expose entries, and returns the detected host port bindings.
func ExtractAndStripHostPorts(services map[string]interface{}, envMaps ...map[string]string) ([]HostPortBinding, error) {
	return ExtractAndPartitionHostPorts(services, false, envMaps...)
}

// ExtractAndPartitionHostPorts scans compose services for host port mappings.
// HTTP gateway ports are always stripped into container-only expose entries; when
// keepDirect is true, non-HTTP/UDP direct L4 ports remain published on the service.
func ExtractAndPartitionHostPorts(services map[string]interface{}, keepDirect bool, envMaps ...map[string]string) ([]HostPortBinding, error) {
	var extracted []HostPortBinding

	svcNames := make([]string, 0, len(services))
	for name := range services {
		svcNames = append(svcNames, name)
	}
	sort.Strings(svcNames)

	for _, svcName := range svcNames {
		svc, ok := toStringMap(services[svcName])
		if !ok || hasServiceProfiles(svc) {
			continue
		}
		rawPorts, hasPorts := svc["ports"]
		if !hasPorts || rawPorts == nil {
			continue
		}

		var keptPorts []interface{}
		var newExpose []string

		handleEntry := func(entry interface{}) error {
			binding, isHostPub, err := parsePortEntry(svcName, entry, envMaps...)
			if err != nil {
				return err
			}
			if !isHostPub {
				keptPorts = append(keptPorts, entry)
				return nil
			}
			binding.Direct = isDirectHostPort(binding, svc)
			extracted = append(extracted, binding)
			if keepDirect && binding.Direct {
				keptPorts = append(keptPorts, formatDirectPortEntry(binding))
			} else {
				newExpose = append(newExpose, strconv.Itoa(binding.ContainerPort))
			}
			return nil
		}

		switch portsList := rawPorts.(type) {
		case []interface{}:
			for _, p := range portsList {
				if err := handleEntry(p); err != nil {
					return nil, err
				}
			}
		case string:
			if err := handleEntry(portsList); err != nil {
				return nil, err
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
	seen := make(map[string]bool, len(current)+len(ports))
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

func toStringMap(v interface{}) (map[string]interface{}, bool) {
	switch m := v.(type) {
	case nil:
		return map[string]interface{}{}, true
	case map[string]interface{}:
		return m, true
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(m))
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
