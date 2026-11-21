package handlers

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/dockerapi"
)

func TestReadinessProbeCascade_HTTPPass(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	_, portStr, err := net.SplitHostPort(ts.Listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to split host port: %v", err)
	}
	port, _ := strconv.Atoi(portStr)

	p := &Panel{}
	var logBuf bytes.Buffer
	err = p.ProbeContainerReadiness(context.Background(), ReadinessProbeOptions{
		ContainerName: "127.0.0.1",
		TargetPort:    port,
		Timeout:       5 * time.Second,
		LogW:          &logBuf,
	})
	if err != nil {
		t.Fatalf("expected readiness probe to pass, got: %v", err)
	}
	if !strings.Contains(logBuf.String(), "passed Tier 2 HTTP probe") {
		t.Errorf("expected Tier 2 log, got:\n%s", logBuf.String())
	}
}

func TestReadinessProbeCascade_TCPPass(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	p := &Panel{}
	var logBuf bytes.Buffer
	err = p.ProbeContainerReadiness(context.Background(), ReadinessProbeOptions{
		ContainerName: "127.0.0.1",
		TargetPort:    port,
		Timeout:       5 * time.Second,
		LogW:          &logBuf,
	})
	if err != nil {
		t.Fatalf("expected TCP probe to pass, got: %v", err)
	}
	if !strings.Contains(logBuf.String(), "passed Tier 3 TCP probe") {
		t.Errorf("expected Tier 3 log, got:\n%s", logBuf.String())
	}
}

