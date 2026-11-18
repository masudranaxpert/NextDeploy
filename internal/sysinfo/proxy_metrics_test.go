package sysinfo

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestParseProxyRequestsTotal(t *testing.T) {
	t.Run("caddy prometheus", func(t *testing.T) {
		body := `# HELP caddy_http_requests_total Counter of HTTP(S) requests made.
# TYPE caddy_http_requests_total counter
caddy_http_requests_total{handler="subroute",server="srv0"} 120
caddy_http_requests_total{handler="subroute",server="srv0"} 30
`
		got, ok := parseProxyRequestsTotal(body)
		if !ok || got != 150 {
			t.Fatalf("got (%d, %v), want (150, true)", got, ok)
		}
	})

	t.Run("angie prometheus server zones", func(t *testing.T) {
		body := `# TYPE angie_http_server_zones_requests counter
angie_http_server_zones_requests{zone="site_a"} 420
angie_http_server_zones_requests{zone="site_b"} 80 1731920000
`
		got, ok := parseProxyRequestsTotal(body)
		if !ok || got != 500 {
			t.Fatalf("got (%d, %v), want (500, true)", got, ok)
		}
	})

	t.Run("angie status json", func(t *testing.T) {
		body := `{"http":{"server_zones":{"app1":{"requests":{"total":75}},"app2":{"requests":{"total":25}}}}}`
		got, ok := parseProxyRequestsTotal(body)
		if !ok || got != 100 {
			t.Fatalf("got (%d, %v), want (100, true)", got, ok)
		}
	})

	t.Run("ferron counter", func(t *testing.T) {
		body := "ferron_http_requests_total{host=\"example.com\"} 1.5e+02\n"
		got, ok := parseProxyRequestsTotal(body)
		if !ok || got != 150 {
			t.Fatalf("got (%d, %v), want (150, true)", got, ok)
		}
	})

	t.Run("unrecognized output returns false", func(t *testing.T) {
		if got, ok := parseProxyRequestsTotal("go_goroutines 12\n"); ok || got != 0 {
			t.Fatalf("got (%d, %v), want (0, false)", got, ok)
		}
	})
}

func TestQueryProxyRequestsTotalEndpointFallback(t *testing.T) {
	t.Cleanup(func() { SetProxyMetricsEndpoints(nil) })

	var p8sConfigPosts atomic.Int32
	failingSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			p8sConfigPosts.Add(1)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer failingSrv.Close()

	angieSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/p8s" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("angie_http_server_zones_requests{zone=\"main\"} 345\n"))
	}))
	defer angieSrv.Close()

	SetProxyMetricsEndpoints([]string{
		failingSrv.URL + "/metrics",
		angieSrv.URL + "/p8s",
	})

	total, ok := queryProxyRequestsTotal()
	if !ok || total != 345 {
		t.Fatalf("queryProxyRequestsTotal() = (%d, %v), want (345, true)", total, ok)
	}
	if p8sConfigPosts.Load() != 0 {
		t.Fatalf("unexpected POST to config endpoint on non-200 failure")
	}
}
