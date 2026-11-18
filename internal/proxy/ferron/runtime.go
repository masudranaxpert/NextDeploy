package ferron

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"panel/internal/dockerapi"
	"panel/internal/proxy"
)

// RuntimeHooks overrides container/CLI/API operations for Docker-free unit tests.
type RuntimeHooks struct {
	ValidateFunc         func(ctx context.Context, stagedDir string) error
	AdminRequestFunc     func(ctx context.Context, method, path string) (int, []byte, error)
	InspectContainerFunc func(ctx context.Context, container string) (dockerapi.ContainerReadinessInfo, error)
	FetchLogsFunc        func(ctx context.Context, container string, lines int) (string, error)
	RestartFunc          func(ctx context.Context, container string) error
}

type validateJSONReport struct {
	Valid       bool                 `json:"valid"`
	Diagnostics []validateDiagnostic `json:"diagnostics"`
}

type validateDiagnostic struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Span    *struct {
		Line   int    `json:"line"`
		Column int    `json:"column"`
		File   string `json:"file"`
	} `json:"span"`
}

// Validate runs `ferron validate --json --config-adapter ferronconf` inside an
// ephemeral Ferron container mounting stagedDir read-only at /etc/ferron/nd.
func (d *Driver) Validate(ctx context.Context, stagedDir string) error {
	ctx = contextOrBackground(ctx)
	if d.hooks.ValidateFunc != nil {
		return d.hooks.ValidateFunc(ctx, stagedDir)
	}
	dir := strings.TrimSpace(stagedDir)
	if dir == "" {
		return fmt.Errorf("ferron validate: staged directory is required")
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("ferron validate: resolve %q: %w", dir, err)
	}
	confPath := filepath.Join(absDir, "ferron.conf")
	if _, err := os.Stat(confPath); err != nil {
		return fmt.Errorf("ferron validate: missing %s: %w", confPath, err)
	}

	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--quiet",
		"-v", d.toHost(absDir)+":"+containerConfigRoot+"/sites:ro",
		d.image,
		"/usr/local/bin/ferron", "validate",
		"-c", containerLiveConfig,
		"--json",
		"--config-adapter", "ferronconf",
	)
	out, cmdErr := cmd.CombinedOutput()
	return parseValidateOutput(out, cmdErr)
}

func parseValidateOutput(raw []byte, cmdErr error) error {
	trimmed := strings.TrimSpace(string(raw))
	var report validateJSONReport
	if jsonStart := strings.IndexByte(trimmed, '{'); jsonStart >= 0 {
		if err := json.Unmarshal([]byte(trimmed[jsonStart:]), &report); err == nil {
			if report.Valid && cmdErr == nil {
				return nil
			}
			if !report.Valid {
				var msgs []string
				for _, diag := range report.Diagnostics {
					loc := ""
					if diag.Span != nil {
						loc = fmt.Sprintf(" (%s:%d:%d)", diag.Span.File, diag.Span.Line, diag.Span.Column)
					}
					msgs = append(msgs, strings.TrimSpace(diag.Kind+": "+diag.Message+loc))
				}
				if len(msgs) > 0 {
					return fmt.Errorf("ferron validate failed: %s", strings.Join(msgs, "; "))
				}
				return fmt.Errorf("ferron validate reported invalid configuration")
			}
		}
	}
	if cmdErr != nil {
		if trimmed != "" {
			return fmt.Errorf("ferron validate failed: %w: %s", cmdErr, trimmed)
		}
		return fmt.Errorf("ferron validate failed: %w", cmdErr)
	}
	return nil
}

// Apply stages files into an isolated generation directory, validates via ferron
// validate, atomically swaps the live relative symlinks, triggers POST /reload,
// verifies GET /reload generation increment and zero reload error, and rolls
// back automatically if validation or reload fails.
func (d *Driver) Apply(ctx context.Context, files proxy.Files) (proxy.ApplyResult, error) {
	ctx = contextOrBackground(ctx)
	start := time.Now()
	if len(files) == 0 {
		return proxy.ApplyResult{Driver: DriverName, AppliedAt: time.Now().UTC()}, fmt.Errorf("ferron apply: empty file set")
	}

	gen := filesGeneration(files)
	store, err := d.resolveStageStore()
	if err != nil {
		return proxy.ApplyResult{Generation: gen, Driver: DriverName, AppliedAt: time.Now().UTC()}, err
	}

	beforeReload, beforeErr := d.getReloadStatus(ctx)
	hasBefore := beforeErr == nil

	stagedDir, targetBase, err := store.Stage(gen, files)
	if err != nil {
		return proxy.ApplyResult{Generation: gen, Driver: DriverName, Changed: true, Duration: time.Since(start), AppliedAt: time.Now().UTC()}, err
	}
	if err := d.Validate(ctx, stagedDir); err != nil {
		store.RemoveStaged(stagedDir)
		return proxy.ApplyResult{Generation: gen, Driver: DriverName, Changed: true, Duration: time.Since(start), AppliedAt: time.Now().UTC()}, err
	}

	prevTarget, err := store.Activate(targetBase)
	if err != nil {
		store.RemoveStaged(stagedDir)
		return proxy.ApplyResult{Generation: gen, Driver: DriverName, Changed: true, Duration: time.Since(start), AppliedAt: time.Now().UTC()}, err
	}

	if err := d.reloadAndVerify(ctx, hasBefore, beforeReload.ActiveGeneration); err != nil {
		rbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		rbErr := store.Rollback(prevTarget)
		if prevTarget != "" && rbErr == nil {
			_ = d.triggerReload(rbCtx)
		}
		return proxy.ApplyResult{
			Generation: gen, Driver: DriverName, Changed: true, Reloaded: false, RolledBack: rbErr == nil,
			Duration: time.Since(start), AppliedAt: time.Now().UTC(),
		}, err
	}

	_ = store.Prune(targetBase)
	return proxy.ApplyResult{
		Generation: gen,
		Driver:     DriverName,
		Changed:    true,
		Reloaded:   true,
		Duration:   time.Since(start),
		Message:    "reloaded",
		AppliedAt:  time.Now().UTC(),
	}, nil
}

