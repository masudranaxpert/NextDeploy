package edge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// AngieRunner implements Runner for Angie (`angie -t -c`, `systemctl reload` /
// `angie -s reload`, liveness checks, and `angie -v`).
type AngieRunner struct {
	Binary      string
	MainConfig  string
	ServiceUnit string
	StatusURL   string
	PIDFile     string
	CheckAddr   string
	UseSystemd  *bool

	runCmd     func(ctx context.Context, name string, args ...string) ([]byte, error)
	lookPath   func(file string) (string, error)
	httpClient *http.Client
	checkTCP   func(ctx context.Context, addr string, timeout time.Duration) error
}

func (r *AngieRunner) bin() string {
	if strings.TrimSpace(r.Binary) != "" {
		return strings.TrimSpace(r.Binary)
	}
	return DefaultAngieBinary
}

func (r *AngieRunner) mainConfig(override string) string {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}
	if strings.TrimSpace(r.MainConfig) != "" {
		return strings.TrimSpace(r.MainConfig)
	}
	return DefaultAngieMainConfig
}

func (r *AngieRunner) serviceUnit() string {
	if strings.TrimSpace(r.ServiceUnit) != "" {
		return strings.TrimSpace(r.ServiceUnit)
	}
	return DefaultAngieServiceUnit
}

func (r *AngieRunner) execCmd(ctx context.Context, name string, args ...string) ([]byte, error) {
	if r.runCmd != nil {
		return r.runCmd(ctx, name, args...)
	}
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func (r *AngieRunner) findBin(name string) (string, error) {
	if r.lookPath != nil {
		return r.lookPath(name)
	}
	return exec.LookPath(name)
}

func (r *AngieRunner) hasSystemdUnit(ctx context.Context) bool {
	if r.UseSystemd != nil {
		return *r.UseSystemd
	}
	if _, err := r.findBin("systemctl"); err != nil {
		return false
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := r.execCmd(cctx, "systemctl", "is-active", r.serviceUnit())
	return err == nil && strings.TrimSpace(string(out)) == "active"
}

// Validate runs `angie -t -c <mainConfig>` (60s timeout).
func (r *AngieRunner) Validate(ctx context.Context, mainConfig string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, caddyValidateTimeout)
	defer cancel()

	cfgPath := r.mainConfig(mainConfig)
	// `angie -t` opens the pid file named in the config. Its directory (/run/angie) is created
	// by systemd only once the service has started, so on a node switching from another engine
	// the very first validation would fail without it.
	pidFile := strings.TrimSpace(r.PIDFile)
	if pidFile == "" {
		pidFile = DefaultAngiePIDFile
	}
	if runtime.GOOS != "windows" {
		_ = os.MkdirAll(filepath.Dir(pidFile), 0o755)
	}
	out, err := r.execCmd(cctx, r.bin(), "-t", "-c", cfgPath)
	trimmed := truncateStr(string(out), maxValidateErrBytes)
	if err != nil {
		return trimmed, fmt.Errorf("angie -t failed: %w: %s", err, trimmed)
	}
	return trimmed, nil
}

// Reload reloads Angie via `systemctl reload <unit>` when managed by systemd,
// falling back to `angie -s reload -c <mainConfig>` otherwise.
func (r *AngieRunner) Reload(ctx context.Context) (time.Duration, error) {
	cctx, cancel := context.WithTimeout(ctx, caddyReloadTimeout)
	defer cancel()

	start := time.Now()
	var out []byte
	var err error
	if r.hasSystemdUnit(cctx) {
		out, err = r.execCmd(cctx, "systemctl", "reload", r.serviceUnit())
	} else {
		out, err = r.execCmd(cctx, r.bin(), "-s", "reload", "-c", r.mainConfig(""))
	}
	dur := time.Since(start)
	if err != nil {
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			return dur, fmt.Errorf("angie reload timed out after %s", caddyReloadTimeout)
		}
		return dur, fmt.Errorf("angie reload failed: %w: %s", err, truncateStr(string(out), maxValidateErrBytes))
	}
	return dur, nil
}

// CheckAlive verifies that Angie is running via StatusURL, systemd unit state,
// TCP check address, or PID file liveness.
func (r *AngieRunner) CheckAlive(ctx context.Context) error {
	if statusURL := strings.TrimSpace(r.StatusURL); statusURL != "" {
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(cctx, http.MethodGet, statusURL, nil)
		if err != nil {
			return fmt.Errorf("angie status request: %w", err)
		}
		client := r.httpClient
		if client == nil {
			client = &http.Client{Timeout: 3 * time.Second}
		}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("angie status unreachable: %w", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("angie status returned HTTP %d", resp.StatusCode)
		}
		return nil
	}

	if r.UseSystemd == nil || *r.UseSystemd {
		if _, err := r.findBin("systemctl"); err == nil {
			cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			out, err := r.execCmd(cctx, "systemctl", "is-active", r.serviceUnit())
			state := strings.TrimSpace(string(out))
			if err == nil && state == "active" {
				return nil
			}
			if r.UseSystemd != nil && *r.UseSystemd {
				return fmt.Errorf("%s not active: %s", r.serviceUnit(), truncateStr(state, 256))
			}
		} else if r.UseSystemd != nil && *r.UseSystemd {
			return fmt.Errorf("systemctl not found for %s", r.serviceUnit())
		}
	}

	if addr := strings.TrimSpace(r.CheckAddr); addr != "" {
		hostPort := strings.TrimPrefix(strings.TrimPrefix(addr, "http://"), "https://")
		if r.checkTCP != nil {
			return r.checkTCP(ctx, hostPort, 3*time.Second)
		}
		return (netDialChecker{}).CheckTCP(ctx, hostPort, 3*time.Second)
	}

	pidFile := strings.TrimSpace(r.PIDFile)
	if pidFile == "" {
		pidFile = DefaultAngiePIDFile
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		return fmt.Errorf("angie not alive (pid file %s: %w)", pidFile, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return fmt.Errorf("invalid angie pid in %s", pidFile)
	}
	if !isPIDAlive(pid) {
		return fmt.Errorf("angie pid %d is not running", pid)
	}
	return nil
}

// Version returns the installed Angie version (e.g. "1.12.4") from `angie -v`.
func (r *AngieRunner) Version(ctx context.Context) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	out, err := r.execCmd(cctx, r.bin(), "-v")
	if err != nil {
		return "", fmt.Errorf("angie -v failed: %w: %s", err, truncateStr(string(out), 256))
	}
	ver := parseAngieVersionOutput(string(out))
	if ver == "" {
		return "", fmt.Errorf("unexpected angie -v output: %q", truncateStr(string(out), 256))
	}
	return ver, nil
}

func parseAngieVersionOutput(out string) string {
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if idx := strings.Index(line, "Angie/"); idx >= 0 {
			rest := line[idx+len("Angie/"):]
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				return strings.TrimSpace(fields[0])
			}
		}
	}
	return ""
}
