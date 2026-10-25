package dockerapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/shirou/gopsutil/v3/mem"
	"golang.org/x/sync/errgroup"

	"panel/internal/resmatch"
	"panel/internal/workspace"
)

const composeProjectLabel = "com.docker.compose.project"
const composeWorkingDirLabel = "com.docker.compose.project.working_dir"

func containerStatsEligible(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "running", "paused":
		return true
	default:
		return false
	}
}

var (
	apiClientOnce sync.Once
	apiClientInst *client.Client
	apiClientErr  error
)

func apiClient() (*client.Client, error) {
	apiClientOnce.Do(func() {
		ver := strings.TrimSpace(os.Getenv("DOCKER_API_VERSION"))
		if ver == "" {
			ver = "1.44"
		}
		opts := []client.Opt{
			client.FromEnv,
			client.WithVersion(ver),
		}
		apiClientInst, apiClientErr = client.NewClientWithOpts(opts...)
	})
	return apiClientInst, apiClientErr
}

// Client returns the shared Docker API client instance.
func Client() (*client.Client, error) {
	return apiClient()
}

type ContainerRow struct {
	ID             string
	Name           string
	Image          string
	State          string
	Status         string
	Ports          string
	Created        time.Time
	ComposeProject string
	ComposeService string
}

type ImageRow struct {
	ID        string
	Tags      string
	Size      int64
	SizeHuman string
	Created   time.Time
	RepoTags  []string
}

type ContainerUsageRow struct {
	ID              string
	Name            string
	Image           string
	State           string
	Status          string
	ComposeProject  string
	ComposeService  string
	CPUPercent      float64
	MemUsage        uint64
	MemLimit        uint64
	MemPercent      float64
	NetInput        uint64
	NetOutput       uint64
	BlockRead       uint64
	BlockWrite      uint64
	Pids            uint64
	MemUsageHuman   string
	MemLimitHuman   string
	NetInputHuman   string
	NetOutputHuman  string
	BlockReadHuman  string
	BlockWriteHuman string
	NetDLRateHuman  string
	NetULRateHuman  string
}

type statsJSON struct {
	Read        time.Time `json:"read"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	CPUStats struct {
		CPUUsage struct {
			TotalUsage  uint64   `json:"total_usage"`
			PercpuUsage []uint64 `json:"percpu_usage"`
		} `json:"cpu_usage"`
		SystemUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs  uint32 `json:"online_cpus"`
	} `json:"cpu_stats"`
	MemoryStats struct {
		Usage uint64            `json:"usage"`
		Limit uint64            `json:"limit"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
	Networks map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	} `json:"networks"`
	BlkioStats struct {
		IoServiceBytesRecursive []struct {
			Op    string `json:"op"`
			Value uint64 `json:"value"`
		} `json:"io_service_bytes_recursive"`
	} `json:"blkio_stats"`
	PidsStats struct {
		Current uint64 `json:"current"`
	} `json:"pids_stats"`
}

var (
	containersCacheMu   sync.RWMutex
	containersCacheList []ContainerRow
	containersCacheAt   time.Time
	containersCacheTTL  = 3 * time.Second

	imagesCacheMu   sync.RWMutex
	imagesCacheList []ImageRow
	imagesCacheAt   time.Time
	imagesCacheTTL  = 10 * time.Second
)

func InvalidateContainerCache() {
	containersCacheMu.Lock()
	containersCacheAt = time.Time{}
	containersCacheMu.Unlock()
}

func InvalidateImageCache() {
	imagesCacheMu.Lock()
	imagesCacheAt = time.Time{}
	imagesCacheMu.Unlock()
}

func ListContainers(ctx context.Context) ([]ContainerRow, string) {
	containersCacheMu.RLock()
	if time.Since(containersCacheAt) < containersCacheTTL && len(containersCacheList) > 0 {
		cached := append([]ContainerRow(nil), containersCacheList...)
		containersCacheMu.RUnlock()
		return cached, ""
	}
	containersCacheMu.RUnlock()

	cli, err := apiClient()
	if err != nil {
		return nil, err.Error()
	}

	list, err := cli.ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		return nil, err.Error()
	}
	out := make([]ContainerRow, 0, len(list))
	for _, c := range list {
		ports := formatPorts(c.Ports)
		out = append(out, ContainerRow{
			ID:             c.ID[:12],
			Name:           containerName(c),
			Image:          c.Image,
			State:          c.State,
			Status:         c.Status,
			Ports:          ports,
			Created:        time.Unix(c.Created, 0).UTC(),
			ComposeProject: c.Labels[composeProjectLabel],
			ComposeService: c.Labels["com.docker.compose.service"],
		})
	}

	containersCacheMu.Lock()
	containersCacheList = out
	containersCacheAt = time.Now()
	containersCacheMu.Unlock()

	return out, ""
}

