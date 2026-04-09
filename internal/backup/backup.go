package backup

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func BackupVolume(ctx context.Context, volumeName string) (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	backupName := fmt.Sprintf("%s-%s.tar.gz", volumeName, timestamp)
	tmpPath := filepath.Join(os.TempDir(), backupName)

	f, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer f.Close()

	cmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"-v", fmt.Sprintf("%s:/vol:ro", volumeName),
		"alpine:3.20", "tar", "czf", "-", "-C", "/vol", ".")
	cmd.Stdout = f

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("backup failed: %s: %w", stderr.String(), err)
	}

	return tmpPath, nil
}

func BackupFullApp(ctx context.Context, appName, composePath string) (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	backupName := fmt.Sprintf("%s-full-%s.tar.gz", appName, timestamp)
	tmpPath := filepath.Join(os.TempDir(), backupName)

	composeDir := filepath.Dir(composePath)

	cmd := exec.CommandContext(ctx, "tar", "czf", tmpPath, "-C", composeDir, ".")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("backup failed: %s: %w", stderr.String(), err)
	}

	return tmpPath, nil
}

func RestoreVolume(ctx context.Context, volumeName, backupPath string, force bool) error {
	if force {
		if _, err := StopContainersUsingVolume(ctx, volumeName); err != nil {
			return fmt.Errorf("stop containers: %w", err)
		}
	}

	f, err := os.Open(backupPath)
	if err != nil {
		return err
	}
	defer f.Close()

	cmd := exec.CommandContext(ctx, "docker", "run", "-i", "--rm",
		"-v", fmt.Sprintf("%s:/restore", volumeName),
		"alpine:3.20", "sh", "-c", "cd /restore && rm -rf * && tar xzf -")

	cmd.Stdin = f

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("restore failed: %s: %w", stderr.String(), err)
	}

	return nil
}

func RestoreFullApp(ctx context.Context, appName, composePath, backupPath string) error {
	composeDir := filepath.Dir(composePath)

	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", composePath, "down")
	_ = cmd.Run()

	cmd = exec.CommandContext(ctx, "tar", "xzf", backupPath, "-C", composeDir)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("extract failed: %s: %w", stderr.String(), err)
	}

	cmd = exec.CommandContext(ctx, "docker", "compose", "-f", composePath, "up", "-d", "--build")
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rebuild failed: %s: %w", stderr.String(), err)
	}

	return nil
}

func StopContainersUsingVolume(ctx context.Context, volumeName string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-q", "--filter", fmt.Sprintf("volume=%s", volumeName))
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return nil, err
	}

	var containerIDs []string
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			containerIDs = append(containerIDs, line)
		}
	}

	for _, id := range containerIDs {
		stopCmd := exec.CommandContext(ctx, "docker", "stop", id)
		if err := stopCmd.Run(); err != nil {
			return containerIDs, fmt.Errorf("failed to stop container %s: %w", id, err)
		}
	}

	return containerIDs, nil
}
