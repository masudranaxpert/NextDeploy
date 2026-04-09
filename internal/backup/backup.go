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

	"panel/internal/rclone"
)

// VolumeBackup creates a backup of Docker volumes
func VolumeBackup(ctx context.Context, volumeNames []string, appID string) (string, int64, error) {
	if len(volumeNames) == 0 {
		return "", 0, fmt.Errorf("no volumes specified")
	}
	
	timestamp := time.Now().Format("20060102-150405")
	backupName := fmt.Sprintf("%s-volumes-%s.tar.gz", appID, timestamp)
	tmpPath := filepath.Join(os.TempDir(), backupName)
	
	f, err := os.Create(tmpPath)
	if err != nil {
		return "", 0, fmt.Errorf("create temp file: %w", err)
	}
	defer f.Close()
	
	volumeMounts := make([]string, 0, len(volumeNames)*2)
	tarPaths := make([]string, 0, len(volumeNames))
	
	for i, vol := range volumeNames {
		mountPath := fmt.Sprintf("/vol%d", i)
		volumeMounts = append(volumeMounts, "-v", fmt.Sprintf("%s:%s:ro", vol, mountPath))
		tarPaths = append(tarPaths, mountPath)
	}
	
	args := []string{"run", "--rm"}
	args = append(args, volumeMounts...)
	args = append(args, "alpine:3.20", "tar", "czf", "-", "-C", "/")
	args = append(args, tarPaths...)
	
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = f
	
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	
	if err := cmd.Run(); err != nil {
		os.Remove(tmpPath)
		return "", 0, fmt.Errorf("backup failed: %s: %w", stderr.String(), err)
	}
	
	stat, err := os.Stat(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return "", 0, err
	}
	
	return tmpPath, stat.Size(), nil
}

// FullAppBackup creates a complete app backup (volumes + compose + env)
func FullAppBackup(ctx context.Context, appID string, volumeNames []string, composeContent, envContent string) (string, int64, error) {
	timestamp := time.Now().Format("20060102-150405")
	backupName := fmt.Sprintf("%s-full-%s.tar.gz", appID, timestamp)
	tmpDir := filepath.Join(os.TempDir(), fmt.Sprintf("backup-%s", timestamp))
	tmpPath := filepath.Join(os.TempDir(), backupName)
	
	if err := os.MkdirAll(tmpDir, 0700); err != nil {
		return "", 0, err
	}
	defer os.RemoveAll(tmpDir)
	
	if err := os.WriteFile(filepath.Join(tmpDir, "docker-compose.yml"), []byte(composeContent), 0600); err != nil {
		return "", 0, err
	}
	
	if err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte(envContent), 0600); err != nil {
		return "", 0, err
	}
	
	if len(volumeNames) > 0 {
		volumesDir := filepath.Join(tmpDir, "volumes")
		if err := os.MkdirAll(volumesDir, 0700); err != nil {
			return "", 0, err
		}
		
		for _, vol := range volumeNames {
			volBackup := filepath.Join(volumesDir, vol+".tar.gz")
			f, err := os.Create(volBackup)
			if err != nil {
				return "", 0, err
			}
			
			cmd := exec.CommandContext(ctx, "docker", "run", "--rm",
				"-v", fmt.Sprintf("%s:/vol:ro", vol),
				"alpine:3.20", "tar", "czf", "-", "-C", "/vol", ".")
			cmd.Stdout = f
			
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			
			if err := cmd.Run(); err != nil {
				f.Close()
				return "", 0, fmt.Errorf("backup volume %s failed: %s: %w", vol, stderr.String(), err)
			}
			f.Close()
		}
	}
	
	cmd := exec.CommandContext(ctx, "tar", "czf", tmpPath, "-C", tmpDir, ".")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	
	if err := cmd.Run(); err != nil {
		os.Remove(tmpPath)
		return "", 0, fmt.Errorf("create archive failed: %s: %w", stderr.String(), err)
	}
	
	stat, err := os.Stat(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return "", 0, err
	}
	
	return tmpPath, stat.Size(), nil
}

// VolumeRestore restores volumes from backup
func VolumeRestore(ctx context.Context, backupPath string, volumeNames []string) error {
	if len(volumeNames) == 0 {
		return fmt.Errorf("no volumes specified")
	}
	
	for i, vol := range volumeNames {
		mountPath := fmt.Sprintf("/vol%d", i)
		
		f, err := os.Open(backupPath)
		if err != nil {
			return err
		}
		defer f.Close()
		
		cmd := exec.CommandContext(ctx, "docker", "run", "-i", "--rm",
			"-v", fmt.Sprintf("%s:/restore", vol),
			"alpine:3.20", "sh", "-c",
			fmt.Sprintf("cd /restore && tar xzf - --strip-components=1 %s", mountPath))
		
		cmd.Stdin = f
		
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("restore volume %s failed: %s: %w", vol, stderr.String(), err)
		}
	}
	
	return nil
}

// FullAppRestore restores complete app from backup
func FullAppRestore(ctx context.Context, backupPath, extractDir string) (composeContent, envContent string, volumeBackups map[string]string, err error) {
	if err := os.MkdirAll(extractDir, 0700); err != nil {
		return "", "", nil, err
	}
	
	cmd := exec.CommandContext(ctx, "tar", "xzf", backupPath, "-C", extractDir)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	
	if err := cmd.Run(); err != nil {
		return "", "", nil, fmt.Errorf("extract failed: %s: %w", stderr.String(), err)
	}
	
	composeBytes, err := os.ReadFile(filepath.Join(extractDir, "docker-compose.yml"))
	if err != nil {
		return "", "", nil, err
	}
	composeContent = string(composeBytes)
	
	envBytes, err := os.ReadFile(filepath.Join(extractDir, ".env"))
	if err != nil && !os.IsNotExist(err) {
		return "", "", nil, err
	}
	envContent = string(envBytes)
	
	volumeBackups = make(map[string]string)
	volumesDir := filepath.Join(extractDir, "volumes")
	if entries, err := os.ReadDir(volumesDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".tar.gz") {
				volName := strings.TrimSuffix(entry.Name(), ".tar.gz")
				volumeBackups[volName] = filepath.Join(volumesDir, entry.Name())
			}
		}
	}
	
	return composeContent, envContent, volumeBackups, nil
}

// UploadToRemote uploads backup to remote storage
func UploadToRemote(ctx context.Context, remoteName, localPath, remotePath string) error {
	return rclone.Upload(ctx, remoteName, localPath, remotePath)
}

// DownloadFromRemote downloads backup from remote storage
func DownloadFromRemote(ctx context.Context, remoteName, remotePath, localPath string) error {
	return rclone.Download(ctx, remoteName, remotePath, localPath)
}

// ListRemoteBackups lists backups in remote storage
func ListRemoteBackups(ctx context.Context, remoteName, remotePath string) ([]string, error) {
	return rclone.List(ctx, remoteName, remotePath)
}

// DeleteRemoteBackup deletes a backup from remote storage
func DeleteRemoteBackup(ctx context.Context, remoteName, remotePath string) error {
	return rclone.Delete(ctx, remoteName, remotePath)
}

// StopContainersUsingVolume stops all containers using the specified volume
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

// StartContainers starts containers by IDs
func StartContainers(ctx context.Context, containerIDs []string) error {
	for _, id := range containerIDs {
		cmd := exec.CommandContext(ctx, "docker", "start", id)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to start container %s: %w", id, err)
		}
	}
	return nil
}
