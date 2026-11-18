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
	4222:  true, // NATS
	5432:  true, // PostgreSQL
	5672:  true, // RabbitMQ AMQP
	6379:  true, // Redis/Valkey
	6380:  true, // Redis TLS
	8883:  true, // MQTT TLS
	9042:  true, // Cassandra
	9092:  true, // Kafka
	11211: true, // Memcached
	26257: true, // CockroachDB
	27017: true, // MongoDB
	27018: true, // MongoDB
	27019: true, // MongoDB
	33060: true, // MySQL X
}

func isDirectHostPort(b HostPortBinding, svc map[string]interface{}) bool {
	if strings.EqualFold(b.Protocol, "udp") {
		return true
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
