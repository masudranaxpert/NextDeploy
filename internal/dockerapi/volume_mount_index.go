package dockerapi

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/docker/docker/api/types"
	"golang.org/x/sync/errgroup"
)

const volumeInspectWorkers = 6

type VolumeMountRef struct {
	VolumeName     string
	ContainerName  string
	ComposeProject string
	WorkingDir     string
}

type VolumeMountIndex struct {
	refsByVolume map[string][]VolumeMountRef
}

func (idx *VolumeMountIndex) RefsForVolume(name string) []VolumeMountRef {
	if idx == nil {
		return nil
	}
	return idx.refsByVolume[name]
}

func (idx *VolumeMountIndex) MountedVolumeNames() []string {
	if idx == nil || len(idx.refsByVolume) == 0 {
		return nil
	}
	out := make([]string, 0, len(idx.refsByVolume))
	for name := range idx.refsByVolume {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (idx *VolumeMountIndex) RefsByVolume() map[string][]VolumeMountRef {
	if idx == nil {
		return nil
	}
	return idx.refsByVolume
}

func NewVolumeMountIndex(refs map[string][]VolumeMountRef) *VolumeMountIndex {
	if refs == nil {
		refs = map[string][]VolumeMountRef{}
	}
	return &VolumeMountIndex{refsByVolume: refs}
}

func BuildVolumeMountIndex(ctx context.Context) (*VolumeMountIndex, error) {
	cli, err := apiClient()
	if err != nil {
		return nil, err
	}
	list, err := cli.ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return &VolumeMountIndex{refsByVolume: map[string][]VolumeMountRef{}}, nil
	}

	index := &VolumeMountIndex{refsByVolume: make(map[string][]VolumeMountRef, len(list))}
	sem := make(chan struct{}, volumeInspectWorkers)
	g, gctx := errgroup.WithContext(ctx)
	var mu sync.Mutex

	for i := range list {
		c := list[i]
		g.Go(func() error {
			select {
			case sem <- struct{}{}:
			case <-gctx.Done():
				return gctx.Err()
			}
			defer func() { <-sem }()

			insp, err := cli.ContainerInspect(gctx, c.ID)
			if err != nil {
				return nil
			}
			cname := containerName(c)
			proj := ""
			workDir := ""
			if insp.Config != nil {
				proj = strings.TrimSpace(insp.Config.Labels[composeProjectLabel])
				workDir = strings.TrimSpace(insp.Config.Labels["com.docker.compose.project.working_dir"])
			}
			var batch []VolumeMountRef
			for _, m := range insp.Mounts {
				if m.Type != "volume" {
					continue
				}
				vn := strings.TrimSpace(m.Name)
				if vn == "" {
					continue
				}
				batch = append(batch, VolumeMountRef{
					VolumeName:     vn,
					ContainerName:  cname,
					ComposeProject: proj,
					WorkingDir:     workDir,
				})
			}
			if len(batch) == 0 {
				return nil
			}
			mu.Lock()
			for _, ref := range batch {
				index.refsByVolume[ref.VolumeName] = append(index.refsByVolume[ref.VolumeName], ref)
			}
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()
	return index, nil
}
