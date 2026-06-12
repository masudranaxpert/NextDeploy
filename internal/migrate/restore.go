package migrate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/volumex"

	"golang.org/x/sync/errgroup"
)

func restoreAppArchive(ctx context.Context, wrapperPath, workspaceRoot string, onProgress func(string)) error {
	emit := func(msg string) {
		if onProgress != nil {
			onProgress(msg)
		}
	}

	manifest, err := readAppArchiveManifest(wrapperPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	if manifest.Type != appArchiveType {
		return fmt.Errorf("unexpected archive type %q", manifest.Type)
	}
	appMember := manifest.AppArchive
	if strings.TrimSpace(appMember) == "" {
		appMember = appArchiveAppMember
	}

	workDir := filepath.Join(StagingRoot(), fmt.Sprintf("restore-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(workDir, 0700); err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	members := []string{appMember}
	for _, v := range manifest.Volumes {
		if strings.TrimSpace(v.Archive) == "" || strings.TrimSpace(v.Name) == "" {
			continue
		}
		members = append(members, v.Archive)
	}

	emit("extracting app archive")
	if err := tarExtractMembers(ctx, wrapperPath, workDir, members); err != nil {
		return err
	}

	appInner := filepath.Join(workDir, filepath.FromSlash(appMember))
	if err := os.MkdirAll(workspaceRoot, 0750); err != nil {
		return err
	}
	emit("restoring workspace")
	if err := tarExtractGz(ctx, appInner, workspaceRoot); err != nil {
		return err
	}

	vols := make([]AppArchiveVolumeRef, 0, len(manifest.Volumes))
	for _, v := range manifest.Volumes {
		if strings.TrimSpace(v.Name) == "" || strings.TrimSpace(v.Archive) == "" {
			continue
		}
		vols = append(vols, v)
	}
	if len(vols) == 0 {
		return nil
	}

	sem := newSemaphore(ParallelWorkers())
	g, gctx := errgroup.WithContext(ctx)
	for _, v := range vols {
		v := v
		g.Go(func() error {
			if err := sem.acquire(gctx); err != nil {
				return err
			}
			defer sem.release()
			inner := filepath.Join(workDir, filepath.FromSlash(v.Archive))
			emit("restoring volume " + v.Name)
			if msg := volumex.ExtractTarGzForBackupRestore(gctx, v.Name, inner); msg != "" {
				return fmt.Errorf("volume %s: %s", v.Name, msg)
			}
			return nil
		})
	}
	return g.Wait()
}
