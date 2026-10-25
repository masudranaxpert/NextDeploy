package dockerapi

import (
	"context"
	"sync"
	"time"

	"github.com/docker/docker/api/types/volume"
)

const volumeLabelsTTL = 20 * time.Second

var (
	volLabelsMu sync.Mutex
	volLabelsAt time.Time
	volLabels   map[string][]string // compose project -> volume names
)

// VolumesByComposeProject returns every Docker volume grouped by its compose project label. One
// Docker call serves all projects, so a page with many projects does not start one `docker volume
// ls` process per project; the answer is reused for a few seconds.
func VolumesByComposeProject(ctx context.Context) (map[string][]string, error) {
	volLabelsMu.Lock()
	defer volLabelsMu.Unlock()
	if volLabels != nil && time.Since(volLabelsAt) < volumeLabelsTTL {
		return volLabels, nil
	}
	cli, err := apiClient()
	if err != nil {
		return nil, err
	}
	list, err := cli.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string)
	for _, v := range list.Volumes {
		if v == nil {
			continue
		}
		if proj := v.Labels[composeProjectLabel]; proj != "" {
			out[proj] = append(out[proj], v.Name)
		}
	}
	volLabels, volLabelsAt = out, time.Now()
	return out, nil
}

// InvalidateVolumeLabels drops the cached grouping after a volume is created or removed.
func InvalidateVolumeLabels() {
	volLabelsMu.Lock()
	volLabelsAt = time.Time{}
	volLabelsMu.Unlock()
}
