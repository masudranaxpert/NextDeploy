package edge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// FerronRunner implements Runner for Ferron 3 (`ferron validate -c ... --json --config-adapter ferronconf`,
// admin HTTP `/reload` + `GET /reload` generation verification or `systemctl reload`, `/health`, and `ferron version`).
type FerronRunner struct {
	Binary         string
	MainConfig     string
	ServiceUnit    string
	AdminURL       string
	AdminToken     string
	AdminTokenFile string
	PIDFile        string
	CheckAddr      string
	UseSystemd     *bool

	runCmd     func(ctx context.Context, name string, args ...string) ([]byte, error)
	lookPath   func(file string) (string, error)
	httpClient *http.Client
	checkTCP   func(ctx context.Context, addr string, timeout time.Duration) error
}

func (r *FerronRunner) bin() string {
	if strings.TrimSpace(r.Binary) != "" {
		return strings.TrimSpace(r.Binary)
	}
	return DefaultFerronBinary
}

func (r *FerronRunner) mainConfig(override string) string {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}
	if strings.TrimSpace(r.MainConfig) != "" {
		return strings.TrimSpace(r.MainConfig)
	}
	return DefaultFerronMainConfig
}

func (r *FerronRunner) serviceUnit() string {
	if strings.TrimSpace(r.ServiceUnit) != "" {
		return strings.TrimSpace(r.ServiceUnit)
	}
	return DefaultFerronServiceUnit
}

func (r *FerronRunner) adminURL() string {
	u := strings.TrimRight(strings.TrimSpace(r.AdminURL), "/")
	return u
}

func (r *FerronRunner) adminToken() string {
	if tok := strings.TrimSpace(r.AdminToken); tok != "" {
		return tok
	}
	if p := strings.TrimSpace(r.AdminTokenFile); p != "" {
		if raw, err := os.ReadFile(p); err == nil {
			if tok := strings.TrimSpace(string(raw)); tok != "" {
				return tok
			}
		}
	}
	return strings.TrimSpace(os.Getenv("ND_FERRON_ADMIN_TOKEN"))
}

func (r *FerronRunner) client() *http.Client {
	if r.httpClient != nil {
		return r.httpClient
	}
	return &http.Client{Timeout: 5 * time.Second}
}

func (r *FerronRunner) execCmd(ctx context.Context, name string, args ...string) ([]byte, error) {
	if r.runCmd != nil {
		return r.runCmd(ctx, name, args...)
	}
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func (r *FerronRunner) findBin(name string) (string, error) {
	if r.lookPath != nil {
		return r.lookPath(name)
	}
	return exec.LookPath(name)
}

func (r *FerronRunner) hasSystemdUnit(ctx context.Context) bool {
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

type ferronValidateJSON struct {
	Valid       bool `json:"valid"`
	Diagnostics []struct {
		Kind    string `json:"kind"`
		Message string `json:"message"`
	} `json:"diagnostics"`
}

// Validate runs `ferron validate -c <mainConfig> --json --config-adapter ferronconf`.
func (r *FerronRunner) Validate(ctx context.Context, mainConfig string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, caddyValidateTimeout)
	defer cancel()

	cfgPath := r.mainConfig(mainConfig)
	out, err := r.execCmd(cctx, r.bin(), "validate", "-c", cfgPath, "--json", "--config-adapter", "ferronconf")
	trimmed := truncateStr(string(out), maxValidateErrBytes)

	var parsed ferronValidateJSON
	parsedOK := json.Unmarshal(out, &parsed) == nil
	if err != nil || (parsedOK && !parsed.Valid) {
		var msgs []string
		for _, d := range parsed.Diagnostics {
			if strings.EqualFold(d.Kind, "Invalid configuration") || err != nil {
				msgs = append(msgs, d.Message)
			}
		}
		detail := trimmed
		if len(msgs) > 0 {
			detail = truncateStr(strings.Join(msgs, "; "), maxValidateErrBytes)
		}
		if err != nil {
			return trimmed, fmt.Errorf("ferron validate failed: %w: %s", err, detail)
		}
		return trimmed, fmt.Errorf("ferron validate failed: %s", detail)
	}
	return trimmed, nil
}

type ferronReloadStatusJSON struct {
	LastReloadTime   string  `json:"last_reload_time"`
	LastReloadError  *string `json:"last_reload_error"`
	ActiveGeneration int64   `json:"active_generation"`
}

type ferronReloadPostJSON struct {
	Status string  `json:"status"`
	Error  *string `json:"error"`
}

// Reload triggers a configuration reload via Ferron's admin HTTP API
// (`POST /reload` followed by `GET /reload` generation/error verification),
// falling back to `systemctl reload ferron.service` when the admin listener is
// unavailable and systemd is active.
func (r *FerronRunner) Reload(ctx context.Context) (time.Duration, error) {
	cctx, cancel := context.WithTimeout(ctx, caddyReloadTimeout)
	defer cancel()

	start := time.Now()
	if base := r.adminURL(); base != "" {
		prevStatus, prevErr := r.getReloadStatus(cctx, base)
		hasPrev := prevErr == nil

		postCode, postBody, postErr := r.doAdminRequest(cctx, http.MethodPost, base+"/reload")
		if postErr == nil {
			dur := time.Since(start)
			if postCode < 200 || postCode >= 300 {
				var p ferronReloadPostJSON
				if json.Unmarshal(postBody, &p) == nil && p.Error != nil && *p.Error != "" {
					return dur, fmt.Errorf("ferron POST /reload failed (HTTP %d): %s", postCode, *p.Error)
				}
				return dur, fmt.Errorf("ferron POST /reload returned HTTP %d: %s", postCode, truncateStr(string(postBody), 256))
			}

			// Ferron briefly restarts the admin API listener during reload; retry GET /reload briefly.
			if err := r.verifyReloadStatus(cctx, base, hasPrev, prevStatus.ActiveGeneration); err != nil {
				return time.Since(start), err
			}
			return time.Since(start), nil
		}
	}

	if r.hasSystemdUnit(cctx) {
		out, err := r.execCmd(cctx, "systemctl", "reload", r.serviceUnit())
		dur := time.Since(start)
		if err != nil {
			if errors.Is(cctx.Err(), context.DeadlineExceeded) {
				return dur, fmt.Errorf("ferron reload timed out after %s", caddyReloadTimeout)
			}
			return dur, fmt.Errorf("ferron systemd reload failed: %w: %s", err, truncateStr(string(out), maxValidateErrBytes))
		}
		return dur, nil
	}

	return time.Since(start), errors.New("ferron reload failed: admin API unreachable and systemd unit inactive")
}

func (r *FerronRunner) doAdminRequest(ctx context.Context, method, fullURL string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, fullURL, nil)
	if err != nil {
		return 0, nil, err
	}
	if tok := r.adminToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := r.client().Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	return resp.StatusCode, body, nil
}

