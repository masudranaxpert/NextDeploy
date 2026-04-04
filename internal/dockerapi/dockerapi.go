package dockerapi

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

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
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		ports := formatPorts(c.Ports)
		out = append(out, ContainerRow{
			ID:      c.ID[:12],
			Name:    name,
			Image:   c.Image,
			State:   c.State,
			Status:  c.Status,
			Ports:   ports,
			Created: time.Unix(c.Created, 0).UTC(),
		})
	}
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

// RemoveAppImages removes images whose repo tags reference projectID (compose-built names like <id>-service).
func RemoveAppImages(ctx context.Context, projectID string) []string {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil
	}
	alt := strings.ReplaceAll(projectID, "-", "_")
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
			if strings.Contains(t, projectID) || strings.Contains(t, alt) {
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

// RemoveAppContainers force-removes any container whose name contains projectID (orphans after failed compose).
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
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		if !strings.Contains(name, projectID) {
			continue
		}
		if err := cli.ContainerRemove(ctx, c.ID, types.ContainerRemoveOptions{Force: true, RemoveVolumes: false}); err != nil {
			errs = append(errs, name+": "+err.Error())
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
	fl := filters.NewArgs(filters.Arg("label", "com.docker.compose.project="+projectID))
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
