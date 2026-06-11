package backup

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/volumex"
)

const (
	generatedComposeName  = ".nextdeploy.generated.compose.yml"
	fullBackupManifestFn  = "MANIFEST.json"
	fullBackupAppArchive  = "app.tar.gz"
	fullBackupVolumesDir  = "volumes"
	fullBackupVersion     = 1
	fullBackupType        = "full"
	streamCopyBufferBytes = 64 * 1024
)

type FullBackupManifest struct {
	Version    int                      `json:"version"`
	Type       string                   `json:"type"`
	AppName    string                   `json:"app_name"`
	Timestamp  string                   `json:"timestamp"`
	AppArchive string                   `json:"app_archive,omitempty"`
	Volumes    []FullBackupVolumeEntry  `json:"volumes,omitempty"`
}

type FullBackupVolumeEntry struct {
	Name    string `json:"name"`
	Archive string `json:"archive"`
}

type innerArchive struct {
	name       string
	localPath  string
	memberPath string
}

func backupStagingDir() string {
	if d := strings.TrimSpace(os.Getenv("DATA_DIR")); d != "" {
		return filepath.Join(d, "backup-staging")
	}
	return os.TempDir()
}

func copyFileContents(srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	buf := make([]byte, streamCopyBufferBytes)
	if _, err := io.CopyBuffer(dst, src, buf); err != nil {
		_ = dst.Close()
		return err
	}
	return dst.Close()
}

func streamingCopy(dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, streamCopyBufferBytes)
	return io.CopyBuffer(dst, src, buf)
}

func BackupVolume(ctx context.Context, volumeName string) (string, error) {
	return BackupVolumeWithOptions(ctx, volumeName, false, nil)
}

// BackupVolumeWithOptions archives a Docker volume. With pauseContainers,
// containers mounting the volume are frozen during the tar so the copy is a
// crash-consistent point-in-time snapshot (safe for database restores).
func BackupVolumeWithOptions(ctx context.Context, volumeName string, pauseContainers bool, progress FullBackupProgress) (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	backupName := fmt.Sprintf("%s-%s.tar.gz", volumeName, timestamp)
	staging := backupStagingDir()
	if err := os.MkdirAll(staging, 0700); err != nil {
		return "", fmt.Errorf("create backup staging dir: %w", err)
	}
	tmpPath := filepath.Join(staging, backupName)

	emit := func(msg string) {
		if progress != nil {
			progress(msg)
		}
	}

	if err := archiveVolumeMaybePaused(ctx, volumeName, tmpPath, pauseContainers, emit); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	return tmpPath, nil
}

// PauseContainersUsingVolume freezes running containers that mount the volume
// and returns the IDs that were actually paused.
func PauseContainersUsingVolume(ctx context.Context, volumeName string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-q",
		"--filter", fmt.Sprintf("volume=%s", volumeName),
		"--filter", "status=running")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	var paused []string
	for _, line := range strings.Split(stdout.String(), "\n") {
		id := strings.TrimSpace(line)
		if id == "" {
			continue
		}
		var stderr bytes.Buffer
		pauseCmd := exec.CommandContext(ctx, "docker", "pause", id)
		pauseCmd.Stderr = &stderr
		if err := pauseCmd.Run(); err != nil {
			UnpauseContainers(context.Background(), paused)
			return nil, fmt.Errorf("pause %s: %s: %w", id, strings.TrimSpace(stderr.String()), err)
		}
		paused = append(paused, id)
	}
	return paused, nil
}

func UnpauseContainers(ctx context.Context, containerIDs []string) {
	for _, id := range containerIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		_ = exec.CommandContext(ctx, "docker", "unpause", id).Run()
	}
}

