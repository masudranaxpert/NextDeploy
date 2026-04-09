package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/volumex"
)

const generatedComposeName = ".nextdeploy.generated.compose.yml"

func backupStagingDir() string {
	if d := strings.TrimSpace(os.Getenv("DATA_DIR")); d != "" {
		return filepath.Join(d, "backup-staging")
	}
	return os.TempDir()
}

func BackupVolume(ctx context.Context, volumeName string) (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	backupName := fmt.Sprintf("%s-%s.tar.gz", volumeName, timestamp)
	staging := backupStagingDir()
	if err := os.MkdirAll(staging, 0700); err != nil {
		return "", fmt.Errorf("create backup staging dir: %w", err)
	}
	tmpPath := filepath.Join(staging, backupName)

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

func shouldSkipFullAppEntry(rel string, d fs.DirEntry) bool {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == "" || rel == "." {
		return false
	}
	parts := strings.Split(rel, "/")
	for _, p := range parts {
		switch p {
		case ".git", "tmp", "node_modules", "vendor":
			return true
		}
	}
	base := parts[len(parts)-1]
	if strings.HasSuffix(base, ".sock") || strings.HasSuffix(base, ".pid") {
		return true
	}
	if d.Type()&os.ModeSocket != 0 {
		return true
	}
	return false
}

func writeFullAppArchive(ctx context.Context, sourceDir, outPath string) error {
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create archive: %w", err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	return filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, walkErr error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}

		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if shouldSkipFullAppEntry(rel, d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !(info.Mode().IsRegular() || info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return nil
		}

		linkTarget := ""
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err = os.Readlink(path)
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
		}
		hdr, err := tar.FileInfoHeader(info, linkTarget)
		if err != nil {
			return err
		}
		hdr.Name = rel
		if info.IsDir() && !strings.HasSuffix(hdr.Name, "/") {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}

		src, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		_, err = io.Copy(tw, src)
		closeErr := src.Close()
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		return nil
	})
}

func firstExistingComposePath(restoreDir string, preferred string) string {
	var candidates []string
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		for _, existing := range candidates {
			if filepath.Clean(existing) == filepath.Clean(p) {
				return
			}
		}
		candidates = append(candidates, p)
	}

	add(preferred)
	add(filepath.Join(restoreDir, generatedComposeName))
	add(filepath.Join(restoreDir, "docker-compose.yml"))
	add(filepath.Join(restoreDir, "docker-compose.yaml"))
	add(filepath.Join(restoreDir, "compose.yml"))
	add(filepath.Join(restoreDir, "compose.yaml"))

	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func BackupFullApp(ctx context.Context, appName, sourceDir string) (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	backupName := fmt.Sprintf("%s-full-%s.tar.gz", appName, timestamp)

	staging := backupStagingDir()
	if err := os.MkdirAll(staging, 0700); err != nil {
		return "", fmt.Errorf("create backup staging dir: %w", err)
	}
	backupTmpDir := filepath.Join(staging, fmt.Sprintf("work-%s-%d", appName, time.Now().UnixNano()))
	if err := os.MkdirAll(backupTmpDir, 0700); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}
	defer os.RemoveAll(backupTmpDir)

	tmpPath := filepath.Join(backupTmpDir, backupName)
	if !filepath.IsAbs(sourceDir) {
		if wd, err := os.Getwd(); err == nil {
			sourceDir = filepath.Join(wd, sourceDir)
		}
	}
	if st, err := os.Stat(sourceDir); err != nil {
		return "", fmt.Errorf("workspace not found (%s): %w", sourceDir, err)
	} else if !st.IsDir() {
		return "", fmt.Errorf("workspace path is not a directory: %s", sourceDir)
	}

	if err := writeFullAppArchive(ctx, sourceDir, tmpPath); err != nil {
		return "", fmt.Errorf("backup failed: %w", err)
	}
	if st, err := os.Stat(tmpPath); err != nil || st.Size() == 0 {
		if err != nil {
			return "", fmt.Errorf("backup failed: %w", err)
		}
		return "", fmt.Errorf("backup failed: empty archive")
	}

	finalPath := filepath.Join(staging, backupName)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		data, readErr := os.ReadFile(tmpPath)
		if readErr != nil {
			return "", fmt.Errorf("read backup: %w", readErr)
		}
		if err := os.WriteFile(finalPath, data, 0600); err != nil {
			return "", fmt.Errorf("write backup: %w", err)
		}
	}

	return finalPath, nil
}

func RestoreVolume(ctx context.Context, volumeName, backupPath string, force bool) error {
	if force {
		if _, err := StopContainersUsingVolume(ctx, volumeName); err != nil {
			return fmt.Errorf("stop containers: %w", err)
		}
	}

	if msg := volumex.ExtractTarGzForBackupRestore(ctx, volumeName, backupPath); msg != "" {
		return fmt.Errorf("restore failed: %s", msg)
	}
	return nil
}

func RestoreFullApp(ctx context.Context, appName, composePath, restoreDir, backupPath string) error {
	// Ensure composePath is absolute for reliable directory resolution.
	if !filepath.IsAbs(composePath) {
		if wd, err := os.Getwd(); err == nil {
			composePath = filepath.Join(wd, composePath)
		}
	}
	if !filepath.IsAbs(restoreDir) {
		if wd, err := os.Getwd(); err == nil {
			restoreDir = filepath.Join(wd, restoreDir)
		}
	}
	if strings.TrimSpace(restoreDir) == "" {
		restoreDir = filepath.Dir(composePath)
	}

	// Bring the stack down before extracting (best-effort). Prefer the configured
	// compose path, but fall back to the generated override when the base file is absent.
	if downComposePath := firstExistingComposePath(restoreDir, composePath); downComposePath != "" {
		downCmd := exec.CommandContext(ctx, "docker", "compose", "-f", downComposePath, "down")
		_ = downCmd.Run()
	}

	var stderr bytes.Buffer
	extractCmd := exec.CommandContext(ctx, "tar", "xzf", backupPath, "-C", restoreDir)
	extractCmd.Stderr = &stderr
	if err := extractCmd.Run(); err != nil {
		return fmt.Errorf("extract failed: %s: %w", strings.TrimSpace(stderr.String()), err)
	}

	runComposePath := firstExistingComposePath(restoreDir, composePath)
	if runComposePath == "" {
		return fmt.Errorf("rebuild failed: no compose file found in %s after restore", restoreDir)
	}

	stderr.Reset()
	upCmd := exec.CommandContext(ctx, "docker", "compose", "-f", runComposePath, "up", "-d", "--build")
	upCmd.Stderr = &stderr
	if err := upCmd.Run(); err != nil {
		return fmt.Errorf("rebuild failed: %s: %w", strings.TrimSpace(stderr.String()), err)
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
