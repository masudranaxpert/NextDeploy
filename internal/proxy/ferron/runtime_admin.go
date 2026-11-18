package ferron

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type adminStatusResponse struct {
	UptimeSec         int64  `json:"uptime_sec"`
	ConnectionsActive int64  `json:"connections_active"`
	RequestsTotal     int64  `json:"requests_total"`
	Reloads           uint64 `json:"reloads"`
	ConfigDrift       bool   `json:"config_drift"`
}

type adminReloadPostResponse struct {
	Status string  `json:"status"`
	Error  *string `json:"error"`
}

type adminReloadGetResponse struct {
	LastReloadTime   string  `json:"last_reload_time"`
	LastReloadError  *string `json:"last_reload_error"`
	ActiveGeneration uint64  `json:"active_generation"`
}

func (d *Driver) reloadAndVerify(ctx context.Context, hasBefore bool, beforeGen uint64) error {
	if err := d.triggerReload(ctx); err != nil {
		return err
	}

	deadline := time.Now().Add(3 * time.Second)
	var lastResp adminReloadGetResponse
	var lastErr error
	for attempt := 0; ; attempt++ {
		resp, err := d.getReloadStatus(ctx)
		if err == nil {
			lastResp = resp
			if resp.LastReloadError != nil && strings.TrimSpace(*resp.LastReloadError) != "" {
				return fmt.Errorf("ferron reload error: %s", strings.TrimSpace(*resp.LastReloadError))
			}
			if !hasBefore || resp.ActiveGeneration > beforeGen {
				return nil
			}
			lastErr = fmt.Errorf("active_generation remained %d (expected > %d)", resp.ActiveGeneration, beforeGen)
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(25*(attempt+1)) * time.Millisecond):
		}
	}
	if lastResp.LastReloadError != nil && strings.TrimSpace(*lastResp.LastReloadError) != "" {
		return fmt.Errorf("ferron reload error: %s", strings.TrimSpace(*lastResp.LastReloadError))
	}
	return fmt.Errorf("ferron reload verification failed: %w", lastErr)
}

func (d *Driver) triggerReload(ctx context.Context) error {
	code, body, err := d.doAdminRequest(ctx, http.MethodPost, "/reload")
	if err != nil {
		return fmt.Errorf("ferron POST /reload: %w", err)
	}
	var resp adminReloadPostResponse
	_ = json.Unmarshal(body, &resp)
	if code != http.StatusOK {
		if resp.Error != nil && strings.TrimSpace(*resp.Error) != "" {
			return fmt.Errorf("ferron reload failed (%d): %s", code, strings.TrimSpace(*resp.Error))
		}
		return fmt.Errorf("ferron POST /reload HTTP %d: %s", code, strings.TrimSpace(string(body)))
	}
	if resp.Error != nil && strings.TrimSpace(*resp.Error) != "" {
		return fmt.Errorf("ferron reload failed: %s", strings.TrimSpace(*resp.Error))
	}
	return nil
}

func (d *Driver) getHealth(ctx context.Context) (bool, error) {
	code, _, err := d.doAdminRequest(ctx, http.MethodGet, "/health")
	if err != nil {
		return false, err
	}
	return code == http.StatusOK, nil
}

func (d *Driver) getStatus(ctx context.Context) (adminStatusResponse, error) {
	code, body, err := d.doAdminRequest(ctx, http.MethodGet, "/status")
	if err != nil {
		return adminStatusResponse{}, err
	}
	if code != http.StatusOK {
		return adminStatusResponse{}, fmt.Errorf("GET /status HTTP %d: %s", code, strings.TrimSpace(string(body)))
	}
	var out adminStatusResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return adminStatusResponse{}, fmt.Errorf("decode /status: %w", err)
	}
	return out, nil
}

func (d *Driver) getReloadStatus(ctx context.Context) (adminReloadGetResponse, error) {
	code, body, err := d.doAdminRequest(ctx, http.MethodGet, "/reload")
	if err != nil {
		return adminReloadGetResponse{}, err
	}
	if code != http.StatusOK {
		return adminReloadGetResponse{}, fmt.Errorf("GET /reload HTTP %d: %s", code, strings.TrimSpace(string(body)))
	}
	var out adminReloadGetResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return adminReloadGetResponse{}, fmt.Errorf("decode /reload: %w", err)
	}
	return out, nil
}

