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

	"panel/internal/caddy"
	"panel/internal/db"
	"panel/internal/dockerapi"

	"gopkg.in/yaml.v3"
)

// ReadinessProbeOptions defines parameters for probing a container's readiness before cutover.
type ReadinessProbeOptions struct {
	ContainerName string
	// TargetPort is a single fallback TCP port (kept for callers/tests). Prefer TargetPorts.
	TargetPort int
	// TargetPorts is the ordered list of TCP ports this specific container is expected to listen on.
	TargetPorts []int
	// SkipHTTP skips Tier 2 HTTP probes (used for direct raw-TCP services like RustDesk/DBs).
	SkipHTTP bool
	// AllowRunningWithoutPort allows portless/UDP-only or network-isolated containers to pass via
	// Docker native HEALTHCHECK (when configured) or a brief crash-free running settle window.
	AllowRunningWithoutPort bool
	// SettleDelay controls how long a running container without a reachable TCP port or healthcheck
	// must stay up without restarting before passing (defaults to 2s).
	SettleDelay time.Duration
	Timeout     time.Duration
	LogW        io.Writer

	inspectFn func(context.Context, string) (dockerapi.ContainerReadinessInfo, error)
}

func dedupePositivePorts(ports ...int) []int {
	seen := make(map[int]struct{}, len(ports))
	out := make([]int, 0, len(ports))
	for _, p := range ports {
		if p <= 0 || p > 65535 {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func formatPortList(ports []int) string {
	parts := make([]string, len(ports))
	for i, p := range ports {
		parts[i] = strconv.Itoa(p)
	}
	return strings.Join(parts, ",")
}

// resolveServiceReadinessTarget determines the TCP ports a specific compose service should be probed
// on, plus whether HTTP probing should be skipped (when every target port is a direct non-HTTP L4 port).
func resolveServiceReadinessTarget(
	service string,
	totalServices int,
	domains []db.AppDomain,
	bindings []caddy.HostPortBinding,
	baseCompose []byte,
) (ports []int, skipHTTP bool) {
	service = strings.TrimSpace(service)
	var domainPorts []int
	for _, d := range domains {
		if d.Port <= 0 {
			continue
		}
		dSvc := strings.TrimSpace(d.Service)
		if (service != "" && strings.EqualFold(dSvc, service)) || (dSvc == "" && totalServices <= 1) {
			domainPorts = append(domainPorts, d.Port)
		}
	}

	var tcpBindingPorts []int
	allBindingsDirect := true
	hasTCPBinding := false
	for _, b := range bindings {
		if b.ContainerPort <= 0 || strings.EqualFold(b.Protocol, "udp") {
			continue
		}
		bSvc := strings.TrimSpace(b.Service)
		if (service != "" && strings.EqualFold(bSvc, service)) || (bSvc == "" && totalServices <= 1) {
			tcpBindingPorts = append(tcpBindingPorts, b.ContainerPort)
			hasTCPBinding = true
			if !b.Direct {
				allBindingsDirect = false
			}
		}
	}

	if len(domainPorts) > 0 || hasTCPBinding {
		combined := dedupePositivePorts(append(domainPorts, tcpBindingPorts...)...)
		return combined, len(domainPorts) == 0 && hasTCPBinding && allBindingsDirect
	}

	if exposePorts := serviceComposeExposeTCPPorts(baseCompose, service); len(exposePorts) > 0 {
		return exposePorts, false
	}
	return nil, false
}

func serviceComposeExposeTCPPorts(baseCompose []byte, service string) []int {
	if len(baseCompose) == 0 || strings.TrimSpace(service) == "" {
		return nil
	}
	var doc struct {
		Services map[string]struct {
			Expose []any `yaml:"expose"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(baseCompose, &doc); err != nil {
		return nil
	}
	svc, ok := doc.Services[service]
	if !ok {
		for k, v := range doc.Services {
			if strings.EqualFold(k, service) {
				svc = v
				ok = true
				break
			}
		}
	}
	if !ok || len(svc.Expose) == 0 {
		return nil
	}
	var out []int
	for _, item := range svc.Expose {
		raw := strings.ToLower(strings.TrimSpace(fmt.Sprint(item)))
		if strings.HasSuffix(raw, "/udp") {
			continue
		}
		raw = strings.TrimSuffix(raw, "/tcp")
		if p, err := strconv.Atoi(raw); err == nil && p > 0 && p <= 65535 {
			out = append(out, p)
		}
	}
	return dedupePositivePorts(out...)
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
// Tier 2: HTTP Non-5xx Probe (/health, /api/health, /) — skipped for direct non-HTTP L4 ports
// Tier 3: Raw TCP Port Connect Probe
// For portless/UDP-only or network-isolated containers (when AllowRunningWithoutPort is set),
// it waits for Tier 1 healthcheck if configured, or verifies crash-free running state.
func (p *Panel) ProbeContainerReadiness(ctx context.Context, opts ReadinessProbeOptions) error {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	settleDelay := opts.SettleDelay
	if settleDelay <= 0 {
		settleDelay = 2 * time.Second
	}
	targetPorts := dedupePositivePorts(opts.TargetPorts...)
	if len(targetPorts) == 0 && opts.TargetPort > 0 {
		targetPorts = []int{opts.TargetPort}
	}
	if len(targetPorts) == 0 && !opts.AllowRunningWithoutPort {
		targetPorts = []int{80}
	}
	inspect := opts.inspectFn
	if inspect == nil {
		inspect = dockerapi.InspectContainerReadiness
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
	if len(targetPorts) > 0 {
		logProbe(opts.LogW, "[readiness] Initiating readiness probe cascade for %s on port(s) %s (timeout: %v)...", opts.ContainerName, formatPortList(targetPorts), timeout)
	} else {
		logProbe(opts.LogW, "[readiness] Initiating container state/health readiness check for %s (no TCP port declared, timeout: %v)...", opts.ContainerName, timeout)
	}

	var runningSince time.Time

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
		info, err := inspect(ctx, opts.ContainerName)
		if err == nil {
			if !info.Running && (info.Status == "exited" || info.Status == "dead" || info.ExitCode > 0) {
				return fmt.Errorf("container %s crashed or exited during readiness check (status: %s, code: %d)", opts.ContainerName, info.Status, info.ExitCode)
			}

			// Tier 1: Native Docker Healthcheck
			if strings.EqualFold(info.Health, "healthy") {
				logProbe(opts.LogW, "[readiness] ✓ Container %s passed Tier 1 Docker native healthcheck (healthy).", opts.ContainerName)
				return nil
			}

			// Portless/UDP-only services or direct-L4 services kept off the shared NextDeploy
			// network cannot be TCP-probed from the panel container. Wait for Docker healthcheck
			// when present, otherwise verify the container stays running without crash-looping.
			if opts.AllowRunningWithoutPort && (len(targetPorts) == 0 || !info.OnPanelNetwork) {
				if info.Health == "" && info.Running {
					if info.RestartCount > 0 {
						return fmt.Errorf("container %s restarted during readiness check (restart_count: %d)", opts.ContainerName, info.RestartCount)
					}
					if runningSince.IsZero() {
						runningSince = time.Now()
					}
					if time.Since(runningSince) >= settleDelay {
						if len(targetPorts) == 0 {
							logProbe(opts.LogW, "[readiness] ✓ Container %s is running (no TCP port or healthcheck configured).", opts.ContainerName)
						} else {
							logProbe(opts.LogW, "[readiness] ✓ Container %s is running on isolated/direct network (no native healthcheck configured).", opts.ContainerName)
						}
						return nil
					}
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-ticker.C:
				}
				continue
			}
		}

		if len(targetPorts) > 0 {
			// Resolve candidate host addresses
			var candidateHosts []string
			if info.IPAddress != "" {
				candidateHosts = append(candidateHosts, info.IPAddress)
			}
			if opts.ContainerName != "" {
				candidateHosts = append(candidateHosts, opts.ContainerName)
			}
			if err != nil || info.IPAddress == "" {
				candidateHosts = append(candidateHosts, "127.0.0.1")
			}

			// Tier 2: HTTP Non-5xx Probe (skipped for direct raw-TCP services)
			if !opts.SkipHTTP {
				for _, targetPort := range targetPorts {
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
									logProbe(opts.LogW, "[readiness] ✓ Container %s passed Tier 2 HTTP probe (port %d %s returned %d).", opts.ContainerName, targetPort, ep, resp.StatusCode)
									return nil
								}
							}
						}
					}
				}
			}

			// Tier 3: TCP Port Connect Probe
			for _, targetPort := range targetPorts {
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
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