type ComposeContainerRow struct {
	Name       string
	State      string
	Status     string
	Project    string
	WorkingDir string
	Image      string
}

func ListComposeContainers(ctx context.Context) ([]ComposeContainerRow, string) {
	cli, err := apiClient()
	if err != nil {
		return nil, err.Error()
	}
	list, err := cli.ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		return nil, err.Error()
	}
	out := make([]ComposeContainerRow, 0, len(list))
	for _, c := range list {
		proj := strings.TrimSpace(c.Labels[composeProjectLabel])
		if proj == "" {
			continue
		}
		out = append(out, ComposeContainerRow{
			Name:       containerName(c),
			State:      c.State,
			Status:     c.Status,
			Project:    proj,
			WorkingDir: strings.TrimSpace(c.Labels[composeWorkingDirLabel]),
			Image:      c.Image,
		})
	}
	return out, ""
}

func ContainerComposeProjectAndMountSource(ctx context.Context, containerName, mountDestination string) (project, source string, err error) {
	cli, err := apiClient()
	if err != nil {
		return "", "", err
	}

	insp, err := cli.ContainerInspect(ctx, strings.TrimSpace(containerName))
	if err != nil {
		return "", "", err
	}
	if insp.Config != nil {
		project = strings.TrimSpace(insp.Config.Labels[composeProjectLabel])
	}
	want := filepath.ToSlash(filepath.Clean(strings.TrimSpace(mountDestination)))
	for _, m := range insp.Mounts {
		if filepath.ToSlash(filepath.Clean(m.Destination)) == want {
			return project, strings.TrimSpace(m.Source), nil
		}
	}
	return project, "", fmt.Errorf("mount %q not found on container %q", mountDestination, containerName)
}

func ListContainerUsage(ctx context.Context) ([]ContainerUsageRow, string) {
	return listContainerUsageFiltered(ctx, nil)
}

// ListContainerUsageForProjects returns live stats only for containers whose
// compose project label is in the given set. Stats are fetched only for the
// matching containers, so this stays fast on busy hosts.
func ListContainerUsageForProjects(ctx context.Context, projects map[string]bool) ([]ContainerUsageRow, string) {
	if len(projects) == 0 {
		return nil, ""
	}
	return listContainerUsageFiltered(ctx, func(c types.Container) bool {
		return projects[c.Labels[composeProjectLabel]]
	})
}

