package migrate

import (
	"encoding/json"
	"os/exec"
	"strings"
)

func volumeMountpoint(volumeName string) (string, error) {
	cmd := exec.Command("docker", "volume", "inspect", volumeName, "--format", "{{json .Mountpoint}}")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	var mount string
	if err := json.Unmarshal(out, &mount); err != nil {
		return strings.TrimSpace(string(out)), nil
	}
	return strings.TrimSpace(mount), nil
}
