package migrate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/volumex"

	"golang.org/x/sync/errgroup"
)

const (
	appArchiveManifestName = "MANIFEST.json"
	appArchiveAppMember    = "app.tar.gz"
	appArchiveVolumesDir   = "volumes"
	appArchiveType         = "full"
	appArchiveVersion      = 1
)

type AppArchiveManifest struct {
	Version    int                   `json:"version"`
	Type       string                `json:"type"`
	AppName    string                `json:"app_name"`
	Timestamp  string                `json:"timestamp"`
	AppArchive string                `json:"app_archive,omitempty"`
	Volumes    []AppArchiveVolumeRef `json:"volumes,omitempty"`
}

type AppArchiveVolumeRef struct {
	Name    string `json:"name"`
	Archive string `json:"archive"`
}

func readAppArchiveManifest(archivePath string) (*AppArchiveManifest, error) {
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
		if filepath.ToSlash(hdr.Name) != appArchiveManifestName {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(tr, 1<<20))
		if err != nil {
			return nil, err
		}
		var m AppArchiveManifest
		if err := json.Unmarshal(body, &m); err != nil {
			return nil, fmt.Errorf("parse manifest: %w", err)
		}
		return &m, nil
	}
}

func exportAppArchive(ctx context.Context, appName, sourceDir string, volumeNames []string, destPath string, onProgress func(string)) error {
	emit := func(msg string) {
		if onProgress != nil {
			onProgress(msg)
		}
	}
	if !filepath.IsAbs(sourceDir) {
		if wd, err := os.Getwd(); err == nil {
			sourceDir = filepath.Join(wd, sourceDir)
		}
	}
	if st, err := os.Stat(sourceDir); err != nil {
		return fmt.Errorf("workspace not found (%s): %w", sourceDir, err)
	} else if !st.IsDir() {
		return fmt.Errorf("workspace path is not a directory: %s", sourceDir)
	}

	workDir := filepath.Join(StagingRoot(), fmt.Sprintf("export-app-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(filepath.Join(workDir, appArchiveVolumesDir), 0700); err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	appPath := filepath.Join(workDir, appArchiveAppMember)
	emit("packing workspace")
	if err := tarCreateWorkspace(ctx, sourceDir, appPath); err != nil {
		return err
	}

	cleanVolumes := dedupeVolumeNames(volumeNames)
	manifest := AppArchiveManifest{
		Version:    appArchiveVersion,
		Type:       appArchiveType,
		AppName:    appName,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		AppArchive: appArchiveAppMember,
	}

	if len(cleanVolumes) > 0 {
		sem := newSemaphore(ParallelWorkers())
		g, gctx := errgroup.WithContext(ctx)
		volResults := make([]AppArchiveVolumeRef, len(cleanVolumes))
		for i, vol := range cleanVolumes {
			i, vol := i, vol
			g.Go(func() error {
				if err := sem.acquire(gctx); err != nil {
					return err
				}
				defer sem.release()
				if !volumex.ValidVolumeName(vol) {
					return fmt.Errorf("invalid Docker volume name %q", vol)
				}
				if ok, msg := volumex.VolumeExists(gctx, vol); !ok {
					if strings.TrimSpace(msg) == "" {
						msg = "Docker volume not found"
					}
					return fmt.Errorf("volume %s: %s", vol, msg)
				}
				memberPath := path.Join(appArchiveVolumesDir, vol+".tar.gz")
				localPath := filepath.Join(workDir, appArchiveVolumesDir, vol+".tar.gz")
				emit("packing volume " + vol)
				if err := dockerVolumeArchive(gctx, vol, localPath); err != nil {
					return fmt.Errorf("pack volume %s: %w", vol, err)
				}
				volResults[i] = AppArchiveVolumeRef{Name: vol, Archive: memberPath}
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			return err
		}
		manifest.Volumes = volResults
	}

	mb, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(workDir, appArchiveManifestName), mb, 0600); err != nil {
		return err
	}

	members := []string{appArchiveManifestName, appArchiveAppMember}
	for _, v := range manifest.Volumes {
		members = append(members, filepath.ToSlash(v.Archive))
	}
	emit("writing app archive")
	tmp := destPath + ".part"
	if err := tarCreateGz(ctx, tmp, workDir, members); err != nil {
		return err
	}
	if err := os.Rename(tmp, destPath); err != nil {
		_ = os.Remove(destPath)
		if err2 := os.Rename(tmp, destPath); err2 != nil {
			return err2
		}
	}
	return nil
}

func dedupeVolumeNames(names []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(names))
	for _, v := range names {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func tarCreateWorkspace(ctx context.Context, sourceDir, outPath string) error {
	if !filepath.IsAbs(outPath) {
		if wd, err := os.Getwd(); err == nil {
			outPath = filepath.Join(wd, outPath)
		}
	}
	args := []string{
		"--exclude=.git", "--exclude=node_modules", "--exclude=vendor", "--exclude=tmp",
		"czf", outPath, "-C", sourceDir, ".",
	}
	return runTar(ctx, args, "pack workspace")
}

func dockerVolumeArchive(ctx context.Context, volumeName, outPath string) error {
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"-v", volumeName+":/vol:ro",
		"alpine:3.20", "tar", "czf", "-", "-C", "/vol", ".")
	cmd.Stdout = f
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(outPath)
		return fmt.Errorf("%s: %w", strings.TrimSpace(stderr.String()), err)
	}
	return f.Close()
}