func listContainerUsageFiltered(ctx context.Context, keep func(types.Container) bool) ([]ContainerUsageRow, string) {
	cli, err := apiClient()
	if err != nil {
		return nil, err.Error()
	}

	all, err := cli.ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		return nil, err.Error()
	}
	list := all
	if keep != nil {
		list = list[:0]
		for _, c := range all {
			if keep(c) {
				list = append(list, c)
			}
		}
	}

	ids := make([]string, 0, len(list))
	for _, c := range list {
		if containerStatsEligible(c.State) {
			ids = append(ids, c.ID)
		}
	}

	// up to 32 containers: take the slower, accurate CPU reading (all of them are sampled at once)
	statsMap := fetchStatsSnapshot(ctx, cli, ids, len(ids) <= 32)

	out := make([]ContainerUsageRow, 0, len(list))
	for _, c := range list {
		row := ContainerUsageRow{
			ID:             c.ID[:12],
			Name:           containerName(c),
			Image:          c.Image,
			State:          c.State,
			Status:         c.Status,
			ComposeProject: c.Labels[composeProjectLabel],
			ComposeService: c.Labels["com.docker.compose.service"],
		}

		cur, ok := statsMap[c.ID]
		if !ok {
			row.MemUsageHuman = "—"
			row.MemLimitHuman = "—"
			row.NetInputHuman = formatBytes(0)
			row.NetOutputHuman = formatBytes(0)
			row.BlockReadHuman = formatBytes(0)
			row.BlockWriteHuman = formatBytes(0)
			out = append(out, row)
			continue
		}
		row.CPUPercent = math.Round(cpuPercentSingleSample(cur)*100) / 100
		memUsage := memoryUsageLikeDockerCLI(cur)
		row.MemUsage = memUsage
		row.MemLimit = cur.MemoryStats.Limit
		if row.MemLimit > 0 {
			row.MemPercent = math.Round(((float64(row.MemUsage) / float64(row.MemLimit)) * 100)*10) / 10
		}
		row.MemUsageHuman = formatBytes(int64(row.MemUsage))
		row.MemLimitHuman = formatBytes(int64(row.MemLimit))
		for _, net := range cur.Networks {
			row.NetInput += net.RxBytes
			row.NetOutput += net.TxBytes
		}
		for _, entry := range cur.BlkioStats.IoServiceBytesRecursive {
			switch strings.ToLower(strings.TrimSpace(entry.Op)) {
			case "read":
				row.BlockRead += entry.Value
			case "write":
				row.BlockWrite += entry.Value
			}
		}
		row.Pids = cur.PidsStats.Current

		if row.MemUsageHuman == "" {
			row.MemUsageHuman = "—"
		}
		if row.MemLimitHuman == "" {
			row.MemLimitHuman = "—"
		}
		row.NetInputHuman = formatBytes(int64(row.NetInput))
		row.NetOutputHuman = formatBytes(int64(row.NetOutput))
		row.BlockReadHuman = formatBytes(int64(row.BlockRead))
		row.BlockWriteHuman = formatBytes(int64(row.BlockWrite))
		out = append(out, row)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].State == out[j].State {
			if out[i].CPUPercent == out[j].CPUPercent {
				return out[i].Name < out[j].Name
			}
			return out[i].CPUPercent > out[j].CPUPercent
		}
		if out[i].State == "running" {
			return true
		}
		if out[j].State == "running" {
			return false
		}
		return out[i].Name < out[j].Name
	})
	return out, ""
}

func cpuPercentSingleSample(cur statsJSON) float64 {
	cpuDelta := float64(cur.CPUStats.CPUUsage.TotalUsage) - float64(cur.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(cur.CPUStats.SystemUsage) - float64(cur.PreCPUStats.SystemUsage)
	if cpuDelta < 0 || systemDelta <= 0 {
		return 0
	}
	online := float64(cur.CPUStats.OnlineCPUs)
	if online <= 0 {
		online = float64(len(cur.CPUStats.CPUUsage.PercpuUsage))
	}
	if online <= 0 {
		online = 1
	}
	return (cpuDelta / systemDelta) * online * 100
}

func ListImages(ctx context.Context) ([]ImageRow, string) {
	imagesCacheMu.RLock()
	if time.Since(imagesCacheAt) < imagesCacheTTL && len(imagesCacheList) > 0 {
		cached := append([]ImageRow(nil), imagesCacheList...)
		imagesCacheMu.RUnlock()
		return cached, ""
	}
	imagesCacheMu.RUnlock()

	cli, err := apiClient()
	if err != nil {
		return nil, err.Error()
	}

	list, err := cli.ImageList(ctx, types.ImageListOptions{All: false})
	if err != nil {
		return nil, err.Error()
	}
	out := make([]ImageRow, 0, len(list))
	for _, im := range list {
		tags := strings.Join(im.RepoTags, ", ")
		if tags == "" {
			tags = "<none>"
		}
		shortID := im.ID
		if strings.HasPrefix(shortID, "sha256:") {
			shortID = shortID[7:]
		}
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}
		out = append(out, ImageRow{
			ID:        shortID,
			Tags:      tags,
			Size:      im.Size,
			SizeHuman: formatBytes(im.Size),
			Created:   time.Unix(im.Created, 0).UTC(),
			RepoTags:  im.RepoTags,
		})
	}

	imagesCacheMu.Lock()
	imagesCacheList = out
	imagesCacheAt = time.Now()
	imagesCacheMu.Unlock()

	return out, ""
}

func formatBytes(n int64) string {
	return workspace.FormatByteSize(n)
}

