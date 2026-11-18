package angie

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"panel/internal/proxy"
	"panel/internal/proxy/pki"
)

const (
	angieACMEVolumeName = "angie_acme"
	ndSharedVolPrefix   = "nd_shared_"
)

// StackService builds the Docker Compose service definition and top-level
// named volumes required to run Angie OSS in the NextDeploy root stack.
func (d *Driver) StackService(opts proxy.StackOptions) (map[string]any, map[string]any, error) {
	image := strings.TrimSpace(opts.Image)
	if image == "" {
		if d != nil && strings.TrimSpace(d.image) != "" {
			image = d.image
		} else {
			image = AngieImage
		}
	}
	containerName := strings.TrimSpace(opts.ContainerName)
	if containerName == "" {
		containerName = d.ContainerName()
	}

	netKey := strings.TrimSpace(opts.NetworkKey)
	if netKey == "" {
		netKey = DefaultNetworkKey
	}

	dataDir := strings.TrimSpace(opts.DataDir)
	if dataDir == "" {
		if d != nil && strings.TrimSpace(d.dataDir) != "" {
			dataDir = d.dataDir
		} else {
			dataDir = DefaultDataDir
		}
	}
	dataDir = strings.TrimRight(filepath.ToSlash(dataDir), "/")

	pkiSitesDir, pkiImportedDir, err := resolvePKIMountDirs(opts.PKIDir, d)
	if err != nil {
		return nil, nil, err
	}

	workspacesDir := strings.TrimSpace(opts.WorkspacesDir)
	if workspacesDir == "" {
		workspacesDir = DefaultWorkspacesDir
	}
	workspacesDir = strings.TrimRight(filepath.ToSlash(workspacesDir), "/")

	phpSocketsMount := resolvePHPSocketsMount(opts.PHPSocketsDir, workspacesDir)

	ports := buildServicePorts(opts.Gateways)
	mounts := []any{
		"/var/run/docker.sock:/var/run/docker.sock",
		dataDir + ":" + containerConfigRoot + ":ro",
		pkiSitesDir + ":/etc/nd-pki/sites:ro",
		pkiImportedDir + ":/etc/nd-pki/imported:ro",
		angieACMEVolumeName + ":/var/lib/angie/acme",
		workspacesDir + ":/data/workspaces:ro",
		phpSocketsMount,
	}

	volumes := map[string]any{
		angieACMEVolumeName: map[string]any{},
	}

	seenShared := make(map[string]struct{}, len(opts.SharedMounts))
	for _, sm := range opts.SharedMounts {
		vol := strings.TrimSpace(sm.VolumeName)
		tgt := strings.TrimSpace(sm.Target)
		if vol == "" || !proxy.ValidSharedTarget(tgt) {
			continue
		}
		volKey := ndSharedComposeKey(vol)
		if _, dup := seenShared[volKey]; dup {
			continue
		}
		seenShared[volKey] = struct{}{}
		volumes[volKey] = map[string]any{
			"external": true,
			"name":     vol,
		}
		mounts = append(mounts, volKey+":"+tgt+":ro")
	}

	svc := map[string]any{
		"container_name": containerName,
		"image":          image,
		"restart":        "unless-stopped",
		"networks":       []any{netKey},
		"ports":          ports,
		"ulimits": map[string]any{
			"nofile": map[string]any{
				"soft": 1048576,
				"hard": 1048576,
			},
		},
		"volumes": mounts,
		"command": []any{
			"sh", "-c",
			"while [ ! -f " + containerLiveConfig + " ]; do sleep 0.2; done; exec angie -c " + containerLiveConfig + " -g 'daemon off;'",
		},
	}

	return svc, volumes, nil
}

func resolvePKIMountDirs(rawPKIDir string, d *Driver) (sitesDir, importedDir string, err error) {
	trimmed := strings.TrimRight(filepath.ToSlash(strings.TrimSpace(rawPKIDir)), "/")
	if trimmed == "" {
		sitesDir = pki.DefaultSitesDir
		importedDir = DefaultPKIImportedDir
		if d != nil {
			if strings.TrimSpace(d.pkiSitesDir) != "" {
				sitesDir = strings.TrimRight(filepath.ToSlash(d.pkiSitesDir), "/")
			}
			if strings.TrimSpace(d.pkiImportedDir) != "" {
				importedDir = strings.TrimRight(filepath.ToSlash(d.pkiImportedDir), "/")
			}
		}
		return sitesDir, importedDir, nil
	}
	if trimmed == "/ca" || strings.HasSuffix(trimmed, "/ca") {
		return "", "", fmt.Errorf("angie stack: pki_dir %q must not point to the private CA directory", rawPKIDir)
	}
	if strings.HasSuffix(trimmed, "/sites") {
		base := path.Dir(trimmed)
		return trimmed, path.Join(base, "imported"), nil
	}
	return path.Join(trimmed, "sites"), path.Join(trimmed, "imported"), nil
}

func resolvePHPSocketsMount(rawSocketsDir, workspacesDir string) string {
	s := strings.TrimRight(filepath.ToSlash(strings.TrimSpace(rawSocketsDir)), "/")
	defaultWS := workspacesDir + "/.nd-php"
	if s == "" || s == defaultWS {
		return defaultWS + ":/data/workspaces/.nd-php"
	}
	return s + ":" + s
}

func buildServicePorts(gateways []proxy.GatewayPort) []any {
	ports := []any{
		"80:80",
		"443:443/tcp",
		"443:443/udp",
	}
	seen := map[string]bool{
		"80:80":       true,
		"443:443":     true,
		"443:443/tcp": true,
		"443:443/udp": true,
	}
	for _, gw := range gateways {
		if gw.Direct || gw.Port <= 0 {
			continue
		}
		bindIP := strings.TrimSpace(gw.BindIP)
		if (bindIP == "" || bindIP == "0.0.0.0") && (gw.Port == 80 || gw.Port == 443) {
			continue
		}
		mapping := fmt.Sprintf("%d:%d", gw.Port, gw.Port)
		if bindIP != "" && bindIP != "0.0.0.0" {
			mapping = fmt.Sprintf("%s:%d:%d", bindIP, gw.Port, gw.Port)
		}
		if strings.EqualFold(strings.TrimSpace(gw.Protocol), "udp") {
			mapping += "/udp"
		}
		if !seen[mapping] {
			seen[mapping] = true
			ports = append(ports, mapping)
		}
	}
	return ports
}

func ndSharedComposeKey(volName string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(volName)))
	return ndSharedVolPrefix + hex.EncodeToString(h[:8])
}
