package dockerapi

import (
	"context"
	"os"
	"strings"

	"github.com/docker/docker/api/types"
)

// EnsureProxySharedNetwork ensures that containerName (default "caddy") shares
// the same Docker bridge network as the NextDeploy panel container (default "NextDeploy").
func EnsureProxySharedNetwork(ctx context.Context, containerName string) error {
	target := strings.TrimSpace(containerName)
	if target == "" {
		target = "caddy"
	}
	panelNet := detectPanelNetwork(ctx)
	return EnsureContainerNetwork(ctx, target, panelNet)
}

func detectPanelNetwork(ctx context.Context) string {
	panelNet := "NextDeploy"
	cli, err := apiClient()
	if err != nil {
		return panelNet
	}
	candNames := []string{"panel", os.Getenv("HOSTNAME")}
	for _, name := range candNames {
		if strings.TrimSpace(name) == "" {
			continue
		}
		if insp, err := cli.ContainerInspect(ctx, name); err == nil && insp.NetworkSettings != nil {
			for netName := range insp.NetworkSettings.Networks {
				if netName != "bridge" && netName != "host" && netName != "none" {
					return netName
				}
			}
			return panelNet
		}
	}
	return panelNet
}

// matchContainerName checks whether any Docker container name in names matches target.
// Proxy names use exact canonical aliases rather than broad substring matching.
func matchContainerName(names []string, target string) bool {
	target = strings.TrimPrefix(strings.TrimSpace(target), "/")
	if target == "" {
		return false
	}
	for _, n := range names {
		trimmed := strings.TrimPrefix(strings.TrimSpace(n), "/")
		if trimmed == target {
			return true
		}
		if target == "caddy" && trimmed == "nextdeploy_caddy" {
			return true
		}
		if target == "nd-proxy" && trimmed == "nextdeploy_nd-proxy" {
			return true
		}
	}
	return false
}

func classifyProxyContainer(name, proj string, labels map[string]string) (isProxy bool, isLegacyCaddy bool) {
	svc := strings.TrimSpace(labels["com.docker.compose.service"])
	isLegacyCaddy = strings.EqualFold(name, "caddy") ||
		strings.EqualFold(name, "nextdeploy_caddy") ||
		(strings.EqualFold(proj, "nextdeploy") && strings.EqualFold(svc, "caddy"))
	isNDProxy := strings.EqualFold(name, "nd-proxy") ||
		strings.EqualFold(name, "nextdeploy_nd-proxy") ||
		(strings.EqualFold(proj, "nextdeploy") && strings.EqualFold(svc, "proxy"))
	return isLegacyCaddy || isNDProxy, isLegacyCaddy
}

func classifyLivePortState(list []types.Container) *LivePortState {
	st := &LivePortState{
		DirectHostPorts:   make(map[int]bool),
		CaddyGatewayPorts: make(map[int]bool),
		CaddyBoundPorts:   make(map[int]bool),
		RunningProjects:   make(map[string]bool),
	}
	for _, c := range list {
		if !strings.EqualFold(c.State, "running") {
			continue
		}
		proj := strings.TrimSpace(c.Labels[composeProjectLabel])
		if proj != "" {
			st.RunningProjects[proj] = true
		}
		name := containerName(c)
		isProxy, isLegacyCaddy := classifyProxyContainer(name, proj, c.Labels)
		if isProxy && st.ProxyContainer == "" {
			st.ProxyContainer = name
		}

		for _, p := range c.Ports {
			if p.PublicPort == 0 {
				continue
			}
			port := int(p.PublicPort)
			if isProxy {
				if port != 80 && port != 443 {
					st.CaddyBoundPorts[port] = true
				}
			} else {
				st.DirectHostPorts[port] = true
			}
		}
		if isProxy {
			if isLegacyCaddy {
				if _, ok := c.Labels["caddy.auto_https"]; ok {
					st.CaddyHasLegacyAutoHTTPS = true
				}
			}
		} else {
			extractCaddyLabelPorts(c.Labels, st.CaddyGatewayPorts)
		}
	}
	return st
}
