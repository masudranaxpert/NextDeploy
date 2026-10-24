package handlers

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"panel/internal/dockerapi"
)

// ReadinessProbeOptions defines parameters for probing a container's readiness before cutover.
type ReadinessProbeOptions struct {
	ContainerName string
	TargetPort    int
	Timeout       time.Duration
	LogW          io.Writer
}

func logProbe(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if !strings.HasSuffix(msg, "\n") {
		msg += "\n"
	}
	_, _ = io.WriteString(w, msg)
}

// ProbeContainerReadiness executes a 3-tier cascade:
// Tier 1: Docker Native Healthcheck
// Tier 2: HTTP Non-5xx Probe (/health, /api/health, /)
// Tier 3: Raw TCP Port Connect Probe
func (p *Panel) ProbeContainerReadiness(ctx context.Context, opts ReadinessProbeOptions) error {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	targetPort := opts.TargetPort
	if targetPort <= 0 {
		targetPort = 80
	}

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	httpClient := &http.Client{
		Timeout: 1 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	endpoints := []string{"/health", "/api/health", "/"}
	logProbe(opts.LogW, "[readiness] Initiating readiness probe cascade for %s on port %d (timeout: %v)...", opts.ContainerName, targetPort, timeout)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("container %s failed readiness probe after %v (all 3 tiers timed out)", opts.ContainerName, timeout)
		}

		// Inspect container state
		info, err := dockerapi.InspectContainerReadiness(ctx, opts.ContainerName)
		if err == nil {
			if !info.Running && (info.Status == "exited" || info.Status == "dead" || info.ExitCode > 0) {
				return fmt.Errorf("container %s crashed or exited during readiness check (status: %s, code: %d)", opts.ContainerName, info.Status, info.ExitCode)
			}

			// Tier 1: Native Docker Healthcheck
			if strings.EqualFold(info.Health, "healthy") {
				logProbe(opts.LogW, "[readiness] ✓ Container %s passed Tier 1 Docker native healthcheck (healthy).", opts.ContainerName)
				return nil
			}
		}

		// Resolve candidate host addresses
		var candidateHosts []string
		if info.IPAddress != "" {
			candidateHosts = append(candidateHosts, info.IPAddress)
		}
		if opts.ContainerName != "" {
			candidateHosts = append(candidateHosts, opts.ContainerName)
		}
		candidateHosts = append(candidateHosts, "127.0.0.1")

		// Tier 2: HTTP Non-5xx Probe
		httpPassed := false
		for _, host := range candidateHosts {
			for _, ep := range endpoints {
				url := fmt.Sprintf("http://%s:%d%s", host, targetPort, ep)
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
				if err != nil {
					continue
				}
				resp, err := httpClient.Do(req)
				if err == nil {
					_ = resp.Body.Close()
					if resp.StatusCode >= 200 && resp.StatusCode < 400 {
						logProbe(opts.LogW, "[readiness] ✓ Container %s passed Tier 2 HTTP probe (%s returned %d).", opts.ContainerName, ep, resp.StatusCode)
						httpPassed = true
						break
					}
				}
			}
			if httpPassed {
				return nil
			}
		}

		// Tier 3: TCP Port Connect Probe
		for _, host := range candidateHosts {
			targetAddr := net.JoinHostPort(host, strconv.Itoa(targetPort))
			d := net.Dialer{Timeout: 1 * time.Second}
			conn, err := d.DialContext(ctx, "tcp", targetAddr)
			if err == nil {
				_ = conn.Close()
				logProbe(opts.LogW, "[readiness] ✓ Container %s passed Tier 3 TCP probe (port %d accepting connections).", opts.ContainerName, targetPort)
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
