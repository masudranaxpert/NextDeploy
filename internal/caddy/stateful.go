package caddy

import (
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Blue/green deploys start the new slot next to the old one. Named volumes are shared by both
// slots (see normalizeNamedVolumes), so a database service would run twice on the same data
// directory. Postgres, MySQL and friends cannot detect that across containers (each sees itself
// as PID 1 and treats the other's lock file as stale), and the two instances corrupt the files.
// Such services must only ever run once.

// statefulImages are image names (last path segment, without tag) of servers that own their
// data directory and must never run twice on it.
var statefulImages = map[string]bool{
	"postgres": true, "postgis": true, "timescaledb": true, "timescaledb-ha": true, "pgvector": true,
	"mysql": true, "mariadb": true, "percona": true, "percona-server": true,
	"mongo": true, "mongodb": true, "mongodb-community-server": true,
	"redis": true, "redis-stack": true, "redis-stack-server": true, "valkey": true, "keydb": true, "dragonfly": true,
	"elasticsearch": true, "opensearch": true, "meilisearch": true, "typesense": true,
	"clickhouse-server": true, "clickhouse": true, "influxdb": true, "questdb": true,
	"cockroach": true, "couchdb": true, "neo4j": true, "cassandra": true, "scylla": true,
	"rabbitmq": true, "nats": true, "minio": true, "qdrant": true, "weaviate": true, "milvus": true,
	"etcd": true, "zookeeper": true, "kafka": true, "surrealdb": true,
}

// StatefulLabel lets a compose service say what it is: "true" forces single-instance handling,
// "false" opts a service out (for example a read replica or a cache that may run twice).
const StatefulLabel = "nd.stateful"

// StatefulServices lists the services of a compose file that keep their data in a named volume
// and must not run in two slots at once. Unparsable input yields nil.
func StatefulServices(compose []byte) []string {
	var doc map[string]interface{}
	if err := yaml.Unmarshal(compose, &doc); err != nil || doc == nil {
		return nil
	}
	services, ok := toStringMap(doc["services"])
	if !ok {
		return nil
	}
	var out []string
	for name, raw := range services {
		svc, ok := toStringMap(raw)
		if !ok {
			continue
		}
		switch strings.ToLower(serviceLabel(svc, StatefulLabel)) {
		case "true", "1", "yes":
			out = append(out, name)
			continue
		case "false", "0", "no":
			continue
		}
		if !statefulImage(svc["image"]) || !hasNamedVolume(svc) {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func statefulImage(raw interface{}) bool {
	img, _ := raw.(string)
	img = strings.ToLower(strings.TrimSpace(img))
	if img == "" {
		return false
	}
	if at := strings.IndexByte(img, '@'); at >= 0 {
		img = img[:at]
	}
	if slash := strings.LastIndexByte(img, '/'); slash >= 0 {
		img = img[slash+1:]
	}
	if colon := strings.IndexByte(img, ':'); colon >= 0 {
		img = img[:colon]
	}
	return statefulImages[img]
}

// hasNamedVolume reports whether the service mounts a named volume (not a bind mount or tmpfs):
// a database without one keeps its data inside the container, so two copies cannot collide.
func hasNamedVolume(svc map[string]interface{}) bool {
	list, _ := svc["volumes"].([]interface{})
	for _, v := range list {
		switch m := v.(type) {
		case string:
			src := strings.SplitN(m, ":", 2)[0]
			if len(strings.SplitN(m, ":", 2)) == 2 && src != "" && !strings.HasPrefix(src, ".") &&
				!strings.HasPrefix(src, "/") && !strings.HasPrefix(src, "~") && !strings.HasPrefix(src, "$") {
				return true
			}
		case map[string]interface{}:
			t, _ := m["type"].(string)
			src, _ := m["source"].(string)
			if (t == "" || t == "volume") && src != "" {
				return true
			}
		}
	}
	return false
}

func serviceLabel(svc map[string]interface{}, key string) string {
	switch l := svc["labels"].(type) {
	case map[string]interface{}:
		if v, ok := l[key]; ok {
			s, _ := v.(string)
			if b, isBool := v.(bool); isBool {
				if b {
					return "true"
				}
				return "false"
			}
			return strings.TrimSpace(s)
		}
	case []interface{}:
		for _, e := range l {
			s, _ := e.(string)
			if k, v, ok := strings.Cut(s, "="); ok && strings.TrimSpace(k) == key {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}
