package dockerapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

const composeProjectLabel = "com.docker.compose.project"

func newAPIClient() (*client.Client, error) {
	ver := strings.TrimSpace(os.Getenv("DOCKER_API_VERSION"))
	opts := []client.Opt{
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	}
	if ver != "" {
		opts = append(opts, client.WithVersion(ver))
	} else {
		// Newer Docker Engines reject API < 1.44; pin a safe floor when env is unset.
		opts = append(opts, client.WithVersion("1.45"))
	}
	return client.NewClientWithOpts(opts...)
}

type ContainerRow struct {
	ID      string
	Name    string
	Image   string
	State   string
	Status  string
	Ports   string
	Created time.Time
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
	ID            string
	Name          string
	Image         string
	State         string
	Status        string
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
	cli, err := newAPIClient()
	if err != nil {
		return nil, err.Error()
	}
	defer cli.Close()
	list, err := cli.ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		return nil, err.Error()
	}
	out := make([]ContainerRow, 0, len(list))
	for _, c := range list {
		ports := formatPorts(c.Ports)
		out = append(out, ContainerRow{
			ID:      c.ID[:12],
			Name:    containerName(c),
			Image:   c.Image,
			State:   c.State,
			Status:  c.Status,
			Ports:   ports,
			Created: time.Unix(c.Created, 0).UTC(),
		})
	}
	return out, ""
}

// ContainerComposeProjectAndMountSource returns the compose project label and the
// host source path for a specific mount destination on a container.
func ContainerComposeProjectAndMountSource(ctx context.Context, containerName, mountDestination string) (project, source string, err error) {
	cli, err := newAPIClient()
	if err != nil {
		return "", "", err
	}
	defer cli.Close()

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
	cli, err := newAPIClient()
	if err != nil {
		return nil, err.Error()
	}
	defer cli.Close()

	list, err := cli.ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		return nil, err.Error()
	}

	out := make([]ContainerUsageRow, 0, len(list))
	for _, c := range list {
		row := ContainerUsageRow{
			ID:     c.ID[:12],
			Name:   containerName(c),
			Image:  c.Image,
			State:  c.State,
			Status: c.Status,
		}

		stats, err := cli.ContainerStatsOneShot(ctx, c.ID)
		if err == nil {
			var payload statsJSON
			body, readErr := io.ReadAll(stats.Body)
			_ = stats.Body.Close()
			if readErr == nil && json.Unmarshal(body, &payload) == nil {
				row.CPUPercent = calculateCPUPercent(payload)
				memUsage := payload.MemoryStats.Usage
				if cache := payload.MemoryStats.Stats["cache"]; cache > 0 && memUsage > cache {
					memUsage -= cache
				}
				row.MemUsage = memUsage
				row.MemLimit = payload.MemoryStats.Limit
				if row.MemLimit > 0 {
					row.MemPercent = (float64(row.MemUsage) / float64(row.MemLimit)) * 100
				}
				row.MemUsageHuman = formatBytes(int64(row.MemUsage))
				row.MemLimitHuman = formatBytes(int64(row.MemLimit))
				for _, net := range payload.Networks {
					row.NetInput += net.RxBytes
					row.NetOutput += net.TxBytes
				}
				for _, entry := range payload.BlkioStats.IoServiceBytesRecursive {
					switch strings.ToLower(strings.TrimSpace(entry.Op)) {
					case "read":
						row.BlockRead += entry.Value
					case "write":
						row.BlockWrite += entry.Value
					}
				}
				row.Pids = payload.PidsStats.Current
			}
		}

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

func ListImages(ctx context.Context) ([]ImageRow, string) {
	cli, err := newAPIClient()
	if err != nil {
		return nil, err.Error()
	}
	defer cli.Close()
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

func calculateCPUPercent(s statsJSON) float64 {
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(s.CPUStats.SystemUsage) - float64(s.PreCPUStats.SystemUsage)
	if cpuDelta <= 0 || systemDelta <= 0 {
		return 0
	}
	online := float64(s.CPUStats.OnlineCPUs)
	if online <= 0 {
		online = float64(len(s.CPUStats.CPUUsage.PercpuUsage))
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

// removeContainersUsingImage force-removes any container (running or stopped) that uses the given image ID.
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

// imageTagLooksOwnedByComposeProject matches compose-built local names without substring false positives:
// project "a" must not match image "a-longer-name:1".
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
	// Safe prefixes: project_service, project-service (only when next rune is _ so "proj_extra" differs from "proj-extra")
	if strings.HasPrefix(repo, projectID+"_") || strings.HasPrefix(repo, alt+"_") {
		return true
	}
	return false
}

// RemoveAppImages removes images whose repo tags clearly belong to this compose project.
// Avoids strings.Contains(projectID) which matches longer project names and unrelated images.
func RemoveAppImages(ctx context.Context, projectID string) []string {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil
	}
	cli, err := newAPIClient()
	if err != nil {
		return []string{err.Error()}
	}
	defer cli.Close()
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

// RemoveContainersByComposeProject force-removes containers with com.docker.compose.project=<projectID>.
// Custom container_name stacks often need this because name-based removal does not see the project string.
func RemoveContainersByComposeProject(ctx context.Context, projectID string) []string {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil
	}
	cli, err := newAPIClient()
	if err != nil {
		return []string{err.Error()}
	}
	defer cli.Close()
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

// RemoveAppContainers removes containers that belong to projectID.
// Never uses strings.Contains on the container name: a project "ma-masud" must not match
// "ma-masud-now-..." (another app's compose project) or unrelated names like the panel container.
func RemoveAppContainers(ctx context.Context, projectID string) []string {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil
	}
	cli, err := newAPIClient()
	if err != nil {
		return []string{err.Error()}
	}
	defer cli.Close()
	list, err := cli.ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		return []string{err.Error()}
	}
	var errs []string
	for _, c := range list {
		name := containerName(c)
		insp, err := cli.ContainerInspect(ctx, c.ID)
		if err != nil {
			continue
		}
		lbl := strings.TrimSpace(insp.Config.Labels[composeProjectLabel])
		if lbl == projectID {
			if err := cli.ContainerRemove(ctx, c.ID, types.ContainerRemoveOptions{Force: true, RemoveVolumes: false}); err != nil {
				errs = append(errs, name+": "+err.Error())
			}
			continue
		}
		if lbl != "" {
			continue
		}
		// Unlabeled orphan: only safe patterns (underscore form). Hyphen form "proj-svc-1" is
		// ambiguous when another project is "proj-extra" (prefix + hyphen).
		n := strings.TrimPrefix(name, "/")
		if n == projectID || strings.HasPrefix(n, projectID+"_") {
			if err := cli.ContainerRemove(ctx, c.ID, types.ContainerRemoveOptions{Force: true, RemoveVolumes: false}); err != nil {
				errs = append(errs, name+": "+err.Error())
			}
		}
	}
	return errs
}

// RemoveAppNetworks removes compose-managed networks for this project (label com.docker.compose.project).
func RemoveAppNetworks(ctx context.Context, projectID string) []string {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil
	}
	cli, err := newAPIClient()
	if err != nil {
		return []string{err.Error()}
	}
	defer cli.Close()
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
	cli, err := newAPIClient()
	if err != nil {
		return "", err
	}
	defer cli.Close()
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
	cli, err := newAPIClient()
	if err != nil {
		return err
	}
	defer cli.Close()
	return cli.ContainerStart(ctx, id, types.ContainerStartOptions{})
}

