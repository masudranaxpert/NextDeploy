package client

import (
	"bytes"
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
	cfg    config.Config
	http   *http.Client
	sessID string // CLI session ID for heartbeats
}

// New creates a Client from saved config.
func New(cfg config.Config, sessID string) *Client {
	return &Client{
		cfg:    cfg,
		http:   &http.Client{Timeout: 30 * time.Second},
		sessID: sessID,
	}
}

// Do executes an authenticated request and returns the response body.
func (c *Client) Do(method, path string, body any) ([]byte, int, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		r = bytes.NewReader(b)
	}

	url := strings.TrimRight(c.cfg.ServerURL, "/") + path
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
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
	url := strings.TrimRight(c.cfg.ServerURL, "/") + path
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	req.Header.Set("Content-Type", contentType)

	// Increase timeout for uploads
	c.http.Timeout = 10 * time.Minute
	defer func() { c.http.Timeout = 30 * time.Second }()

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("upload failed: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

// Heartbeat sends a CLI session heartbeat to the server.
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
