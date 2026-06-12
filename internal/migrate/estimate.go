package migrate

import (
	"context"
	"os"
	"path/filepath"

	"panel/internal/db"
	"panel/internal/volumex"
)

type EstimateInput struct {
	WorkspaceBytes func(appID string) int64
	VolumeNames    func(ctx context.Context, app db.App) ([]string, error)
}

type SizeEstimate struct {
	WorkspaceBytes int64
	VolumeBytes    int64
	VolumeCount    int
	AppCount       int
}

func (e SizeEstimate) TotalBytes() int64 {
	return e.WorkspaceBytes + e.VolumeBytes
}

func EstimateApps(ctx context.Context, database *db.Store, apps []db.App, in EstimateInput) (SizeEstimate, error) {
	var out SizeEstimate
	out.AppCount = len(apps)
	allVolNames, _ := volumex.List(ctx)
	for _, app := range apps {
		out.WorkspaceBytes += in.WorkspaceBytes(app.ID)
		if in.VolumeNames == nil {
			continue
		}
		names, err := in.VolumeNames(ctx, app)
		if err != nil {
			return out, err
		}
		out.VolumeCount += len(names)
		for _, vol := range names {
			out.VolumeBytes += volumeDiskBytes(vol)
		}
		_ = allVolNames
	}
	return out, nil
}

func volumeDiskBytes(volumeName string) int64 {
	mount, err := volumeMountpoint(volumeName)
	if err != nil || mount == "" {
		return 0
	}
	return dirSizeBytes(mount)
}

func dirSizeBytes(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, ierr := d.Info(); ierr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
