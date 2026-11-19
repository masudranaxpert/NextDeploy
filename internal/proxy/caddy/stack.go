package caddy

import (
	"fmt"
	"strings"

	"panel/internal/db"
	"panel/internal/proxy"
	"panel/internal/runutil"
	icaddy "panel/internal/caddy"

	"gopkg.in/yaml.v3"
)

// StackService builds the root Docker Compose service definition and top-level
// named volumes for Caddy by delegating to internal/caddy.GenerateRootStackCompose.
func (d *Driver) StackService(opts proxy.StackOptions) (map[string]any, map[string]any, error) {
	image := strings.TrimSpace(opts.Image)
	if image == "" {
		image = icaddy.CaddyImage
	}
	containerName := strings.TrimSpace(opts.ContainerName)
	if containerName == "" {
		containerName = d.ContainerName()
	}
	workspacesDir := strings.TrimSpace(opts.WorkspacesDir)
	if workspacesDir == "" {
		workspacesDir = "/data/workspaces"
	}

	baseDoc := map[string]any{
		"networks": map[string]any{
			icaddy.PanelNetworkKey: map[string]any{
				"name": icaddy.PanelNetworkName,
			},
		},
		"services": map[string]any{
			icaddy.CaddyContainerName: map[string]any{
				"container_name": containerName,
				"image":          image,
				"restart":        "unless-stopped",
				"logging":        runutil.ComposeLogging(),
				"ports": []any{
					"80:80",
					"443:443/tcp",
					"443:443/udp",
				},
				"volumes": []any{
					"/var/run/docker.sock:/var/run/docker.sock",
					"caddy_data:/data",
					workspacesDir + ":/data/workspaces:ro",
				},
			},
			"panel": map[string]any{
				"image": "nextdeploy-panel",
			},
		},
		"volumes": map[string]any{
			"caddy_data": map[string]any{},
		},
	}

	baseBytes, err := yaml.Marshal(baseDoc)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal caddy base compose: %w", err)
	}
	mergedBytes, err := MergeRootCompose(baseBytes, opts)
	if err != nil {
		return nil, nil, err
	}

	var doc map[string]any
	if err := yaml.Unmarshal(mergedBytes, &doc); err != nil {
		return nil, nil, fmt.Errorf("unmarshal merged caddy compose: %w", err)
	}

	services, _ := doc["services"].(map[string]any)
	caddySvc, ok := services[icaddy.CaddyContainerName].(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("merged compose missing %s service", icaddy.CaddyContainerName)
	}
	applyNetworkOverrides(caddySvc, opts.NetworkKey, opts.NetworkName)

	volumes, _ := doc["volumes"].(map[string]any)
	if volumes == nil {
		volumes = map[string]any{}
	}
	return caddySvc, volumes, nil
}

// MergeRootCompose merges root stack configuration into an existing root compose YAML
// using internal/caddy.GenerateRootStackCompose so Caddy compose output stays identical.
func MergeRootCompose(base []byte, opts proxy.StackOptions) ([]byte, error) {
	image := strings.TrimSpace(opts.Image)
	if image == "" {
		image = icaddy.CaddyImage
	}
	return icaddy.GenerateRootStackCompose(
		base,
		opts.PanelDomain,
		opts.PanelEnableHTTPS,
		opts.PanelEnableWWW,
		opts.ACMEEmail,
		image,
		toCaddySharedMounts(opts.SharedMounts),
		toCaddyGlobalOptions(opts.Global),
		toDBAllocatedPorts(opts.Gateways)...,
	)
}

func applyNetworkOverrides(caddySvc map[string]any, networkKey, networkName string) {
	netKey := strings.TrimSpace(networkKey)
	if netKey != "" && netKey != icaddy.PanelNetworkKey {
		caddySvc["networks"] = []any{netKey}
	}
	netName := strings.TrimSpace(networkName)
	if netName != "" && netName != icaddy.PanelNetworkName {
		if envList, ok := caddySvc["environment"].([]any); ok {
			for i, item := range envList {
				if s, ok := item.(string); ok && strings.HasPrefix(s, "CADDY_INGRESS_NETWORKS=") {
					envList[i] = "CADDY_INGRESS_NETWORKS=" + netName
				}
			}
			caddySvc["environment"] = envList
		}
	}
}

func toCaddyGlobalOptions(g proxy.GlobalOptions) icaddy.GlobalOptions {
	return icaddy.GlobalOptions{
		Profile:          g.Profile,
		ReadHeader:       g.ReadHeader,
		ReadBodyIdle:     g.ReadBodyIdle,
		Idle:             g.Idle,
		GracePeriod:      g.GracePeriod,
		MaxHeaderSize:    g.MaxHeaderSize,
		RealIP:           g.RealIP,
		TrustedProxies:   append([]string(nil), g.TrustedProxies...),
		TrustedStrict:    g.TrustedStrict,
		ShieldProxies:    append([]string(nil), g.ShieldProxies...),
		DirectCloudflare: g.DirectCloudflare,
	}
}

func toCaddySharedMounts(mounts []proxy.SharedMount) []icaddy.NDSharedMount {
	if len(mounts) == 0 {
		return nil
	}
	out := make([]icaddy.NDSharedMount, 0, len(mounts))
	for _, m := range mounts {
		out = append(out, icaddy.NDSharedMount{
			VolumeName: m.VolumeName,
			Target:     m.Target,
		})
	}
	return out
}

func toDBAllocatedPorts(ports []proxy.GatewayPort) []db.AllocatedPort {
	if len(ports) == 0 {
		return nil
	}
	out := make([]db.AllocatedPort, 0, len(ports))
	for _, p := range ports {
		out = append(out, db.AllocatedPort{
			Port:     p.Port,
			BindIP:   p.BindIP,
			AppID:    p.AppID,
			Protocol: p.Protocol,
			Direct:   p.Direct,
		})
	}
	return out
}
