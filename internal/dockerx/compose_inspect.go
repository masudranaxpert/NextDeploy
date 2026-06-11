package dockerx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"

	"panel/internal/dockerapi"
)

// ContainerComposeLabels reads com.docker.compose.project and com.docker.compose.project.working_dir
// from docker inspect (container name or id). Used when container_name overrides hide the project prefix.
func ContainerComposeLabels(ctx context.Context, container string) (project, workingDir string, err error) {
	container = strings.TrimSpace(container)
	if container == "" {
		return "", "", errors.New("empty container")
	}

	proj, workDir, sdkErr := dockerapi.ContainerComposeLabels(ctx, container)
	if sdkErr == nil {
		return proj, workDir, nil
	}

	cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{json .Config.Labels}}", container)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", "", errors.New(msg)
	}
	var labels map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &labels); err != nil {
		return "", "", err
	}
	if labels == nil {
		return "", "", nil
	}
	return labels["com.docker.compose.project"], labels["com.docker.compose.project.working_dir"], nil
}
