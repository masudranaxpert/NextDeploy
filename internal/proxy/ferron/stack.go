package ferron

import (
	"fmt"
	"sort"
	"strings"

	"panel/internal/proxy"
	"panel/internal/runutil"
)

// StackService builds the Docker Compose service definition and top-level named
// volumes needed to run Ferron 3 as the NextDeploy root proxy container.
func (d *Driver) StackService(opts proxy.StackOptions) (map[string]any, map[string]any, error) {
	containerName := strings.TrimSpace(opts.ContainerName)
	if containerName == "" {
		containerName = d.ContainerName()
	}
	image := strings.TrimSpace(opts.Image)
	if image == "" {
		image = d.image
	}
	netKey := strings.TrimSpace(opts.NetworkKey)
	if netKey == "" {
		netKey = DefaultNetworkKey
	}
	dataDir := strings.TrimSpace(opts.DataDir)
	if dataDir == "" {
		dataDir = d.dataDir
	}
	pkiDir := strings.TrimSpace(opts.PKIDir)
	if pkiDir == "" {
		pkiDir = d.pkiSitesDir
	}
	workspacesDir := strings.TrimSpace(opts.WorkspacesDir)
	if workspacesDir == "" {
		workspacesDir = DefaultWorkspacesDir
	}
	tokenEnv := strings.TrimSpace(opts.AdminTokenEnv)
	if tokenEnv == "" {
		tokenEnv = d.adminTokenEnv
	}
	if !isSafeEnvName(tokenEnv) {
		return nil, nil, fmt.Errorf("ferron: invalid admin token env name %q", tokenEnv)
	}

	ports, err := buildStackPorts(opts.Gateways)
	if err != nil {
		return nil, nil, err
	}

	mounts := []string{
		fmt.Sprintf("%s:%s:ro", dataDir, containerConfigRoot),
		fmt.Sprintf("%s:%s:ro", pkiDir, containerPKISitesDir),
		fmt.Sprintf("%s:%s:ro", d.pkiImportedDir, containerPKIImportedDir),
		"ferron_acme:" + d.acmeCacheDir,
		fmt.Sprintf("%s:%s:ro", workspacesDir, DefaultWorkspacesDir),
	}
	sockDir := strings.TrimSpace(opts.PHPSocketsDir)
	if sockDir == "" {
		sockDir = workspacesDir + "/.nd-php"
	}
	mounts = append(mounts, fmt.Sprintf("%s:%s", sockDir, DefaultWorkspacesDir+"/.nd-php"))

	topVolumes := map[string]any{
		"ferron_acme": map[string]any{},
	}
	for _, sm := range sortedSharedMounts(opts.SharedMounts) {
		volName := strings.TrimSpace(sm.VolumeName)
		target := strings.TrimSpace(sm.Target)
		if volName == "" || target == "" {
			return nil, nil, fmt.Errorf("ferron: shared mount requires volume name and target path")
		}
		entry := fmt.Sprintf("%s:%s", volName, target)
		if sm.ReadOnly {
			entry += ":ro"
		}
		mounts = append(mounts, entry)
		topVolumes[volName] = map[string]any{"external": true}
	}

	tokenVal := d.adminToken
	if tokenVal == "" {
		tokenVal = fmt.Sprintf("${%s:?%s must be set to a non-empty secret}", tokenEnv, tokenEnv)
	}

	service := map[string]any{
		"image":          image,
		"container_name": containerName,
		"user":           "0:0",
		"restart":        "unless-stopped",
		"logging":        runutil.ComposeLogging(),
		"command": []string{
			"/usr/local/bin/ferron",
			"run",
			"-c",
			containerLiveConfig,
			"--config-adapter",
			"ferronconf",
		},
		"networks": []string{netKey},
		"ports":    ports,
		"environment": map[string]any{
			tokenEnv: tokenVal,
		},
		"volumes": mounts,
	}

	return service, topVolumes, nil
}

func buildStackPorts(gateways []proxy.GatewayPort) ([]string, error) {
	ports := []string{"80:80", "443:443/tcp", "443:443/udp"}
	seen := map[string]bool{
		"80/tcp":  true,
		"443/tcp": true,
		"443/udp": true,
	}

	var extras []string
	for _, gw := range gateways {
		if gw.Direct || strings.EqualFold(strings.TrimSpace(gw.Protocol), "udp") {
			continue
		}
		if gw.Port <= 0 || gw.Port > 65535 {
			return nil, fmt.Errorf("ferron: invalid gateway port %d", gw.Port)
		}
		bindIP := strings.TrimSpace(gw.BindIP)
		key := fmt.Sprintf("%s:%d/tcp", bindIP, gw.Port)
		if bindIP == "" || bindIP == "0.0.0.0" {
			key = fmt.Sprintf("%d/tcp", gw.Port)
		}
		if seen[key] {
			continue
		}
		seen[key] = true

		entry := fmt.Sprintf("%d:%d", gw.Port, gw.Port)
		if bindIP != "" && bindIP != "0.0.0.0" {
			hostPart := bindIP
			if strings.Contains(bindIP, ":") && !strings.HasPrefix(bindIP, "[") {
				hostPart = "[" + bindIP + "]"
			}
			entry = fmt.Sprintf("%s:%d:%d", hostPart, gw.Port, gw.Port)
		}
		extras = append(extras, entry)
	}
	sort.Strings(extras)
	return append(ports, extras...), nil
}

func sortedSharedMounts(in []proxy.SharedMount) []proxy.SharedMount {
	if len(in) == 0 {
		return nil
	}
	out := make([]proxy.SharedMount, len(in))
	copy(out, in)
	sort.Slice(out, func(i, j int) bool {
		if out[i].VolumeName != out[j].VolumeName {
			return out[i].VolumeName < out[j].VolumeName
		}
		return out[i].Target < out[j].Target
	})
	return out
}

func isSafeEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			continue
		}
		return false
	}
	return true
}