func TestReadinessProbeCascade_Timeout(t *testing.T) {
	p := &Panel{}
	var logBuf bytes.Buffer
	// Port 59999 should not be listening
	err := p.ProbeContainerReadiness(context.Background(), ReadinessProbeOptions{
		ContainerName: "127.0.0.1",
		TargetPort:    59999,
		Timeout:       2 * time.Second,
		LogW:          &logBuf,
	})
	if err == nil {
		t.Fatalf("expected probe to timeout, but it passed")
	}
	if !strings.Contains(err.Error(), "failed readiness probe after") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestResolveServiceReadinessTarget_MultiServiceAndRustDesk(t *testing.T) {
	base := []byte(`
services:
  hbbs:
    image: rustdesk/rustdesk-server:latest
  hbbr:
    image: rustdesk/rustdesk-server:latest
  web:
    image: myapp/web:latest
  internal_api:
    image: myapp/api:latest
    expose:
      - "8080"
      - "9000/udp"
  worker:
    image: myapp/worker:latest
  wg:
    image: linuxserver/wireguard:latest
`)
	domains := []db.AppDomain{
		{Service: "web", Domain: "app.example.com", Port: 3000},
	}
	bindings := []caddy.HostPortBinding{
		{Service: "hbbr", HostPort: 21117, ContainerPort: 21117, Protocol: "tcp", Direct: true},
		{Service: "hbbs", HostPort: 21115, ContainerPort: 21115, Protocol: "tcp", Direct: true},
		{Service: "hbbs", HostPort: 21116, ContainerPort: 21116, Protocol: "tcp", Direct: true},
		{Service: "hbbs", HostPort: 21116, ContainerPort: 21116, Protocol: "udp", Direct: true},
		{Service: "wg", HostPort: 51820, ContainerPort: 51820, Protocol: "udp", Direct: true},
	}

	hbbsPorts, hbbsSkipHTTP := resolveServiceReadinessTarget("hbbs", 6, domains, bindings, base)
	if !reflect.DeepEqual(hbbsPorts, []int{21115, 21116}) || !hbbsSkipHTTP {
		t.Fatalf("hbbs: got ports=%v skipHTTP=%v, want [21115 21116] true", hbbsPorts, hbbsSkipHTTP)
	}

	hbbrPorts, hbbrSkipHTTP := resolveServiceReadinessTarget("hbbr", 6, domains, bindings, base)
	if !reflect.DeepEqual(hbbrPorts, []int{21117}) || !hbbrSkipHTTP {
		t.Fatalf("hbbr: got ports=%v skipHTTP=%v, want [21117] true", hbbrPorts, hbbrSkipHTTP)
	}

	webPorts, webSkipHTTP := resolveServiceReadinessTarget("web", 6, domains, bindings, base)
	if !reflect.DeepEqual(webPorts, []int{3000}) || webSkipHTTP {
		t.Fatalf("web: got ports=%v skipHTTP=%v, want [3000] false", webPorts, webSkipHTTP)
	}

	apiPorts, apiSkipHTTP := resolveServiceReadinessTarget("internal_api", 6, domains, bindings, base)
	if !reflect.DeepEqual(apiPorts, []int{8080}) || apiSkipHTTP {
		t.Fatalf("internal_api: got ports=%v skipHTTP=%v, want [8080] false", apiPorts, apiSkipHTTP)
	}

	workerPorts, _ := resolveServiceReadinessTarget("worker", 6, domains, bindings, base)
	if len(workerPorts) != 0 {
		t.Fatalf("worker: expected no TCP ports, got %v", workerPorts)
	}

	wgPorts, _ := resolveServiceReadinessTarget("wg", 6, domains, bindings, base)
	if len(wgPorts) != 0 {
		t.Fatalf("wg (UDP-only): expected no TCP ports, got %v", wgPorts)
	}
}

func TestReadinessProbe_SkipHTTPDirectTCP(t *testing.T) {
	var httpHits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	_, portStr, _ := net.SplitHostPort(ts.Listener.Addr().String())
	port, _ := strconv.Atoi(portStr)

	p := &Panel{}
	var logBuf bytes.Buffer
	err := p.ProbeContainerReadiness(context.Background(), ReadinessProbeOptions{
		ContainerName: "127.0.0.1",
		TargetPorts:   []int{port},
		SkipHTTP:      true,
		Timeout:       3 * time.Second,
		LogW:          &logBuf,
	})
	if err != nil {
		t.Fatalf("expected direct TCP probe to pass, got: %v", err)
	}
	if httpHits.Load() != 0 {
		t.Fatalf("expected 0 HTTP hits when SkipHTTP=true, got %d", httpHits.Load())
	}
	if !strings.Contains(logBuf.String(), "passed Tier 3 TCP probe") {
		t.Fatalf("expected Tier 3 TCP log, got:\n%s", logBuf.String())
	}
}

func TestReadinessProbe_IsolatedDirectAndPortlessContainers(t *testing.T) {
	p := &Panel{}

	// 1. Direct-L4 container not attached to NextDeploy network (no native healthcheck) settles cleanly.
	var logBuf bytes.Buffer
	err := p.ProbeContainerReadiness(context.Background(), ReadinessProbeOptions{
		ContainerName:           "rustdesk-blue-hbbs-1",
		TargetPorts:             []int{21115, 21116},
		SkipHTTP:                true,
		AllowRunningWithoutPort: true,
		SettleDelay:             250 * time.Millisecond,
		Timeout:                 3 * time.Second,
		LogW:                    &logBuf,
		inspectFn: func(context.Context, string) (dockerapi.ContainerReadinessInfo, error) {
			return dockerapi.ContainerReadinessInfo{
				Running:        true,
				Status:         "running",
				IPAddress:      "172.22.0.2",
				OnPanelNetwork: false,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("expected isolated direct container to pass settle check, got: %v", err)
	}

	// 2. Portless container that restarts during settle must fail fast.
	err = p.ProbeContainerReadiness(context.Background(), ReadinessProbeOptions{
		ContainerName:           "worker-crashloop",
		AllowRunningWithoutPort: true,
		SettleDelay:             500 * time.Millisecond,
		Timeout:                 3 * time.Second,
		inspectFn: func(context.Context, string) (dockerapi.ContainerReadinessInfo, error) {
			return dockerapi.ContainerReadinessInfo{
				Running:      true,
				Status:       "running",
				RestartCount: 1,
			}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "restarted during readiness check") {
		t.Fatalf("expected crashloop failure, got: %v", err)
	}

	// 3. Isolated container WITH native Docker healthcheck waits until healthy.
	var polls atomic.Int32
	err = p.ProbeContainerReadiness(context.Background(), ReadinessProbeOptions{
		ContainerName:           "postgres-isolated",
		TargetPorts:             []int{5432},
		SkipHTTP:                true,
		AllowRunningWithoutPort: true,
		SettleDelay:             100 * time.Millisecond,
		Timeout:                 3 * time.Second,
		inspectFn: func(context.Context, string) (dockerapi.ContainerReadinessInfo, error) {
			n := polls.Add(1)
			health := "starting"
			if n >= 2 {
				health = "healthy"
			}
			return dockerapi.ContainerReadinessInfo{
				Running:        true,
				Status:         "running",
				Health:         health,
				OnPanelNetwork: false,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("expected healthy isolated container to pass Tier 1, got: %v", err)
	}
	if polls.Load() < 2 {
		t.Fatalf("expected probe to wait for healthcheck transition, polls=%d", polls.Load())
	}
}
