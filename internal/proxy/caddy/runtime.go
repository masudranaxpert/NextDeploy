package caddy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	icaddy "panel/internal/caddy"
	"panel/internal/dockerapi"
	"panel/internal/proxy"
)

const (
	defaultLogTailLines = 300
	maxLogTailLines     = 2000
)

// ContainerInspectResult captures container runtime state inspected from Docker.
type ContainerInspectResult struct {
	Exists  bool
	Running bool
	Status  string
	Health  string
}

// RuntimeHooks holds injectable runtime dependencies for Status, Logs, Restart,
// Validate, and Apply so unit tests can mock Docker and HTTP endpoints cleanly.
type RuntimeHooks struct {
	AdminStatus    func(ctx context.Context, adminURL string) (bool, string)
	AdminConfigGet func(ctx context.Context, adminURL string) (string, error)
	Inspect        func(ctx context.Context, container string) (ContainerInspectResult, error)
	Logs           func(ctx context.Context, container string, tail int) (string, error)
	Restart        func(ctx context.Context, container string) error
	EnsureNetwork  func(ctx context.Context) error
	ExecVersion    func(ctx context.Context, container string) (string, error)
	Apply          func(ctx context.Context, files proxy.Files) (proxy.ApplyResult, error)
	Validate       func(ctx context.Context, stagedDir string) error
}

// Status queries the Caddy admin API and inspects the Caddy container to build
// a proxy.Status snapshot.
func (d *Driver) Status(ctx context.Context) (proxy.Status, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return proxy.Status{Driver: DriverName, ContainerName: d.ContainerName()}, err
	}

	name := d.ContainerName()
	st := proxy.Status{
		Driver:        DriverName,
		ContainerName: name,
		Experimental:  false,
	}

	adminURL := d.effectiveAdminURL()
	info, inspectErr := d.inspectContainer(ctx, name)
	inspectOK := inspectErr == nil

	adminStatusFn := icaddy.AdminStatus
	if d != nil && d.hooks.AdminStatus != nil {
		adminStatusFn = d.hooks.AdminStatus
	}
	adminUp, adminMsg := adminStatusFn(ctx, adminURL)

	if strings.EqualFold(adminURL, "off") {
		if inspectOK {
			st.Running = info.Running
			if info.Running {
				st.Detail = adminMsg
			} else {
				st.Detail = info.Status
				st.LastError = "container is not running"
			}
		} else {
			st.Running = false
			st.LastError = inspectErr.Error()
		}
	} else if adminUp {
		st.Running = !inspectOK || info.Running
		st.Detail = "admin API reachable"
		st.RawConfig = d.resolveRawConfig(ctx, adminURL, adminMsg)
		if inspectOK && !info.Running {
			st.LastError = fmt.Sprintf("container %s is %s", name, info.Status)
		}
	} else {
		st.Running = inspectOK && info.Running
		if inspectOK && info.Status != "" {
			st.Detail = info.Status
		}
		if adminMsg != "" {
			st.LastError = adminMsg
		} else if inspectErr != nil {
			st.LastError = inspectErr.Error()
		}
	}

	if st.Running && d != nil && d.hooks.ExecVersion != nil {
		if ver, err := d.hooks.ExecVersion(ctx, name); err == nil {
			st.Version = strings.TrimSpace(ver)
		}
	}
	return st, nil
}

// Logs returns up to the most recent lines from the Caddy container output.
func (d *Driver) Logs(ctx context.Context, lines int) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
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

// Restart restarts the Caddy container and ensures it stays connected to the panel network.
func (d *Driver) Restart(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	restartFn := dockerapi.RestartContainerByName
	if d != nil && d.hooks.Restart != nil {
		restartFn = d.hooks.Restart
	}
	if err := restartFn(ctx, d.ContainerName()); err != nil {
		return err
	}

	ensureNetFn := dockerapi.EnsureCaddySharedNetwork
	if d != nil && d.hooks.EnsureNetwork != nil {
		ensureNetFn = d.hooks.EnsureNetwork
	}
	if ensureNetFn != nil {
		_ = ensureNetFn(ctx)
	}
	return nil
}

func (d *Driver) effectiveAdminURL() string {
	if d != nil && strings.TrimSpace(d.adminURL) != "" {
		return strings.TrimSpace(d.adminURL)
	}
	return icaddy.DefaultAdminAPI
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

func (d *Driver) resolveRawConfig(ctx context.Context, adminURL, adminMsg string) string {
	if d != nil && d.hooks.AdminConfigGet != nil {
		if cfg, err := d.hooks.AdminConfigGet(ctx, adminURL); err == nil {
			return cfg
		}
	}
	var buf bytes.Buffer
	if json.Indent(&buf, []byte(adminMsg), "", "  ") == nil {
		return buf.String()
	}
	return adminMsg
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
