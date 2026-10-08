package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AccountConfig holds individual account credentials and identity.
type AccountConfig struct {
	ServerURL string `json:"server_url"`
	Token     string `json:"token"`
	DeviceID  string `json:"device_id,omitempty"`
	Username  string `json:"username,omitempty"`
}

// Config holds the saved CLI configuration with multi-account support.
type Config struct {
	CurrentAccount string                   `json:"current_account,omitempty"`
	Accounts       map[string]AccountConfig `json:"accounts,omitempty"`

	// Mirror fields for backward compatibility with older tools and CI runners
	ServerURL      string                   `json:"server_url,omitempty"`
	Token          string                   `json:"token,omitempty"`
	DeviceID       string                   `json:"device_id,omitempty"`
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
	if c.CurrentAccount != "" && c.Accounts != nil {
		if acc, ok := c.Accounts[c.CurrentAccount]; ok {
			if acc.DeviceID == "" {
				acc.DeviceID = c.DeviceID
				c.Accounts[c.CurrentAccount] = acc
			}
		}
	}
	return c.DeviceID
}

// SetAccount adds or updates an account entry and optionally marks it active.
func (c *Config) SetAccount(name string, acc AccountConfig, makeActive bool) {
	if c.Accounts == nil {
		c.Accounts = make(map[string]AccountConfig)
	}
	name = strings.TrimSpace(name)
	acc.ServerURL = strings.TrimRight(strings.TrimSpace(acc.ServerURL), "/")
	acc.Token = strings.TrimSpace(acc.Token)
	c.Accounts[name] = acc
	if makeActive || c.CurrentAccount == "" {
		c.CurrentAccount = name
		c.ServerURL = acc.ServerURL
		c.Token = acc.Token
		c.DeviceID = acc.DeviceID
	}
}

// SwitchAccount switches the active account to the specified name.
func (c *Config) SwitchAccount(name string) error {
	name = strings.TrimSpace(name)
	acc, ok := c.Accounts[name]
	if !ok {
		return fmt.Errorf("account %q not found", name)
	}
	c.CurrentAccount = name
	c.ServerURL = acc.ServerURL
	c.Token = acc.Token
	c.DeviceID = acc.DeviceID
	return nil
}

// DeleteAccount removes an account and auto-switches to the next available account if active.
func (c *Config) DeleteAccount(name string) (string, error) {
	name = strings.TrimSpace(name)
	if _, ok := c.Accounts[name]; !ok {
		return "", fmt.Errorf("account %q not found", name)
	}
	delete(c.Accounts, name)
	if c.CurrentAccount == name {
		c.CurrentAccount = ""
		for remaining, acc := range c.Accounts {
			c.CurrentAccount = remaining
			c.ServerURL = acc.ServerURL
			c.Token = acc.Token
			c.DeviceID = acc.DeviceID
			break
		}
		if c.CurrentAccount == "" {
			c.ServerURL = ""
			c.Token = ""
		}
	}
	return c.CurrentAccount, nil
}

// RenameAccount renames an account alias while maintaining the active reference.
func (c *Config) RenameAccount(oldName, newName string) error {
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if oldName == "" || newName == "" {
		return fmt.Errorf("account names cannot be empty")
	}
	if oldName == newName {
		return nil
	}
	acc, ok := c.Accounts[oldName]
	if !ok {
		return fmt.Errorf("account %q not found", oldName)
	}
	if _, exists := c.Accounts[newName]; exists {
		return fmt.Errorf("account %q already exists", newName)
	}
	delete(c.Accounts, oldName)
	c.Accounts[newName] = acc
	if c.CurrentAccount == oldName {
		c.CurrentAccount = newName
	}
	return nil
}

// ActiveAccount returns the current active account name and configuration.
func (c *Config) ActiveAccount() (string, AccountConfig, bool) {
	if c.CurrentAccount == "" || c.Accounts == nil {
		return "", AccountConfig{}, false
	}
	acc, ok := c.Accounts[c.CurrentAccount]
	return c.CurrentAccount, acc, ok
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

// Load reads the config from disk or environment variables with graceful migration.
func Load() (Config, error) {
	var c Config
	p, err := path()
	if err == nil {
		if data, rerr := os.ReadFile(p); rerr == nil {
			_ = json.Unmarshal(data, &c)
		}
	}
	if c.Accounts == nil {
		c.Accounts = make(map[string]AccountConfig)
	}

	// Graceful migration: if legacy config has credentials but no accounts map
	if len(c.Accounts) == 0 && (c.ServerURL != "" || c.Token != "") {
		alias := "default"
		c.Accounts[alias] = AccountConfig{
			ServerURL: c.ServerURL,
			Token:     c.Token,
			DeviceID:  c.DeviceID,
		}
		c.CurrentAccount = alias
	}

	// Environment variable ND_ACCOUNT overrides active account selection
	if envAcc := strings.TrimSpace(os.Getenv("ND_ACCOUNT")); envAcc != "" {
		if _, ok := c.Accounts[envAcc]; ok {
			c.CurrentAccount = envAcc
		}
	}

	// Fallback to first available account if current_account is invalid
	if c.CurrentAccount == "" || c.Accounts[c.CurrentAccount].ServerURL == "" {
		for name := range c.Accounts {
			c.CurrentAccount = name
			break
		}
	}

	// Synchronize top-level fields with active account
	if acc, ok := c.Accounts[c.CurrentAccount]; ok {
		c.ServerURL = acc.ServerURL
		c.Token = acc.Token
		if acc.DeviceID != "" {
			c.DeviceID = acc.DeviceID
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

// Save writes the config atomically to disk with top-level mirrors.
func Save(c Config) error {
	if c.Accounts == nil {
		c.Accounts = make(map[string]AccountConfig)
	}

	// Keep active account and top-level mirror in sync
	if c.CurrentAccount != "" {
		if acc, ok := c.Accounts[c.CurrentAccount]; ok {
			if c.ServerURL != "" {
				acc.ServerURL = c.ServerURL
			}
			if c.Token != "" {
				acc.Token = c.Token
			}
			if c.DeviceID != "" {
				acc.DeviceID = c.DeviceID
			}
			c.Accounts[c.CurrentAccount] = acc
			c.ServerURL = acc.ServerURL
			c.Token = acc.Token
			c.DeviceID = acc.DeviceID
		}
	} else if len(c.Accounts) == 0 {
		c.ServerURL = ""
		c.Token = ""
	}

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
	if err := os.WriteFile(filepath.Join(dotNd, "project.json"), data, 0644); err != nil {
		return err
	}
	ensureGitIgnore(dir, ".nd/")
	return nil
}

// ensureGitIgnore makes sure the specified pattern is present in .gitignore.
func ensureGitIgnore(dir, pattern string) {
	giPath := filepath.Join(dir, ".gitignore")
	content, err := os.ReadFile(giPath)
	if err != nil {
		if os.IsNotExist(err) {
			_ = os.WriteFile(giPath, []byte(pattern+"\n"), 0644)
		}
		return
	}
	for _, l := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(l)
		if trimmed == pattern || trimmed == strings.TrimSuffix(pattern, "/") {
			return
		}
	}
	newContent := string(content)
	if len(newContent) > 0 && !strings.HasSuffix(newContent, "\n") {
		newContent += "\n"
	}
	newContent += pattern + "\n"
	_ = os.WriteFile(giPath, []byte(newContent), 0644)
}

// ClearProject removes .nd/project.json in the specified directory.
func ClearProject(dir string) error {
	p := filepath.Join(dir, ".nd", "project.json")
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