func (d *Driver) doAdminRequest(ctx context.Context, method, reqPath string) (int, []byte, error) {
	if d.hooks.AdminRequestFunc != nil {
		return d.hooks.AdminRequestFunc(ctx, method, reqPath)
	}
	if d.adminURL != "" && (d.adminURL != DefaultAdminURL || d.adminToken != "") {
		code, body, err := d.doDirectHTTPAdminRequest(ctx, method, reqPath)
		if err == nil || d.adminURL != DefaultAdminURL {
			return code, body, err
		}
	}
	return d.doContainerExecAdminRequest(ctx, method, reqPath)
}

func (d *Driver) doDirectHTTPAdminRequest(ctx context.Context, method, reqPath string) (int, []byte, error) {
	target := strings.TrimRight(d.adminURL, "/") + reqPath
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return 0, nil, err
	}
	if d.adminToken != "" && reqPath != "/health" {
		req.Header.Set("Authorization", "Bearer "+d.adminToken)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, body, nil
}

func (d *Driver) doContainerExecAdminRequest(ctx context.Context, method, reqPath string) (int, []byte, error) {
	addr := d.effectiveAdminSocketAddr()
	u, _ := url.Parse(d.adminURL)
	host, port := "127.0.0.1", "8081"
	if u != nil && u.Hostname() != "" {
		host = u.Hostname()
	}
	if u != nil && u.Port() != "" {
		port = u.Port()
	} else if hp := strings.Split(addr, ":"); len(hp) == 2 {
		host, port = hp[0], hp[1]
	}

	if !isSafeEnvName(d.adminTokenEnv) {
		return 0, nil, errors.New("unsafe admin token env name")
	}
	script := fmt.Sprintf(
		`{ printf '%%s %%s HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\nContent-Length: 0\r\nAuthorization: Bearer %%s\r\n\r\n' %s %s "$%s"; cat; } | nc -w 10 %s %s`,
		shQuote(method), shQuote(reqPath), d.adminTokenEnv, shQuote(host), shQuote(port),
	)
	cmd := exec.CommandContext(ctx, "docker", "exec", "-i", d.ContainerName(), "/bin/sh", "-c", script)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return 0, nil, fmt.Errorf("docker exec stdin %s %s: %w", method, reqPath, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, nil, fmt.Errorf("docker exec stdout %s %s: %w", method, reqPath, err)
	}
	if err := cmd.Start(); err != nil {
		return 0, nil, fmt.Errorf("docker exec admin %s %s: %w", method, reqPath, err)
	}
	resp, perr := http.ReadResponse(bufio.NewReader(stdout), nil)
	if perr != nil {
		_ = stdin.Close()
		_ = cmd.Wait()
		return 0, nil, fmt.Errorf("parse admin response for %s %s: %w", method, reqPath, perr)
	}
	body, rerr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
	_ = stdin.Close()
	_ = cmd.Wait()
	if rerr != nil {
		return 0, nil, rerr
	}
	return resp.StatusCode, body, nil
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func parseAdminListenAddr(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err == nil && u.Host != "" {
		if _, _, serr := net.SplitHostPort(u.Host); serr == nil {
			return u.Host
		}
	}
	if _, _, serr := net.SplitHostPort(rawURL); serr == nil {
		return rawURL
	}
	return "127.0.0.1:8081"
}

func adminURLTagVersion(img string) string {
	idx := strings.LastIndex(img, ":")
	if idx < 0 || idx+1 >= len(img) {
		return ""
	}
	tag := strings.TrimSpace(img[idx+1:])
	tag = strings.TrimSuffix(tag, "-alpine")
	return strings.TrimSuffix(tag, "-debian")
}

func clampLines(lines int) int {
	if lines <= 0 {
		return 200
	}
	if lines > 5000 {
		return 5000
	}
	return lines
}

func parsePositiveIntPort(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 || n > 65535 {
		return 0
	}
	return n
}