func HumanBytes(n uint64) string {
	return workspace.FormatByteSize(int64(n))
}

// fetchContainerStatsJSON reads one stats sample. A one-shot sample has no previous CPU reading
// (so the CPU figure would be the average since the container started, close to 0); accurate
// asks Docker to take two readings about a second apart, which gives the CPU use right now.
func fetchContainerStatsJSON(ctx context.Context, cli *client.Client, id string, accurate bool) (statsJSON, error) {
	var out statsJSON
	timeout := 3 * time.Second
	if accurate {
		timeout = 6 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var stats types.ContainerStats
	var err error
	if accurate {
		stats, err = cli.ContainerStats(cctx, id, false)
	} else {
		stats, err = cli.ContainerStatsOneShot(cctx, id)
	}
	if err != nil {
		return out, err
	}
	defer stats.Body.Close()
	body, err := io.ReadAll(stats.Body)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, err
	}
	return out, nil
}

func autoStatsConcurrency() int {
	if v := strings.TrimSpace(os.Getenv("PANEL_STATS_CONCURRENCY")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 32 {
			return n
		}
	}

	ramGB := 2.0
	if v, err := mem.VirtualMemory(); err == nil && v != nil {
		ramGB = float64(v.Available) / (1024 * 1024 * 1024)
	}

	cores := runtime.NumCPU()
	switch {
	case ramGB <= 1.0:
		return 4
	case ramGB <= 2.0 && cores <= 2:
		return 6
	case ramGB <= 2.0:
		return 10
	default:
		limit := cores * 4
		if limit > 16 {
			limit = 16
		}
		return limit
	}
}

func fetchStatsSnapshot(ctx context.Context, cli *client.Client, ids []string, accurate bool) map[string]statsJSON {
	if len(ids) == 0 {
		return nil
	}
	var mu sync.Mutex
	out := make(map[string]statsJSON, len(ids))
	eg, gctx := errgroup.WithContext(ctx)
	eg.SetLimit(autoStatsConcurrency())
	for _, id := range ids {
		id := id
		eg.Go(func() error {
			j, err := fetchContainerStatsJSON(gctx, cli, id, accurate)
			if err != nil {
				return nil
			}
			mu.Lock()
			out[id] = j
			mu.Unlock()
			return nil
		})
	}
	_ = eg.Wait()
	return out
}

func memoryUsageLikeDockerCLI(s statsJSON) uint64 {
	usage := s.MemoryStats.Usage
	st := s.MemoryStats.Stats
	if st == nil {
		return usage
	}
	if v, ok := st["inactive_file"]; ok && usage > v {
		return usage - v
	}
	if v, ok := st["total_inactive_file"]; ok && usage > v {
		return usage - v
	}
	if v, ok := st["cache"]; ok && usage > v {
		return usage - v
	}
	return usage
}

func cpuPercentBetweenSamples(prev, cur statsJSON) float64 {
	cpuDelta := float64(cur.CPUStats.CPUUsage.TotalUsage) - float64(prev.CPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(cur.CPUStats.SystemUsage) - float64(prev.CPUStats.SystemUsage)
	if cpuDelta < 0 || systemDelta <= 0 {
		return 0
	}
	online := float64(cur.CPUStats.OnlineCPUs)
	if online <= 0 {
		online = float64(len(cur.CPUStats.CPUUsage.PercpuUsage))
	}
	if online <= 0 {
		online = 1
	}
	return (cpuDelta / systemDelta) * online * 100
}

func formatPorts(ports []types.Port) string {
	if len(ports) == 0 {
		return "—"
	}
	var b strings.Builder
	for i, p := range ports {
		if i > 0 {
			b.WriteString(", ")
		}
		if p.PublicPort != 0 {
			fmt.Fprintf(&b, "%d→%d/%s", p.PublicPort, p.PrivatePort, p.Type)
		} else {
			fmt.Fprintf(&b, "%d/%s", p.PrivatePort, p.Type)
		}
	}
	return b.String()
}

func canonicalImageIDHex(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	id = strings.TrimPrefix(id, "sha256:")
	return id
}

func imageIDsMatch(a, b string) bool {
	a, b = canonicalImageIDHex(a), canonicalImageIDHex(b)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	if len(a) >= 12 && len(b) >= 12 && (strings.HasPrefix(a, b[:12]) || strings.HasPrefix(b, a[:12])) {
		return true
	}
	return false
}

func removeContainersUsingImage(ctx context.Context, cli *client.Client, imageID string) []string {
	list, err := cli.ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		return []string{err.Error()}
	}
	var errs []string
	for _, c := range list {
		if !imageIDsMatch(c.ImageID, imageID) {
			continue
		}
		name := containerName(c)
		if err := cli.ContainerRemove(ctx, c.ID, types.ContainerRemoveOptions{Force: true, RemoveVolumes: false}); err != nil {
			errs = append(errs, name+": "+err.Error())
		}
	}
	return errs
}

