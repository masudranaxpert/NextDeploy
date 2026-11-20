package angie

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"panel/internal/dockerapi"
	"panel/internal/proxy"
)

const (
	defaultLogTailLines = 300
	maxLogTailLines     = 2000
	statusHTTPTimeout   = 3 * time.Second
)

// ContainerInspectResult captures container runtime state inspected from Docker.
type ContainerInspectResult struct {
	Exists  bool
	Running bool
	Status  string
	Health  string
}

// RuntimeHooks holds injectable runtime dependencies for Validate, Apply,
// Status, Logs, and Restart so unit tests can execute without Docker.
type RuntimeHooks struct {
	Exec          func(ctx context.Context, container string, args ...string) (string, error)
	RunValidate   func(ctx context.Context, stagedDir string) error
	Reload        func(ctx context.Context, container string) error
	QueryStatus   func(ctx context.Context, container string) ([]byte, error)
	Inspect       func(ctx context.Context, container string) (ContainerInspectResult, error)
	Logs          func(ctx context.Context, container string, tail int) (string, error)
	Restart       func(ctx context.Context, container string) error
	EnsureNetwork func(ctx context.Context) error
	Apply         func(ctx context.Context, files proxy.Files) (proxy.ApplyResult, error)
}

type angieStatusPayload struct {
	Angie struct {
		Version    string `json:"version"`
		Generation int64  `json:"generation"`
		LoadTime   string `json:"load_time"`
	} `json:"angie"`
	Connections struct {
		Accepted int64 `json:"accepted"`
		Dropped  int64 `json:"dropped"`
		Active   int64 `json:"active"`
		Idle     int64 `json:"idle"`
	} `json:"connections"`
	HTTP struct {
		ServerZones map[string]struct {
			Requests struct {
				Total      int64 `json:"total"`
				Processing int64 `json:"processing"`
				Discarded  int64 `json:"discarded"`
			} `json:"requests"`
		} `json:"server_zones"`
	} `json:"http"`
}

// Status queries the Angie OSS /status/ JSON endpoint and inspects the proxy container.
func (d *Driver) Status(ctx context.Context) (proxy.Status, error) {
	ctx = contextOrBackground(ctx)
	name := d.ContainerName()
	if err := ctx.Err(); err != nil {
		return proxy.Status{Driver: DriverName, ContainerName: name}, err
	}

	st := proxy.Status{
		Driver:        DriverName,
		ContainerName: name,
		Experimental:  false,
	}

	info, inspectErr := d.inspectContainer(ctx, name)
	inspectOK := inspectErr == nil

	raw, statusErr := d.queryStatusPayload(ctx, name)
	if statusErr == nil {
		var parsed angieStatusPayload
		if jerr := json.Unmarshal(raw, &parsed); jerr != nil {
			statusErr = fmt.Errorf("parse angie status JSON: %w", jerr)
		} else {
			st.Running = !inspectOK || info.Running
			st.Version = strings.TrimSpace(parsed.Angie.Version)
			st.Connections = parsed.Connections.Active
			for _, sz := range parsed.HTTP.ServerZones {
				st.ActiveRequests += sz.Requests.Processing
			}
			if parsed.Angie.Generation > 0 {
				st.Generation = strconv.FormatInt(parsed.Angie.Generation, 10)
			}
			if parsed.Angie.LoadTime != "" {
				if t, terr := time.Parse(time.RFC3339Nano, parsed.Angie.LoadTime); terr == nil {
					utc := t.UTC()
					st.LastReloadAt = &utc
				}
			}
			var pretty bytes.Buffer
			if json.Indent(&pretty, raw, "", "  ") == nil {
				st.RawConfig = pretty.String()
			} else {
				st.RawConfig = string(raw)
			}
			st.Detail = fmt.Sprintf("angie %s (generation %d, %d server zones)",
				st.Version, parsed.Angie.Generation, len(parsed.HTTP.ServerZones))
			if inspectOK && !info.Running {
				st.LastError = fmt.Sprintf("container %s is %s", name, info.Status)
			}
			return st, nil
		}
	}

	st.Running = inspectOK && info.Running
	if inspectOK && info.Status != "" {
		st.Detail = info.Status
	}
	if statusErr != nil {
		st.LastError = statusErr.Error()
	} else if inspectErr != nil {
		st.LastError = inspectErr.Error()
	}
	return st, nil
}

