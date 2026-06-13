package volumex

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"panel/internal/dockerapi"
	"panel/internal/resmatch"
)

const volumeIndexTTL = 20 * time.Second

type AppVolumeQuery struct {
	AppID            string
	AppDisplayName   string
	WorkspaceRoot    string
	ComposeProjects  []string
	AllPanelProjects []string
}

type Matcher struct {
	index *dockerapi.VolumeMountIndex
}

var (
	matcherMu    sync.Mutex
	matcherAt    time.Time
	matcherIndex *dockerapi.VolumeMountIndex
)

func SharedMatcher(ctx context.Context) *Matcher {
	matcherMu.Lock()
	defer matcherMu.Unlock()
	if matcherIndex == nil || time.Since(matcherAt) >= volumeIndexTTL {
		idx, err := dockerapi.BuildVolumeMountIndex(ctx)
		if err != nil {
			return &Matcher{}
		}
		matcherIndex = idx
		matcherAt = time.Now()
	}
	return &Matcher{index: matcherIndex}
}

func workspaceDirContainedInApp(appRoot, workDir string) bool {
	if appRoot == "" || workDir == "" {
		return false
	}
	appRoot = filepath.Clean(appRoot)
	workDir = filepath.Clean(workDir)
	if appRoot == workDir {
		return true
	}
	rel, err := filepath.Rel(appRoot, workDir)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func mountRefMatchesApp(ref dockerapi.VolumeMountRef, q AppVolumeQuery) bool {
	if q.WorkspaceRoot != "" && ref.WorkingDir != "" {
		if workspaceDirContainedInApp(q.WorkspaceRoot, ref.WorkingDir) {
			return true
		}
	}
	if ref.ComposeProject != "" {
		for _, p := range q.ComposeProjects {
			p = strings.TrimSpace(p)
			if p != "" && ref.ComposeProject == p {
				return true
			}
		}
	}
	if ref.ContainerName != "" {
		for _, p := range q.ComposeProjects {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if resmatch.MatchesComposeContainerName(ref.ContainerName, p, q.AllPanelProjects) {
				return true
			}
		}
	}
	return false
}

func volumesFromContainerIndex(index *dockerapi.VolumeMountIndex, q AppVolumeQuery) []string {
	if index == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	for _, vol := range index.MountedVolumeNames() {
		for _, ref := range index.RefsForVolume(vol) {
			if mountRefMatchesApp(ref, q) {
				if _, ok := seen[vol]; !ok {
					seen[vol] = struct{}{}
					out = append(out, vol)
				}
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

func (m *Matcher) MatchAppIDForVolume(vol string, queries []AppVolumeQuery) string {
	vol = strings.TrimSpace(vol)
	if vol == "" {
		return ""
	}
	if m != nil && m.index != nil {
		for _, ref := range m.index.RefsForVolume(vol) {
			for _, q := range queries {
				if mountRefMatchesApp(ref, q) {
					return q.AppID
				}
			}
		}
	}
	best := ""
	bestLen := -1
	for _, q := range queries {
		for _, p := range q.ComposeProjects {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if resmatch.MatchesVolumeName(vol, p) && len(p) > bestLen {
				best = q.AppID
				bestLen = len(p)
			}
		}
	}
	return best
}

func (m *Matcher) ListForAppFromNames(ctx context.Context, q AppVolumeQuery, allNames []string) ([]string, string) {
	return listAppVolumes(ctx, m, q, allNames)
}

func listAppVolumes(ctx context.Context, m *Matcher, q AppVolumeQuery, allNames []string) ([]string, string) {
	seen := map[string]struct{}{}
	var out []string
	add := func(vol string) {
		vol = strings.TrimSpace(vol)
		if vol == "" {
			return
		}
		if _, ok := seen[vol]; ok {
			return
		}
		seen[vol] = struct{}{}
		out = append(out, vol)
	}

	var index *dockerapi.VolumeMountIndex
	if m != nil {
		index = m.index
	}
	for _, vol := range volumesFromContainerIndex(index, q) {
		add(vol)
	}

	for _, proj := range q.ComposeProjects {
		proj = strings.TrimSpace(proj)
		if proj == "" {
			continue
		}
		matched, msg := listByComposeProjectLabel(ctx, proj)
		if msg != "" {
			return nil, msg
		}
		for _, vol := range matched {
			add(vol)
		}
	}

	for _, vol := range FilterForApp(q.AppID, allNames, q.ComposeProjects) {
		add(vol)
	}

	sort.Strings(out)
	return out, ""
}