func archiveVolumeMaybePaused(ctx context.Context, volumeName, outPath string, pauseContainers bool, emit func(string)) error {
	if !pauseContainers {
		return writeDockerVolumeArchive(ctx, volumeName, outPath)
	}
	paused, err := PauseContainersUsingVolume(ctx, volumeName)
	if err != nil {
		return fmt.Errorf("pause containers: %w", err)
	}
	if len(paused) > 0 && emit != nil {
		emit(fmt.Sprintf("paused %d container(s) using %s for consistent snapshot", len(paused), volumeName))
	}
	archiveErr := writeDockerVolumeArchive(ctx, volumeName, outPath)
	if len(paused) > 0 {
		UnpauseContainers(context.Background(), paused)
		if emit != nil {
			emit(fmt.Sprintf("unpaused %d container(s)", len(paused)))
		}
	}
	return archiveErr
}

func writeDockerVolumeArchive(ctx context.Context, volumeName, outPath string) error {
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	bw := bufio.NewWriterSize(f, streamCopyBufferBytes)

	cmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"-v", fmt.Sprintf("%s:/vol:ro", volumeName),
		"alpine:3.20", "tar", "czf", "-", "-C", "/vol", ".")
	cmd.Stdout = bw

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		_ = bw.Flush()
		_ = f.Close()
		return fmt.Errorf("backup failed: %s: %w", strings.TrimSpace(stderr.String()), err)
	}
	if err := bw.Flush(); err != nil {
		_ = f.Close()
		return fmt.Errorf("flush archive: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close archive: %w", err)
	}
	return nil
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

	bw := bufio.NewWriterSize(f, streamCopyBufferBytes)
	defer bw.Flush()

	gz := gzip.NewWriter(bw)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	buf := make([]byte, streamCopyBufferBytes)

	return filepath.WalkDir(sourceDir, func(p string, d fs.DirEntry, walkErr error) error {
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

		rel, err := filepath.Rel(sourceDir, p)
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
			linkTarget, err = os.Readlink(p)
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

		src, err := os.Open(p)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		_, err = io.CopyBuffer(tw, src, buf)
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
	backupName := fmt.Sprintf("%s-app-%s.tar.gz", appName, timestamp)

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
		if err := copyFileContents(tmpPath, finalPath); err != nil {
			return "", fmt.Errorf("copy backup: %w", err)
		}
	}

	return finalPath, nil
}

type FullBackupProgress func(msg string)

// BackupFullWithVolumes writes a wrapper .tar.gz containing app.tar.gz, one
// <name>.tar.gz per volume, and MANIFEST.json.
func BackupFullWithVolumes(ctx context.Context, appName, sourceDir string, volumeNames []string, progress FullBackupProgress) (string, error) {
	return BackupFullWithVolumesOptions(ctx, appName, sourceDir, volumeNames, false, progress)
}

