package dockerapi

import (
	"context"
	"sort"
	"strings"

	"github.com/docker/docker/api/types"
)

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

	for _, c := range list {
		cname := containerName(c)
		proj := strings.TrimSpace(c.Labels[composeProjectLabel])
		workDir := strings.TrimSpace(c.Labels["com.docker.compose.project.working_dir"])
		for _, m := range c.Mounts {
			if m.Type != "volume" {
				continue
			}
			vn := strings.TrimSpace(m.Name)
			if vn == "" {
				continue
			}
			index.refsByVolume[vn] = append(index.refsByVolume[vn], VolumeMountRef{
				VolumeName:     vn,
				ContainerName:  cname,
				ComposeProject: proj,
				WorkingDir:     workDir,
			})
		}
	}
	return index, nil
}