// Status queries Ferron's Admin API (/health, /status, /reload) and container readiness.
func (d *Driver) Status(ctx context.Context) (proxy.Status, error) {
	ctx = contextOrBackground(ctx)
	st := proxy.Status{
		Driver:        DriverName,
		ContainerName: d.ContainerName(),
		Version:       adminURLTagVersion(d.image),
		Experimental:  true,
	}

	var errs []string
	if d.hooks.InspectContainerFunc != nil {
		info, err := d.hooks.InspectContainerFunc(ctx, st.ContainerName)
		if err != nil {
			errs = append(errs, "inspect: "+err.Error())
		} else if info.Running {
			st.Running = true
		}
	} else if info, err := dockerapi.InspectContainerReadiness(ctx, st.ContainerName); err == nil {
		st.Running = info.Running
	}

	if healthy, err := d.getHealth(ctx); err != nil {
		errs = append(errs, "health: "+err.Error())
	} else {
		st.Running = healthy
	}

	if sResp, err := d.getStatus(ctx); err != nil {
		errs = append(errs, "status: "+err.Error())
	} else {
		st.Connections = sResp.ConnectionsActive
		st.Detail = fmt.Sprintf("uptime=%ds requests=%d reloads=%d", sResp.UptimeSec, sResp.RequestsTotal, sResp.Reloads)
	}

	if rResp, err := d.getReloadStatus(ctx); err != nil {
		errs = append(errs, "reload_status: "+err.Error())
	} else {
		st.Generation = strconv.FormatUint(rResp.ActiveGeneration, 10)
		if ts, perr := time.Parse(time.RFC3339, strings.TrimSpace(rResp.LastReloadTime)); perr == nil && ts.Year() > 1970 {
			st.LastReloadAt = &ts
		}
		if rResp.LastReloadError != nil && strings.TrimSpace(*rResp.LastReloadError) != "" {
			st.LastError = strings.TrimSpace(*rResp.LastReloadError)
		}
	}

	if len(errs) > 0 && !st.Running {
		return st, fmt.Errorf("ferron status: %s", strings.Join(errs, "; "))
	}
	return st, nil
}

// Logs fetches the trailing lines from the managed Ferron container logs.
func (d *Driver) Logs(ctx context.Context, lines int) ([]string, error) {
	ctx = contextOrBackground(ctx)
	n := clampLines(lines)
	var text string
	var err error
	if d.hooks.FetchLogsFunc != nil {
		text, err = d.hooks.FetchLogsFunc(ctx, d.ContainerName(), n)
	} else {
		text, err = dockerapi.FetchContainerLogsText(ctx, d.ContainerName(), n)
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return []string{}, nil
	}
	raw := strings.Split(text, "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimRight(line, "\r")
		if line != "" {
			out = append(out, line)
		}
	}
	return out, nil
}

// Restart restarts the managed Ferron container.
func (d *Driver) Restart(ctx context.Context) error {
	ctx = contextOrBackground(ctx)
	if d.hooks.RestartFunc != nil {
		return d.hooks.RestartFunc(ctx, d.ContainerName())
	}
	return dockerapi.RestartContainerByName(ctx, d.ContainerName())
}

func (d *Driver) resolveStageStore() (*proxy.StageStore, error) {
	if d.stage != nil {
		return d.stage, nil
	}
	return proxy.NewStageStore(proxy.StageConfig{
		BaseDir:         d.dataDir,
		LinkName:        "sites",
		GenPrefix:       "sites.gen-",
		KeepGenerations: 3,
	})
}

func filesGeneration(files proxy.Files) string {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		_, _ = h.Write([]byte(k))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(files[k]))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
