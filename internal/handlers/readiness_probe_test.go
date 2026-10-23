package handlers

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
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