// imageRepoBase returns the repository name without tag (last path segment only).
func imageRepoBase(repo string) string {
	repo = strings.TrimSpace(repo)
	if i := strings.LastIndex(repo, ":"); i > 0 {
		repo = repo[:i]
	}
	if i := strings.LastIndex(repo, "/"); i >= 0 {
		repo = repo[i+1:]
	}
	return repo
}

func imageTagLooksOwnedByComposeProject(tag, projectID string) bool {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return false
	}
	alt := strings.ReplaceAll(projectID, "-", "_")
	repo := imageRepoBase(tag)
	if repo == "" {
		return false
	}
	if repo == projectID || repo == alt {
		return true
	}
	if strings.HasPrefix(repo, projectID+"_") || strings.HasPrefix(repo, alt+"_") {
		return true
	}
	return false
}

func RemoveAppImages(ctx context.Context, projectID string) []string {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil
	}
	cli, err := apiClient()
	if err != nil {
		return []string{err.Error()}
	}

	list, err := cli.ImageList(ctx, types.ImageListOptions{All: true})
	if err != nil {
		return []string{err.Error()}
	}
	seen := make(map[string]struct{})
	var errs []string
	for _, im := range list {
		match := false
		for _, t := range im.RepoTags {
			if t == "" {
				continue
			}
			if imageTagLooksOwnedByComposeProject(t, projectID) {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		if _, ok := seen[im.ID]; ok {
			continue
		}
		seen[im.ID] = struct{}{}
		if ce := removeContainersUsingImage(ctx, cli, im.ID); len(ce) > 0 {
			errs = append(errs, ce...)
		}
		if _, err := cli.ImageRemove(ctx, im.ID, types.ImageRemoveOptions{Force: true, PruneChildren: true}); err != nil {
			short := im.ID
			if len(short) > 12 {
				short = short[:12]
			}
			errs = append(errs, short+": "+err.Error())
		}
	}
	return errs
}

func RemoveContainersByComposeProject(ctx context.Context, projectID string) []string {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil
	}
	cli, err := apiClient()
	if err != nil {
		return []string{err.Error()}
	}

	fl := filters.NewArgs(filters.Arg("label", composeProjectLabel+"="+projectID))
	list, err := cli.ContainerList(ctx, types.ContainerListOptions{All: true, Filters: fl})
	if err != nil {
		return []string{err.Error()}
	}
	var errs []string
	for _, c := range list {
		name := containerName(c)
		if err := cli.ContainerRemove(ctx, c.ID, types.ContainerRemoveOptions{Force: true, RemoveVolumes: false}); err != nil {
			errs = append(errs, name+": "+err.Error())
		}
	}
	return errs
}

func RemoveAppContainers(ctx context.Context, projectID string, allProjects []string) []string {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil
	}
	cli, err := apiClient()
	if err != nil {
		return []string{err.Error()}
	}

	list, err := cli.ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		return []string{err.Error()}
	}
	var errs []string
	for _, c := range list {
		name := containerName(c)
		lbl := strings.TrimSpace(c.Labels[composeProjectLabel])
		if lbl == projectID {
			if err := cli.ContainerRemove(ctx, c.ID, types.ContainerRemoveOptions{Force: true, RemoveVolumes: false}); err != nil {
				errs = append(errs, name+": "+err.Error())
			}
			continue
		}
		if lbl != "" {
			continue
		}
		n := strings.TrimPrefix(name, "/")
		if resmatch.MatchesComposeContainerName(n, projectID, allProjects) {
			if err := cli.ContainerRemove(ctx, c.ID, types.ContainerRemoveOptions{Force: true, RemoveVolumes: false}); err != nil {
				errs = append(errs, name+": "+err.Error())
			}
		}
	}
	return errs
}