func (r *FerronRunner) getReloadStatus(ctx context.Context, base string) (ferronReloadStatusJSON, error) {
	code, body, err := r.doAdminRequest(ctx, http.MethodGet, base+"/reload")
	if err != nil {
		return ferronReloadStatusJSON{}, err
	}
	if code < 200 || code >= 300 {
		return ferronReloadStatusJSON{}, fmt.Errorf("GET /reload returned HTTP %d: %s", code, truncateStr(string(body), 256))
	}
	var st ferronReloadStatusJSON
	if err := json.Unmarshal(body, &st); err != nil {
		return ferronReloadStatusJSON{}, fmt.Errorf("decode GET /reload JSON: %w", err)
	}
	return st, nil
}

func (r *FerronRunner) verifyReloadStatus(ctx context.Context, base string, hasPrev bool, prevGen int64) error {
	var lastErr error
	for attempt := 0; attempt < 8; attempt++ {
		st, err := r.getReloadStatus(ctx, base)
		if err == nil {
			if st.LastReloadError != nil && *st.LastReloadError != "" {
				return fmt.Errorf("ferron reload reported error: %s", *st.LastReloadError)
			}
			if !hasPrev || st.ActiveGeneration > prevGen {
				return nil
			}
			lastErr = fmt.Errorf("ferron active_generation did not advance (%d -> %d)", prevGen, st.ActiveGeneration)
		} else {
			lastErr = err
		}
		if attempt < 7 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	return lastErr
}

// CheckAlive verifies Ferron runtime liveness via `GET /health` on the admin
// listener, falling back to systemd unit status, TCP check, or PID liveness.
func (r *FerronRunner) CheckAlive(ctx context.Context) error {
	if base := r.adminURL(); base != "" {
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(cctx, http.MethodGet, base+"/health", nil)
		if err != nil {
			return fmt.Errorf("ferron health request: %w", err)
		}
		resp, err := r.client().Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			return fmt.Errorf("ferron /health returned HTTP %d", resp.StatusCode)
		}
		if r.UseSystemd != nil && !*r.UseSystemd {
			return fmt.Errorf("ferron /health unreachable: %w", err)
		}
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
		pidFile = DefaultFerronPIDFile
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		return fmt.Errorf("ferron not alive (pid file %s: %w)", pidFile, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return fmt.Errorf("invalid ferron pid in %s", pidFile)
	}
	if !isPIDAlive(pid) {
		return fmt.Errorf("ferron pid %d is not running", pid)
	}
	return nil
}

// Version returns the installed Ferron version (e.g. "3.0.0-rc.11") from `ferron version`.
func (r *FerronRunner) Version(ctx context.Context) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	out, err := r.execCmd(cctx, r.bin(), "version")
	if err != nil {
		return "", fmt.Errorf("ferron version failed: %w: %s", err, truncateStr(string(out), 256))
	}
	ver := parseFerronVersionOutput(string(out))
	if ver == "" {
		return "", fmt.Errorf("unexpected ferron version output: %q", truncateStr(string(out), 256))
	}
	return ver, nil
}

func parseFerronVersionOutput(out string) string {
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if idx := strings.Index(line, "Ferron "); idx >= 0 {
			rest := line[idx+len("Ferron "):]
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				return strings.TrimSpace(fields[0])
			}
		}
		fields := strings.Fields(line)
		if len(fields) > 0 && (fields[0] != "" && fields[0][0] >= '0' && fields[0][0] <= '9') {
			return fields[0]
		}
	}
	return ""
}
