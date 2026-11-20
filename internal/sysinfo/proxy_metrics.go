package sysinfo

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

var defaultProxyMetricsEndpoints = []string{
	"http://caddy:2019/metrics",
	"http://127.0.0.1:2019/metrics",
	"http://nd-proxy:2019/p8s",
	"http://127.0.0.1:2019/p8s",
}

var (
	proxyMetricsMu        sync.RWMutex
	proxyMetricsEndpoints []string
	proxyMetricsClient    = &http.Client{
		Timeout: 2 * time.Second,
	}
)

var proxyRequestMetricPriority = []string{
	"caddy_http_requests_total",
	"angie_http_server_zones_requests",
	"angie_http_server_zone_requests_total",
	"angie_http_requests_total",
	"ferron_http_requests_total",
	"http_server_requests_total",
}

// SetProxyMetricsEndpoints overrides the URLs probed by the proxy request rate scraper.
// Passing an empty slice restores the default Caddy and Angie/Ferron endpoints.
func SetProxyMetricsEndpoints(urls []string) {
	cleaned := make([]string, 0, len(urls))
	for _, u := range urls {
		if trimmed := strings.TrimSpace(u); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	proxyMetricsMu.Lock()
	proxyMetricsEndpoints = cleaned
	proxyMetricsMu.Unlock()
}

func proxyMetricsEndpointsSnapshot() []string {
	proxyMetricsMu.RLock()
	custom := proxyMetricsEndpoints
	proxyMetricsMu.RUnlock()
	if len(custom) > 0 {
		out := make([]string, len(custom))
		copy(out, custom)
		return out
	}
	out := make([]string, len(defaultProxyMetricsEndpoints))
	copy(out, defaultProxyMetricsEndpoints)
	return out
}

// proxyRequestsTotalFn asks the active proxy driver for its request counter (Angie and
// Ferron keep theirs on an endpoint the panel can only reach through the driver). It is set
// by the panel; when it reports nothing, the metrics endpoints below are scraped (Caddy).
var (
	proxyRequestsTotalMu sync.RWMutex
	proxyRequestsTotalFn func(ctx context.Context) (uint64, bool)
)

// SetProxyRequestsTotalFunc installs the driver-backed request counter.
func SetProxyRequestsTotalFunc(fn func(ctx context.Context) (uint64, bool)) {
	proxyRequestsTotalMu.Lock()
	proxyRequestsTotalFn = fn
	proxyRequestsTotalMu.Unlock()
}

func queryProxyRequestsTotal() (uint64, bool) {
	proxyRequestsTotalMu.RLock()
	fn := proxyRequestsTotalFn
	proxyRequestsTotalMu.RUnlock()
	if fn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		total, ok := fn(ctx)
		cancel()
		if ok {
			return total, true
		}
	}
	for _, u := range proxyMetricsEndpointsSnapshot() {
		resp, err := proxyMetricsClient.Get(u)
		if err != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
		resp.Body.Close()
		if err != nil {
			continue
		}
		total, found := parseProxyRequestsTotal(string(body))
		if !found {
			if isCaddyMetricsEndpoint(u) {
				go enableCaddyMetricsOnce(u)
			}
			continue
		}
		return total, true
	}
	return 0, false
}

func isCaddyMetricsEndpoint(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return strings.HasSuffix(strings.TrimSuffix(rawURL, "/"), "/metrics")
	}
	return strings.TrimSuffix(u.Path, "/") == "/metrics"
}

func enableCaddyMetricsOnce(metricsURL string) {
	configURL := strings.Replace(metricsURL, "/metrics", "/config/apps/http/metrics", 1)
	req, err := http.NewRequest(http.MethodPost, configURL, strings.NewReader("{}"))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if resp, err := proxyMetricsClient.Do(req); err == nil && resp != nil {
		resp.Body.Close()
	}
}

func parseCaddyHttpRequestsTotal(metricsData string) (uint64, bool) {
	return parseProxyRequestsTotal(metricsData)
}

func parseProxyRequestsTotal(metricsData string) (uint64, bool) {
	trimmed := strings.TrimSpace(metricsData)
	if strings.HasPrefix(trimmed, "{") {
		return parseAngieStatusRequestsTotal(trimmed)
	}

	sums := make(map[string]uint64)
	seen := make(map[string]bool)
	for _, rawLine := range strings.Split(metricsData, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, val, ok := parsePrometheusSample(line)
		if !ok {
			continue
		}
		if isKnownProxyMetric(name) {
			sums[name] += val
			seen[name] = true
		}
	}
	for _, candidate := range proxyRequestMetricPriority {
		if seen[candidate] {
			return sums[candidate], true
		}
	}
	return 0, false
}

func isKnownProxyMetric(name string) bool {
	for _, candidate := range proxyRequestMetricPriority {
		if name == candidate {
			return true
		}
	}
	return false
}

func parsePrometheusSample(line string) (string, uint64, bool) {
	sep := strings.IndexAny(line, "{ \t")
	if sep <= 0 {
		return "", 0, false
	}
	name := line[:sep]
	rest := line[sep:]
	if strings.HasPrefix(rest, "{") {
		endBrace := strings.IndexByte(rest, '}')
		if endBrace < 0 {
			return "", 0, false
		}
		rest = rest[endBrace+1:]
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", 0, false
	}
	val, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || val < 0 {
		return "", 0, false
	}
	return name, uint64(val), true
}

func parseAngieStatusRequestsTotal(rawJSON string) (uint64, bool) {
	var payload struct {
		HTTP struct {
			ServerZones map[string]struct {
				Requests struct {
					Total uint64 `json:"total"`
				} `json:"requests"`
			} `json:"server_zones"`
		} `json:"http"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &payload); err != nil {
		return 0, false
	}
	if len(payload.HTTP.ServerZones) == 0 {
		return 0, false
	}
	var sum uint64
	for _, zone := range payload.HTTP.ServerZones {
		sum += zone.Requests.Total
	}
	return sum, true
}