func RemoveAppNetworks(ctx context.Context, projectID string) []string {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil
	}
	cli, err := apiClient()
	if err != nil {
		return []string{err.Error()}
	}

	fl := filters.NewArgs(filters.Arg("label", composeProjectLabel+"="+projectID))
	nets, err := cli.NetworkList(ctx, types.NetworkListOptions{Filters: fl})
	if err != nil {
		return []string{err.Error()}
	}
	var errs []string
	for _, n := range nets {
		if err := cli.NetworkRemove(ctx, n.ID); err != nil {
			errs = append(errs, n.Name+": "+err.Error())
		}
	}
	return errs
}

func containerIDByName(ctx context.Context, name string) (string, error) {
	cli, err := apiClient()
	if err != nil {
		return "", err
	}

	list, err := cli.ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	for _, c := range list {
		for _, n := range c.Names {
			trimmed := strings.TrimPrefix(n, "/")
			if trimmed == name || (name == "caddy" && (trimmed == "nextdeploy_caddy" || strings.Contains(trimmed, "caddy"))) {
				return c.ID, nil
			}
		}
	}
	return "", fmt.Errorf("container %q not found", name)
}

func StartContainerByName(ctx context.Context, name string) error {
	id, err := containerIDByName(ctx, name)
	if err != nil {
		return err
	}
	cli, err := apiClient()
	if err != nil {
		return err
	}

	return cli.ContainerStart(ctx, id, types.ContainerStartOptions{})
}

func StopContainerByName(ctx context.Context, name string) error {
	id, err := containerIDByName(ctx, name)
	if err != nil {
		return err
	}
	cli, err := apiClient()
	if err != nil {
		return err
	}

	timeout := 10
	return cli.ContainerStop(ctx, id, container.StopOptions{Timeout: &timeout})
}

func RestartContainerByName(ctx context.Context, name string) error {
	id, err := containerIDByName(ctx, name)
	if err != nil {
		return err
	}
	cli, err := apiClient()
	if err != nil {
		return err
	}

	timeout := 10
	return cli.ContainerRestart(ctx, id, container.StopOptions{Timeout: &timeout})
}

// EnsureContainerNetwork verifies if containerName is attached to networkName.
// If not attached, it connects the container dynamically.
func EnsureContainerNetwork(ctx context.Context, containerName, networkName string) error {
	containerName = strings.TrimSpace(containerName)
	networkName = strings.TrimSpace(networkName)
	if containerName == "" || networkName == "" {
		return nil
	}
	cli, err := apiClient()
	if err != nil {
		return err
	}
	insp, err := cli.ContainerInspect(ctx, containerName)
	if err != nil {
		return err
	}
	if insp.NetworkSettings != nil && insp.NetworkSettings.Networks != nil {
		if _, ok := insp.NetworkSettings.Networks[networkName]; ok {
			return nil // already connected
		}
	}
	if err := cli.NetworkConnect(ctx, networkName, containerName, nil); err != nil {
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "already exists") || strings.Contains(lower, "already connected") {
			return nil
		}
		out, cliErr := exec.CommandContext(ctx, "docker", "network", "connect", networkName, containerName).CombinedOutput()
		if cliErr != nil && !strings.Contains(strings.ToLower(string(out)), "already exists") {
			return fmt.Errorf("connect %s to %s: %s (%w)", containerName, networkName, strings.TrimSpace(string(out)), cliErr)
		}
	}
	return nil
}

// EnsureCaddySharedNetwork ensures that the Caddy container shares the same Docker network
// as the NextDeploy panel container (default "NextDeploy").
func EnsureCaddySharedNetwork(ctx context.Context) error {
	panelNet := "NextDeploy"
	if cli, err := apiClient(); err == nil {
		candNames := []string{"panel", os.Getenv("HOSTNAME")}
		for _, name := range candNames {
			if strings.TrimSpace(name) == "" {
				continue
			}
			if insp, err := cli.ContainerInspect(ctx, name); err == nil && insp.NetworkSettings != nil {
				for netName := range insp.NetworkSettings.Networks {
					if netName != "bridge" && netName != "host" && netName != "none" {
						panelNet = netName
						break
					}
				}
				break
			}
		}
	}
	return EnsureContainerNetwork(ctx, "caddy", panelNet)
}

