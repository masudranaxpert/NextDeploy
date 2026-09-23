package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"nd/internal/config"
)

// Client is an authenticated HTTP client for the NextDeploy API.
type Client struct {
	cfg     config.Config
	http    *http.Client
	sessID  string // CLI session ID for heartbeats
	version string
}

// New creates a Client from saved config.
func New(cfg config.Config, sessID string, version ...string) *Client {
	ver := "1.2.1"
	if len(version) > 0 && version[0] != "" {
		ver = version[0]
	}
	return &Client{
		cfg:     cfg,
		http:    &http.Client{},
		sessID:  sessID,
		version: ver,
	}
}

// userAgent returns the formatted CLI User-Agent header.
func (c *Client) userAgent() string {
	if c.version != "" {
		return "nd/" + c.version
	}
	return "nd/1.2.1"
}

// Do executes an authenticated request and returns the response body.
func (c *Client) Do(method, path string, body any) ([]byte, int, error) {
	return c.DoWithTimeout(method, path, body, 0)
}

// DoWithTimeout executes an authenticated request with a custom timeout duration (0 uses default).
func (c *Client) DoWithTimeout(method, path string, body any, timeout time.Duration) ([]byte, int, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		r = bytes.NewReader(b)
	}

	url := strings.TrimRight(c.cfg.ServerURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	req.Header.Set("X-NextDeploy-Client", "cli")
	req.Header.Set("User-Agent", c.userAgent())
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

// DoRaw executes an authenticated request with a raw reader body (for multipart uploads).
func (c *Client) DoRaw(method, path string, contentType string, body io.Reader) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	url := strings.TrimRight(c.cfg.ServerURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	req.Header.Set("X-NextDeploy-Client", "cli")
	req.Header.Set("User-Agent", c.userAgent())
	req.Header.Set("Content-Type", contentType)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("upload failed: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

// Heartbeat sends a CLI session heartbeat to the server asynchronously.
func (c *Client) Heartbeat(hostname, osStr, arch, version string) {
	if c.sessID == "" {
		return
	}
	payload := map[string]string{
		"id":       c.sessID,
		"hostname": hostname,
		"os":       osStr,
		"arch":     arch,
		"version":  version,
	}
	// Fire and forget — don't block the caller
	go func() {
		_, _, _ = c.Do("POST", "/api/v1/cli/sessions", payload)
	}()
}

// HeartbeatSync sends a CLI session heartbeat synchronously (e.g. during login).
func (c *Client) HeartbeatSync(hostname, osStr, arch, version string) error {
	if c.sessID == "" {
		return nil
	}
	payload := map[string]string{
		"id":       c.sessID,
		"hostname": hostname,
		"os":       osStr,
		"arch":     arch,
		"version":  version,
	}
	_, _, err := c.Do("POST", "/api/v1/cli/sessions", payload)
	return err
}

// Disconnect removes the CLI session from the server.
func (c *Client) Disconnect() {
	if c.sessID == "" {
		return
	}
	_, _, _ = c.Do("DELETE", "/api/v1/cli/sessions/"+c.sessID, nil)
}

// JSONError extracts the "error" field from a JSON response body.
func JSONError(body []byte) string {
	var m map[string]interface{}
	if err := json.Unmarshal(body, &m); err != nil {
		return string(body)
	}
	if e, ok := m["error"].(string); ok {
		return e
	}
	return string(body)
}