// Logs returns up to the most recent lines from the Angie container output.
func (d *Driver) Logs(ctx context.Context, lines int) ([]string, error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if lines <= 0 {
		lines = defaultLogTailLines
	} else if lines > maxLogTailLines {
		lines = maxLogTailLines
	}

	logsFn := func(ctx context.Context, container string, tail int) (string, error) {
		return dockerapi.FetchContainerLogsText(ctx, container, tail)
	}
	if d != nil && d.hooks.Logs != nil {
		logsFn = d.hooks.Logs
	}
	raw, err := logsFn(ctx, d.ContainerName(), lines)
	if err != nil {
		return nil, err
	}
	return splitLogLines(raw), nil
}

// Restart restarts the Angie container and ensures it stays connected to the shared network.
func (d *Driver) Restart(ctx context.Context) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	name := d.ContainerName()
	restartFn := dockerapi.RestartContainerByName
	if d != nil && d.hooks.Restart != nil {
		restartFn = d.hooks.Restart
	}
	if err := restartFn(ctx, name); err != nil {
		return err
	}
	if d != nil && d.hooks.EnsureNetwork != nil {
		_ = d.hooks.EnsureNetwork(ctx)
	} else {
		netName := DefaultNetworkName
		if d != nil && strings.TrimSpace(d.networkName) != "" {
			netName = strings.TrimSpace(d.networkName)
		}
		_ = dockerapi.EnsureContainerNetwork(ctx, name, netName)
	}
	return nil
}

func (d *Driver) queryStatusPayload(ctx context.Context, container string) ([]byte, error) {
	if d != nil && d.hooks.QueryStatus != nil {
		return d.hooks.QueryStatus(ctx, container)
	}
	if d != nil && strings.TrimSpace(d.statusURL) != "" {
		client := &http.Client{Timeout: statusHTTPTimeout}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(d.statusURL), nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		body, rerr := io.ReadAll(resp.Body)
		if rerr != nil {
			return nil, rerr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("angie status HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return body, nil
	}
	out, err := d.execInContainer(ctx, container, "wget", "-qO-", containerStatusURL)
	if err != nil {
		return nil, fmt.Errorf("query angie status: %s (%w)", strings.TrimSpace(out), err)
	}
	return []byte(out), nil
}

func (d *Driver) execInContainer(ctx context.Context, container string, args ...string) (string, error) {
	if d != nil && d.hooks.Exec != nil {
		return d.hooks.Exec(ctx, container, args...)
	}
	cmdArgs := append([]string{"exec", container}, args...)
	out, err := exec.CommandContext(ctx, "docker", cmdArgs...).CombinedOutput()
	return string(out), err
}

func (d *Driver) inspectContainer(ctx context.Context, name string) (ContainerInspectResult, error) {
	if d != nil && d.hooks.Inspect != nil {
		return d.hooks.Inspect(ctx, name)
	}
	info, err := dockerapi.InspectContainerReadiness(ctx, name)
	if err != nil {
		return ContainerInspectResult{}, err
	}
	return ContainerInspectResult{
		Exists:  true,
		Running: info.Running,
		Status:  info.Status,
		Health:  info.Health,
	}, nil
}

func splitLogLines(s string) []string {
	if strings.TrimSpace(s) == "" {
		return []string{}
	}
	raw := strings.Split(s, "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimRight(line, "\r")
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

var _ proxy.RequestCounter = (*Driver)(nil)

// RequestsTotal sums the request counters of every server zone (one per site, plus the panel).
// The status API only listens on the container's loopback, so it is read from inside it.
func (d *Driver) RequestsTotal(ctx context.Context) (uint64, error) {
	ctx = contextOrBackground(ctx)
	raw, err := d.queryStatusPayload(ctx, d.ContainerName())
	if err != nil {
		return 0, err
	}
	var parsed angieStatusPayload
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return 0, fmt.Errorf("parse angie status JSON: %w", err)
	}
	var total uint64
	for _, sz := range parsed.HTTP.ServerZones {
		if sz.Requests.Total > 0 {
			total += uint64(sz.Requests.Total)
		}
	}
	return total, nil
}