func RemoveContainerByName(ctx context.Context, name string) error {
	id, err := containerIDByName(ctx, name)
	if err != nil {
		return err
	}
	cli, err := apiClient()
	if err != nil {
		return err
	}

	return cli.ContainerRemove(ctx, id, types.ContainerRemoveOptions{Force: true, RemoveVolumes: false})
}

func RemoveImageByID(ctx context.Context, imageID string) error {
	cli, err := apiClient()
	if err != nil {
		return err
	}

	_, err = cli.ImageRemove(ctx, imageID, types.ImageRemoveOptions{Force: false, PruneChildren: true})
	return err
}

func RemoveVolumeByName(ctx context.Context, name string) error {
	cli, err := apiClient()
	if err != nil {
		return err
	}

	return cli.VolumeRemove(ctx, name, false)
}

func PruneImages(ctx context.Context) error {
	cli, err := apiClient()
	if err != nil {
		return err
	}

	_, err = cli.ImagesPrune(ctx, filters.Args{})
	return err
}

func PruneContainers(ctx context.Context) error {
	cli, err := apiClient()
	if err != nil {
		return err
	}

	_, err = cli.ContainersPrune(ctx, filters.Args{})
	return err
}

func containerName(c types.Container) string {
	if len(c.Names) > 0 {
		return strings.TrimPrefix(c.Names[0], "/")
	}
	return ""
}

type ComposePsRow struct {
	Name       string
	Service    string
	State      string
	Status     string
	Image      string
	Ports      string
	WorkingDir string
}

func ComposePS(ctx context.Context, project string) ([]ComposePsRow, error) {
	project = strings.TrimSpace(project)
	if project == "" {
		return nil, fmt.Errorf("empty project name")
	}
	cli, err := apiClient()
	if err != nil {
		return nil, err
	}

	fl := filters.NewArgs(filters.Arg("label", composeProjectLabel+"="+project))
	list, err := cli.ContainerList(ctx, types.ContainerListOptions{All: true, Filters: fl})
	if err != nil {
		return nil, err
	}

	out := make([]ComposePsRow, 0, len(list))
	for _, c := range list {
		service := c.Labels["com.docker.compose.service"]
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		} else {
			name = c.ID[:12]
		}
		var portStrs []string
		for _, p := range c.Ports {
			if p.PublicPort > 0 {
				if p.IP != "" && p.IP != "0.0.0.0" {
					portStrs = append(portStrs, fmt.Sprintf("%s:%d->%d/%s", p.IP, p.PublicPort, p.PrivatePort, p.Type))
				} else {
					portStrs = append(portStrs, fmt.Sprintf("%d->%d/%s", p.PublicPort, p.PrivatePort, p.Type))
				}
			} else if p.PrivatePort > 0 {
				portStrs = append(portStrs, fmt.Sprintf("%d/%s", p.PrivatePort, p.Type))
			}
		}
		out = append(out, ComposePsRow{
			Name:       name,
			Service:    service,
			State:      c.State,
			Status:     c.Status,
			Image:      c.Image,
			Ports:      strings.Join(portStrs, ", "),
			WorkingDir: c.Labels["com.docker.compose.project.working_dir"],
		})
	}
	return out, nil
}

func ContainerComposeLabels(ctx context.Context, containerName string) (project, workingDir string, err error) {
	containerName = strings.TrimSpace(containerName)
	if containerName == "" {
		return "", "", fmt.Errorf("empty container name")
	}
	cli, err := apiClient()
	if err != nil {
		return "", "", err
	}

	insp, err := cli.ContainerInspect(ctx, containerName)
	if err != nil {
		return "", "", err
	}
	if insp.Config == nil || insp.Config.Labels == nil {
		return "", "", nil
	}
	return insp.Config.Labels[composeProjectLabel], insp.Config.Labels["com.docker.compose.project.working_dir"], nil
}

// ContainerReadinessInfo returns state, health status, IP address, and exposed TCP ports for readiness probing.
type ContainerReadinessInfo struct {
	Running      bool
	Status       string
	ExitCode     int
	Health       string
	IPAddress    string
	ExposedPorts []int
}

