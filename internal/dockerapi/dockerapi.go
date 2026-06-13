package dockerapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/shirou/gopsutil/v3/mem"
	"golang.org/x/sync/errgroup"

	"panel/internal/resmatch"
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
		opts := []client.Opt{
			client.FromEnv,
			client.WithAPIVersionNegotiation(),
		}
		if ver != "" {
			opts = append(opts, client.WithVersion(ver))
		} else {
			opts = append(opts, client.WithVersion("1.54"))
		}
		apiClientInst, apiClientErr = client.NewClientWithOpts(opts...)
	})
	return apiClientInst, apiClientErr
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
	ID             string
	Name           string
	Image          string
	State          string
	Status         string
	ComposeProject string
	CPUPercent    float64
	MemUsage      uint64
	MemLimit      uint64
	MemPercent    float64
	NetInput      uint64
	NetOutput     uint64
	BlockRead     uint64
	BlockWrite    uint64
	Pids          uint64
	MemUsageHuman string
	MemLimitHuman string
	NetInputHuman  string
	NetOutputHuman string
	BlockReadHuman string
	BlockWriteHuman string
	NetDLRateHuman string
	NetULRateHuman string
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
			TotalUsage uint64 `json:"total_usage"`
			PercpuUsage []uint64 `json:"percpu_usage"`
		} `json:"cpu_usage"`
		SystemUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs  uint32 `json:"online_cpus"`
	} `json:"cpu_stats"`
	MemoryStats struct {
		Usage uint64 `json:"usage"`
		Limit uint64 `json:"limit"`
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

func ListContainers(ctx context.Context) ([]ContainerRow, string) {
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
		})
	}
	return out, ""
}

type ComposeContainerRow struct {
	Name       string
	State      string
	Status     string
	Project    string
	WorkingDir string
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

	statsMap := fetchStatsSnapshot(ctx, cli, ids)

	out := make([]ContainerUsageRow, 0, len(list))
	for _, c := range list {
		row := ContainerUsageRow{
			ID:             c.ID[:12],
			Name:           containerName(c),
			Image:          c.Image,
			State:          c.State,
			Status:         c.Status,
			ComposeProject: c.Labels[composeProjectLabel],
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
		row.CPUPercent = cpuPercentSingleSample(cur)
		memUsage := memoryUsageLikeDockerCLI(cur)
		row.MemUsage = memUsage
		row.MemLimit = cur.MemoryStats.Limit
		if row.MemLimit > 0 {
			row.MemPercent = (float64(row.MemUsage) / float64(row.MemLimit)) * 100
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
	cli, err := apiClient()
	if err != nil {
		return nil, err.Error()
	}

	list, err := cli.ImageList(ctx, types.ImageListOptions{All: true})
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
	return out, ""
}

func formatBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	u := float64(n)
	const kb = 1024
	if u < kb*kb {
		return fmt.Sprintf("%.1f KB", u/kb)
	}
	if u < kb*kb*kb {
		return fmt.Sprintf("%.1f MB", u/(kb*kb))
	}
	return fmt.Sprintf("%.2f GB", u/(kb*kb*kb))
}

func HumanBytes(n uint64) string {
	return formatBytes(int64(n))
}

func fetchContainerStatsJSON(ctx context.Context, cli *client.Client, id string) (statsJSON, error) {
	var out statsJSON
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	stats, err := cli.ContainerStatsOneShot(cctx, id)
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

func fetchStatsSnapshot(ctx context.Context, cli *client.Client, ids []string) map[string]statsJSON {
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
			j, err := fetchContainerStatsJSON(gctx, cli, id)
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
			if strings.TrimPrefix(n, "/") == name {
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
		out = append(out, ComposePsRow{
			Name:       name,
			Service:    service,
			State:      c.State,
			Status:     c.Status,
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
