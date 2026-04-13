package dockerapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/pkg/stdcopy"
)

const composeServiceLabel = "com.docker.compose.service"

// ContainerIDForComposeService returns a container ID for the given compose project and service name.
func ContainerIDForComposeService(ctx context.Context, project, service string) (string, error) {
	project = strings.TrimSpace(project)
	service = strings.TrimSpace(service)
	if project == "" || service == "" {
		return "", fmt.Errorf("empty project or service")
	}
	cli, err := newAPIClient()
	if err != nil {
		return "", err
	}
	defer cli.Close()

	fl := filters.NewArgs(
		filters.Arg("label", composeProjectLabel+"="+project),
		filters.Arg("label", composeServiceLabel+"="+service),
	)
	list, err := cli.ContainerList(ctx, types.ContainerListOptions{Filters: fl, All: true})
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "", fmt.Errorf("no container for project %q service %q", project, service)
	}
	sort.Slice(list, func(i, j int) bool {
		runningI := strings.EqualFold(list[i].State, "running")
		runningJ := strings.EqualFold(list[j].State, "running")
		if runningI != runningJ {
			return runningI
		}
		return list[i].Created > list[j].Created
	})
	return list[0].ID, nil
}

func tailClampString(n int) string {
	if n <= 0 {
		n = 300
	}
	if n > 10000 {
		n = 10000
	}
	return strconv.Itoa(n)
}

// FetchContainerLogsText returns the last tailLines of combined stdout/stderr (demuxed).
func FetchContainerLogsText(ctx context.Context, containerRef string, tailLines int) (string, error) {
	containerRef = strings.TrimSpace(containerRef)
	if containerRef == "" {
		return "", fmt.Errorf("empty container reference")
	}
	cli, err := newAPIClient()
	if err != nil {
		return "", err
	}
	defer cli.Close()

	raw, err := cli.ContainerLogs(ctx, containerRef, types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: true,
		Follow:     false,
		Tail:       tailClampString(tailLines),
	})
	if err != nil {
		return "", err
	}
	defer raw.Close()

	var buf bytes.Buffer
	if _, err = stdcopy.StdCopy(&buf, &buf, raw); err != nil && !errors.Is(err, io.EOF) {
		return buf.String(), err
	}
	return buf.String(), nil
}

// demuxedFollowCloser streams live logs from the Docker API with stdcopy demux (stdout+stderr merged in frame order).
type demuxedFollowCloser struct {
	pr          *io.PipeReader
	raw         io.ReadCloser
	shutdownRaw sync.Once
	wg          sync.WaitGroup
}

func (d *demuxedFollowCloser) Read(p []byte) (int, error) {
	return d.pr.Read(p)
}

func (d *demuxedFollowCloser) Close() error {
	d.shutdownRaw.Do(func() { _ = d.raw.Close() })
	d.wg.Wait()
	_ = d.pr.Close()
	return nil
}

func (d *demuxedFollowCloser) endRaw() {
	d.shutdownRaw.Do(func() { _ = d.raw.Close() })
}

// FollowContainerLogsDemuxed opens a follow log stream on the Docker Engine (no docker CLI / shell).
// tail is passed through to the API (use "0" to mirror `docker logs -f --tail 0` live-only behavior on supported engines).
func FollowContainerLogsDemuxed(ctx context.Context, containerRef string, tail string) (io.ReadCloser, error) {
	containerRef = strings.TrimSpace(containerRef)
	if containerRef == "" {
		return nil, fmt.Errorf("empty container reference")
	}
	if strings.TrimSpace(tail) == "" {
		tail = "0"
	}
	cli, err := newAPIClient()
	if err != nil {
		return nil, err
	}
	raw, err := cli.ContainerLogs(ctx, containerRef, types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: true,
		Follow:     true,
		Tail:       tail,
	})
	if err != nil {
		_ = cli.Close()
		return nil, err
	}

	pr, pw := io.Pipe()
	d := &demuxedFollowCloser{pr: pr, raw: raw}
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		defer func() { _ = cli.Close() }()
		_, copyErr := stdcopy.StdCopy(pw, pw, raw)
		d.endRaw()
		if copyErr != nil && !errors.Is(copyErr, io.EOF) {
			_ = pw.CloseWithError(copyErr)
			return
		}
		_ = pw.Close()
	}()
	return d, nil
}
