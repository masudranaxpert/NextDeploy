package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Config holds the saved CLI configuration.
type Config struct {
	ServerURL string `json:"server_url"`
	Token     string `json:"token"`
	DeviceID  string `json:"device_id,omitempty"`
}

// EnsureDeviceID generates and assigns a persistent device identifier if absent.
func (c *Config) EnsureDeviceID() string {
	if c.DeviceID == "" {
		hostname, _ := os.Hostname()
		if hostname == "" {
			hostname = "device"
		}
		hostname = strings.ToLower(strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
				return r
			}
			return '-'
		}, hostname))
		hostname = strings.Trim(hostname, "-")
		if len(hostname) > 20 {
			hostname = hostname[:20]
		}
		b := make([]byte, 4)
		_, _ = rand.Read(b)
		c.DeviceID = fmt.Sprintf("nd_%s_%s", hostname, hex.EncodeToString(b))
	}
	return c.DeviceID
}

// ProjectConfig holds the locally linked app configuration (.nd/project.json).
type ProjectConfig struct {
	AppID string `json:"app_id"`
}

func path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not find home dir: %w", err)
	}
	dir := filepath.Join(home, ".nd")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads the config from disk or environment variables.
func Load() (Config, error) {
	var c Config
	p, err := path()
	if err == nil {
		if data, rerr := os.ReadFile(p); rerr == nil {
			_ = json.Unmarshal(data, &c)
		}
	}

	// Environment variables take precedence or provide fallbacks for CI / AI runners
	if envURL := strings.TrimRight(os.Getenv("ND_SERVER_URL"), "/"); envURL != "" {
		c.ServerURL = envURL
	} else if envURL := strings.TrimRight(os.Getenv("NEXTDEPLOY_URL"), "/"); envURL != "" {
		c.ServerURL = envURL
	}

	if envTok := strings.TrimSpace(os.Getenv("ND_TOKEN")); envTok != "" {
		c.Token = envTok
	} else if envTok := strings.TrimSpace(os.Getenv("NEXTDEPLOY_TOKEN")); envTok != "" {
		c.Token = envTok
	}

	c.ServerURL = strings.TrimRight(c.ServerURL, "/")
	c.Token = strings.TrimSpace(c.Token)
	return c, nil
}

// Save writes the config atomically to disk.
func Save(c Config) error {
	p, err := path()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	// Write to temp, then rename (atomic on same filesystem)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// LoadProject searches starting from dir upwards for a .nd/project.json file.
func LoadProject(dir string) (ProjectConfig, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ProjectConfig{}, err
	}
	curr := abs
	for {
		target := filepath.Join(curr, ".nd", "project.json")
		if data, err := os.ReadFile(target); err == nil {
			var pc ProjectConfig
			if err := json.Unmarshal(data, &pc); err == nil && pc.AppID != "" {
				return pc, nil
			}
		}
		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}
		curr = parent
	}
	return ProjectConfig{}, nil
}

// SaveProject links a local directory to an app ID by writing .nd/project.json.
func SaveProject(dir, appID string) error {
	dotNd := filepath.Join(dir, ".nd")
	if err := os.MkdirAll(dotNd, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(ProjectConfig{AppID: appID}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dotNd, "project.json"), data, 0644)
}

// ClearProject removes .nd/project.json in the specified directory.
func ClearProject(dir string) error {
	p := filepath.Join(dir, ".nd", "project.json")
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// OSArch returns the current OS and architecture strings.
func OSArch() (string, string) {
	return runtime.GOOS, runtime.GOARCH
}