// InspectContainerReadiness retrieves live status, network address, and exposed ports for a container.
func InspectContainerReadiness(ctx context.Context, containerName string) (ContainerReadinessInfo, error) {
	containerName = strings.TrimSpace(containerName)
	if containerName == "" {
		return ContainerReadinessInfo{}, fmt.Errorf("empty container name")
	}

	cli, err := apiClient()
	if err == nil {
		insp, err := cli.ContainerInspect(ctx, containerName)
		if err == nil {
			info := ContainerReadinessInfo{
				Running: insp.State.Running,
				Status:  insp.State.Status,
			}
			if insp.State != nil {
				info.ExitCode = insp.State.ExitCode
				if insp.State.Health != nil {
					info.Health = insp.State.Health.Status
				}
			}
			if insp.Config != nil && len(insp.Config.ExposedPorts) > 0 {
				seen := make(map[int]bool)
				for p := range insp.Config.ExposedPorts {
					portNum := p.Int()
					if portNum > 0 && !seen[portNum] {
						seen[portNum] = true
						info.ExposedPorts = append(info.ExposedPorts, portNum)
					}
				}
				sort.Ints(info.ExposedPorts)
			}
			if insp.NetworkSettings != nil {
				if len(info.ExposedPorts) == 0 && len(insp.NetworkSettings.Ports) > 0 {
					seen := make(map[int]bool)
					for p := range insp.NetworkSettings.Ports {
						portNum := p.Int()
						if portNum > 0 && !seen[portNum] {
							seen[portNum] = true
							info.ExposedPorts = append(info.ExposedPorts, portNum)
						}
					}
					sort.Ints(info.ExposedPorts)
				}
				if insp.NetworkSettings.IPAddress != "" {
					info.IPAddress = insp.NetworkSettings.IPAddress
				}
				for _, netw := range insp.NetworkSettings.Networks {
					if netw != nil && netw.IPAddress != "" {
						info.IPAddress = netw.IPAddress
						break
					}
				}
			}
			return info, nil
		}
	}

	// CLI fallback: docker inspect
	out, err := exec.CommandContext(ctx, "docker", "inspect", "--format", `{{json .State}}`, containerName).Output()
	if err != nil {
		return ContainerReadinessInfo{}, err
	}
	var state struct {
		Running  bool   `json:"Running"`
		Status   string `json:"Status"`
		ExitCode int    `json:"ExitCode"`
		Health   struct {
			Status string `json:"Status"`
		} `json:"Health"`
	}
	if err := json.Unmarshal(out, &state); err != nil {
		return ContainerReadinessInfo{}, err
	}

	ipOut, _ := exec.CommandContext(ctx, "docker", "inspect", "--format", `{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}`, containerName).Output()
	ipAddr := strings.TrimSpace(string(ipOut))

	return ContainerReadinessInfo{
		Running:   state.Running,
		Status:    state.Status,
		ExitCode:  state.ExitCode,
		Health:    state.Health.Status,
		IPAddress: ipAddr,
	}, nil
}

// ListContainerUsageMatching returns live stats for the containers keep() accepts.
// keep receives the container name (without the leading slash) and its compose project.
func ListContainerUsageMatching(ctx context.Context, keep func(name, project string) bool) ([]ContainerUsageRow, string) {
	return listContainerUsageFiltered(ctx, func(c types.Container) bool {
		return keep(strings.TrimPrefix(containerName(c), "/"), c.Labels[composeProjectLabel])
	})
}

var (
	serverVerMu sync.Mutex
	serverVerAt time.Time
	serverVer   string
)

// ServerVersion returns the Docker engine version ("27.3.1"), cached for five minutes.
func ServerVersion(ctx context.Context) string {
	serverVerMu.Lock()
	defer serverVerMu.Unlock()
	if serverVer != "" && time.Since(serverVerAt) < 5*time.Minute {
		return serverVer
	}
	cli, err := apiClient()
	if err != nil {
		return serverVer
	}
	v, err := cli.ServerVersion(ctx)
	if err != nil {
		return serverVer
	}
	serverVer, serverVerAt = v.Version, time.Now()
	return serverVer
}
