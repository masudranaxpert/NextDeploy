package sysinfo

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// HostCounters are running request totals of one host name, split by response class.
type HostCounters struct {
	Requests, S2xx, S3xx, S4xx, S5xx uint64
}

var (
	perHostMu      sync.Mutex
	perHostTriedAt time.Time
)

// CaddyHostCounters reads Caddy's Prometheus metrics and returns request totals per host.
// Caddy labels its metrics with the host only when per-host metrics are on; when they are
// off, it asks Caddy's admin API to turn them on (at most every 10 minutes; a Caddy that does
// not know the option rejects it and nothing changes) and reports ok=false for now.
func CaddyHostCounters() (map[string]HostCounters, bool) {
	for _, u := range proxyMetricsEndpointsSnapshot() {
		if !isCaddyMetricsEndpoint(u) {
			continue
		}
		resp, err := proxyMetricsClient.Get(u)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			continue
		}
		hosts, ok := parseCaddyHostCounters(string(body))
		if !ok {
			enableCaddyPerHost(u)
			return nil, false
		}
		return hosts, true
	}
	return nil, false
}

func enableCaddyPerHost(metricsURL string) {
	perHostMu.Lock()
	if time.Since(perHostTriedAt) < 10*time.Minute {
		perHostMu.Unlock()
		return
	}
	perHostTriedAt = time.Now()
	perHostMu.Unlock()
	go func() {
		configURL := strings.Replace(metricsURL, "/metrics", "/config/apps/http/metrics", 1)
		req, err := http.NewRequest(http.MethodPost, configURL, strings.NewReader(`{"per_host":true}`))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if resp, err := proxyMetricsClient.Do(req); err == nil {
			resp.Body.Close()
		}
	}()
}

// parseCaddyHostCounters sums caddy_http_request_duration_seconds_count by host and status
// code. Every handler a request passes through (subroute, reverse_proxy, ...) counts it, so
// per host the handler with the most requests stands for the whole host.
func parseCaddyHostCounters(metrics string) (map[string]HostCounters, bool) {
	type key struct{ host, handler string }
	per := map[key]*HostCounters{}
	sawHost := false
	for _, raw := range strings.Split(metrics, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "caddy_http_request_duration_seconds_count{") {
			continue
		}
		end := strings.LastIndexByte(line, '}')
		if end < 0 {
			continue
		}
		labels := parseLabels(line[len("caddy_http_request_duration_seconds_count{"):end])
		host := strings.ToLower(labels["host"])
		if host == "" {
			continue
		}
		sawHost = true
		fields := strings.Fields(line[end+1:])
		if len(fields) == 0 {
			continue
		}
		v, err := strconv.ParseFloat(fields[0], 64)
		if err != nil || v < 0 {
			continue
		}
		n := uint64(v)
		k := key{host, labels["handler"]}
		c := per[k]
		if c == nil {
			c = &HostCounters{}
			per[k] = c
		}
		c.Requests += n
		if code := labels["code"]; code != "" {
			switch code[0] {
			case '2':
				c.S2xx += n
			case '3':
				c.S3xx += n
			case '4':
				c.S4xx += n
			case '5':
				c.S5xx += n
			}
		}
	}
	if !sawHost {
		return nil, false
	}
	out := map[string]HostCounters{}
	for k, c := range per {
		if c.Requests > out[k.host].Requests {
			out[k.host] = *c
		}
	}
	return out, true
}

// parseLabels reads `a="x",b="y"` (values may contain escaped quotes).
func parseLabels(s string) map[string]string {
	out := map[string]string{}
	for len(s) > 0 {
		eq := strings.IndexByte(s, '=')
		if eq < 0 || eq+1 >= len(s) || s[eq+1] != '"' {
			break
		}
		name := strings.TrimSpace(strings.TrimPrefix(s[:eq], ","))
		i := eq + 2
		var val strings.Builder
		for i < len(s) && s[i] != '"' {
			if s[i] == '\\' && i+1 < len(s) {
				i++
			}
			val.WriteByte(s[i])
			i++
		}
		out[name] = val.String()
		if i+1 >= len(s) {
			break
		}
		s = s[i+1:]
	}
	return out
}