func BackupFullWithVolumesOptions(ctx context.Context, appName, sourceDir string, volumeNames []string, pauseContainers bool, progress FullBackupProgress) (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	backupName := fmt.Sprintf("%s-full-%s.tar.gz", appName, timestamp)

	staging := backupStagingDir()
	if err := os.MkdirAll(staging, 0700); err != nil {
		return "", fmt.Errorf("create backup staging dir: %w", err)
	}
	workDir := filepath.Join(staging, fmt.Sprintf("full-%s-%d", appName, time.Now().UnixNano()))
	if err := os.MkdirAll(workDir, 0700); err != nil {
		return "", fmt.Errorf("create backup work dir: %w", err)
	}
	defer os.RemoveAll(workDir)

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

	emit := func(msg string) {
		if progress != nil {
			progress(msg)
		}
	}

	appArchivePath := filepath.Join(workDir, fullBackupAppArchive)
	emit(fmt.Sprintf("packing app workspace → %s", fullBackupAppArchive))
	if err := writeFullAppArchive(ctx, sourceDir, appArchivePath); err != nil {
		return "", fmt.Errorf("pack app workspace: %w", err)
	}
	if st, err := os.Stat(appArchivePath); err != nil || st.Size() == 0 {
		if err != nil {
			return "", fmt.Errorf("pack app workspace: %w", err)
		}
		return "", fmt.Errorf("pack app workspace: empty archive")
	}

	cleanVolumes := make([]string, 0, len(volumeNames))
	seen := map[string]struct{}{}
	for _, v := range volumeNames {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		cleanVolumes = append(cleanVolumes, v)
	}

	manifest := FullBackupManifest{
		Version:    fullBackupVersion,
		Type:       fullBackupType,
		AppName:    appName,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		AppArchive: fullBackupAppArchive,
	}

	var volArchives []innerArchive

	for _, vol := range cleanVolumes {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		if !volumex.ValidVolumeName(vol) {
			return "", fmt.Errorf("invalid Docker volume name %q", vol)
		}
		if ok, msg := volumex.VolumeExists(ctx, vol); !ok {
			if strings.TrimSpace(msg) == "" {
				msg = "Docker volume not found"
			}
			return "", fmt.Errorf("volume %s: %s", vol, msg)
		}

		memberPath := path.Join(fullBackupVolumesDir, vol+".tar.gz")
		localPath := filepath.Join(workDir, vol+".tar.gz")
		emit(fmt.Sprintf("packing volume %s → %s", vol, memberPath))
		if err := archiveVolumeMaybePaused(ctx, vol, localPath, pauseContainers, emit); err != nil {
			return "", fmt.Errorf("pack volume %s: %w", vol, err)
		}
		if st, err := os.Stat(localPath); err != nil || st.Size() == 0 {
			if err != nil {
				return "", fmt.Errorf("pack volume %s: %w", vol, err)
			}
			return "", fmt.Errorf("pack volume %s: empty archive", vol)
		}
		volArchives = append(volArchives, innerArchive{name: vol, localPath: localPath, memberPath: memberPath})
		manifest.Volumes = append(manifest.Volumes, FullBackupVolumeEntry{Name: vol, Archive: memberPath})
	}

	wrapperTmp := filepath.Join(workDir, backupName)
	emit("writing wrapper archive")
	if err := writeFullWrapperArchive(ctx, wrapperTmp, appArchivePath, volArchives, manifest); err != nil {
		return "", fmt.Errorf("wrap archives: %w", err)
	}

	finalPath := filepath.Join(staging, backupName)
	if err := os.Rename(wrapperTmp, finalPath); err != nil {
		if err := copyFileContents(wrapperTmp, finalPath); err != nil {
			return "", fmt.Errorf("copy backup: %w", err)
		}
	}

	emit("verifying wrapper archive")
	if st, err := os.Stat(finalPath); err != nil || st.Size() == 0 {
		if err == nil {
			err = fmt.Errorf("empty wrapper archive")
		}
		_ = os.Remove(finalPath)
		return "", fmt.Errorf("verify wrapper archive: %w", err)
	}
	if _, err := ReadFullBackupManifest(finalPath); err != nil {
		_ = os.Remove(finalPath)
		return "", fmt.Errorf("verify wrapper archive: %w", err)
	}

	emit("wrapper archive ready")
	return finalPath, nil
}

func writeFullWrapperArchive(ctx context.Context, outPath, appArchivePath string, volArchives []innerArchive, manifest FullBackupManifest) error {
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()

	bw := bufio.NewWriterSize(out, streamCopyBufferBytes)

	gz, err := gzip.NewWriterLevel(bw, gzip.NoCompression)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(gz)

	now := time.Now()

	addInner := func(localPath, memberName string) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		fi, err := os.Stat(localPath)
		if err != nil {
			return err
		}
		hdr := &tar.Header{
			Name:    memberName,
			Mode:    0600,
			Size:    fi.Size(),
			ModTime: now,
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(localPath)
		if err != nil {
			return err
		}
		if _, err := streamingCopy(tw, f); err != nil {
			_ = f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		_ = os.Remove(localPath)
		return nil
	}

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	hdr := &tar.Header{
		Name:     fullBackupManifestFn,
		Mode:     0600,
		Size:     int64(len(manifestBytes)),
		ModTime:  now,
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := tw.Write(manifestBytes); err != nil {
		return err
	}

	if err := addInner(appArchivePath, fullBackupAppArchive); err != nil {
		return err
	}
	for _, v := range volArchives {
		if err := addInner(v.localPath, v.memberPath); err != nil {
			return err
		}
	}

	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	return out.Close()
}

// ReadFullBackupManifest extracts only the MANIFEST.json entry without
// unpacking the full archive.
func ReadFullBackupManifest(archivePath string) (*FullBackupManifest, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("manifest missing from archive")
		}
		if err != nil {
			return nil, err
		}
		if filepath.ToSlash(hdr.Name) != fullBackupManifestFn {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(tr, 1<<20))
		if err != nil {
			return nil, err
		}
		var m FullBackupManifest
		if err := json.Unmarshal(body, &m); err != nil {
			return nil, fmt.Errorf("parse manifest: %w", err)
		}
		return &m, nil
	}
}