func StopContainerByName(ctx context.Context, name string) error {
	id, err := containerIDByName(ctx, name)
	if err != nil {
		return err
	}
	cli, err := newAPIClient()
	if err != nil {
		return err
	}
	defer cli.Close()
	timeout := 10
	return cli.ContainerStop(ctx, id, container.StopOptions{Timeout: &timeout})
}

func RestartContainerByName(ctx context.Context, name string) error {
	id, err := containerIDByName(ctx, name)
	if err != nil {
		return err
	}
	cli, err := newAPIClient()
	if err != nil {
		return err
	}
	defer cli.Close()
	timeout := 10
	return cli.ContainerRestart(ctx, id, container.StopOptions{Timeout: &timeout})
}

func RemoveContainerByName(ctx context.Context, name string) error {
	id, err := containerIDByName(ctx, name)
	if err != nil {
		return err
	}
	cli, err := newAPIClient()
	if err != nil {
		return err
	}
	defer cli.Close()
	return cli.ContainerRemove(ctx, id, types.ContainerRemoveOptions{Force: true, RemoveVolumes: false})
}

func RemoveImageByID(ctx context.Context, imageID string) error {
	cli, err := newAPIClient()
	if err != nil {
		return err
	}
	defer cli.Close()
	_, err = cli.ImageRemove(ctx, imageID, types.ImageRemoveOptions{Force: false, PruneChildren: true})
	return err
}

func RemoveVolumeByName(ctx context.Context, name string) error {
	cli, err := newAPIClient()
	if err != nil {
		return err
	}
	defer cli.Close()
	return cli.VolumeRemove(ctx, name, false)
}

// PruneImages removes all unused (dangling) images.
func PruneImages(ctx context.Context) error {
	cli, err := newAPIClient()
	if err != nil {
		return err
	}
	defer cli.Close()
	_, err = cli.ImagesPrune(ctx, filters.Args{})
	return err
}

// PruneContainers removes all stopped containers.
func PruneContainers(ctx context.Context) error {
	cli, err := newAPIClient()
	if err != nil {
		return err
	}
	defer cli.Close()
	_, err = cli.ContainersPrune(ctx, filters.Args{})
	return err
}

func containerName(c types.Container) string {
	if len(c.Names) > 0 {
		return strings.TrimPrefix(c.Names[0], "/")
	}
	return ""
}