func extractFullBackupMembers(ctx context.Context, archivePath, workDir string, members map[string]string) (map[string]string, error) {
	if err := volumex.ValidateTarGzPaths(archivePath); err != nil {
		return nil, fmt.Errorf("archive validation failed: %w", err)
	}
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	out := make(map[string]string, len(members))
	buf := make([]byte, streamCopyBufferBytes)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := filepath.ToSlash(hdr.Name)
		dstRel, ok := members[name]
		if !ok {
			continue
		}
		dstPath := filepath.Join(workDir, dstRel)
		if err := os.MkdirAll(filepath.Dir(dstPath), 0700); err != nil {
			return nil, err
		}
		w, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return nil, err
		}
		if _, err := io.CopyBuffer(w, tr, buf); err != nil {
			_ = w.Close()
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
		out[name] = dstPath
	}
	return out, nil
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
	if !filepath.IsAbs(backupPath) {
		if wd, err := os.Getwd(); err == nil {
			backupPath = filepath.Join(wd, backupPath)
		}
	}
	if err := volumex.ValidateTarGzPaths(backupPath); err != nil {
		return fmt.Errorf("backup validation failed: %w", err)
	}

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

func RestoreFullWithVolumes(ctx context.Context, appName, composePath, restoreDir, backupPath string, progress FullBackupProgress) error {
	emit := func(msg string) {
		if progress != nil {
			progress(msg)
		}
	}

	manifest, err := ReadFullBackupManifest(backupPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	if manifest.Type != fullBackupType {
		return fmt.Errorf("unexpected manifest type %q", manifest.Type)
	}
	if strings.TrimSpace(manifest.AppArchive) == "" {
		manifest.AppArchive = fullBackupAppArchive
	}

	staging := backupStagingDir()
	if err := os.MkdirAll(staging, 0700); err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}
	workDir := filepath.Join(staging, fmt.Sprintf("restore-%s-%d", appName, time.Now().UnixNano()))
	if err := os.MkdirAll(workDir, 0700); err != nil {
		return fmt.Errorf("create restore work dir: %w", err)
	}
	defer os.RemoveAll(workDir)

	wanted := map[string]string{
		manifest.AppArchive: fullBackupAppArchive,
	}
	for _, v := range manifest.Volumes {
		if strings.TrimSpace(v.Archive) == "" || strings.TrimSpace(v.Name) == "" {
			continue
		}
		wanted[v.Archive] = filepath.FromSlash(v.Archive)
	}

	emit("extracting wrapper archive")
	paths, err := extractFullBackupMembers(ctx, backupPath, workDir, wanted)
	if err != nil {
		return err
	}

	appArchive, ok := paths[manifest.AppArchive]
	if !ok {
		return fmt.Errorf("app archive %q missing from backup", manifest.AppArchive)
	}

	emit("restoring app workspace")
	if err := RestoreFullApp(ctx, appName, composePath, restoreDir, appArchive); err != nil {
		return err
	}

	for _, v := range manifest.Volumes {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if strings.TrimSpace(v.Name) == "" || strings.TrimSpace(v.Archive) == "" {
			continue
		}
		inner, ok := paths[v.Archive]
		if !ok {
			return fmt.Errorf("volume archive %q missing from backup", v.Archive)
		}
		emit(fmt.Sprintf("restoring volume %s", v.Name))
		if err := RestoreVolume(ctx, v.Name, inner, true); err != nil {
			return fmt.Errorf("restore volume %s: %w", v.Name, err)
		}
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
