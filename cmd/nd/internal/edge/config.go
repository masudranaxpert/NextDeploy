package edge

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultAdminAddr     = "localhost:2019"
	DefaultPerPanelQuota = 2000
	DefaultPollInterval  = 30
	PlaceholderFileName  = "00-placeholder.caddy"
)

// ErrConfigNotFound is returned when /etc/nd-edge/config.json does not exist yet.
var ErrConfigNotFound = errors.New("nd-edge config not found")

// Paths holds all filesystem locations used by nd-edge. Using DefaultPaths()
// gives the production Linux layout (§1); tests can construct a rooted layout
// with NewPaths(t.TempDir()).
type Paths struct {
	Caddyfile     string
	CaddyNDDir    string
	GlobalOpts    string
	CommonCaddy   string
	PanelsLink    string
	OriginsLink   string
	UserCaddyDir  string
	CaddyCertsDir string

	ConfigFile   string
	StateDir     string
	ClaimsFile   string
	DNSCacheFile string
	CFIPsFile    string
	ApplyMeta    string

	LockFile string
	PIDFile  string
}

// DefaultPaths returns the canonical Shield VPS paths from §1.
func DefaultPaths() Paths {
	return Paths{
		Caddyfile:     "/etc/caddy/Caddyfile",
		CaddyNDDir:    "/etc/caddy/nd",
		GlobalOpts:    "/etc/caddy/nd/global.opts",
		CommonCaddy:   "/etc/caddy/nd/common.caddy",
		PanelsLink:    "/etc/caddy/nd/panels",
		OriginsLink:   "/etc/caddy/nd/origins",
		UserCaddyDir:  "/etc/caddy/nd/user",
		CaddyCertsDir: "/var/lib/caddy/.local/share/caddy",

		ConfigFile:   "/etc/nd-edge/config.json",
		StateDir:     "/var/lib/nd-edge/state",
		ClaimsFile:   "/var/lib/nd-edge/claims.json",
		DNSCacheFile: "/var/lib/nd-edge/dns-cache.json",
		CFIPsFile:    "/var/lib/nd-edge/cf-ips.json",
		ApplyMeta:    "/var/lib/nd-edge/apply-meta.json",

		LockFile: "/run/nd-edge.lock",
		PIDFile:  "/run/nd-edge.pid",
	}
}

// NewPaths roots every nd-edge path inside rootDir for isolated testing.
func NewPaths(rootDir string) Paths {
	return Paths{
		Caddyfile:     filepath.Join(rootDir, "etc", "caddy", "Caddyfile"),
		CaddyNDDir:    filepath.Join(rootDir, "etc", "caddy", "nd"),
		GlobalOpts:    filepath.Join(rootDir, "etc", "caddy", "nd", "global.opts"),
		CommonCaddy:   filepath.Join(rootDir, "etc", "caddy", "nd", "common.caddy"),
		PanelsLink:    filepath.Join(rootDir, "etc", "caddy", "nd", "panels"),
		OriginsLink:   filepath.Join(rootDir, "etc", "caddy", "nd", "origins"),
		UserCaddyDir:  filepath.Join(rootDir, "etc", "caddy", "nd", "user"),
		CaddyCertsDir: filepath.Join(rootDir, "var", "lib", "caddy", ".local", "share", "caddy"),

		ConfigFile:   filepath.Join(rootDir, "etc", "nd-edge", "config.json"),
		StateDir:     filepath.Join(rootDir, "var", "lib", "nd-edge", "state"),
		ClaimsFile:   filepath.Join(rootDir, "var", "lib", "nd-edge", "claims.json"),
		DNSCacheFile: filepath.Join(rootDir, "var", "lib", "nd-edge", "dns-cache.json"),
		CFIPsFile:    filepath.Join(rootDir, "var", "lib", "nd-edge", "cf-ips.json"),
		ApplyMeta:    filepath.Join(rootDir, "var", "lib", "nd-edge", "apply-meta.json"),

		LockFile: filepath.Join(rootDir, "run", "nd-edge.lock"),
		PIDFile:  filepath.Join(rootDir, "run", "nd-edge.pid"),
	}
}

// PanelGenDir returns the generation directory path for panels (e.g. panels.gen-000123).
func (p Paths) PanelGenDir(gen int) string {
	return filepath.Join(p.CaddyNDDir, fmt.Sprintf("panels.gen-%06d", gen))
}

// OriginGenDir returns the generation directory path for origin CAs (e.g. origins.gen-000123).
func (p Paths) OriginGenDir(gen int) string {
	return filepath.Join(p.CaddyNDDir, fmt.Sprintf("origins.gen-%06d", gen))
}

// StateFile returns the per-panel state JSON path.
func (p Paths) StateFile(panelID string) string {
	return filepath.Join(p.StateDir, panelID+".json")
}

// Config represents /etc/nd-edge/config.json (0600).
type Config struct {
	EdgeID          string        `json:"edge_id"`
	ACMEEmail       string        `json:"acme_email,omitempty"`
	AdminAddr       string        `json:"admin_addr,omitempty"`
	DNSGateOff      bool          `json:"dns_gate_off,omitempty"`
	PerPanelQuota   int           `json:"per_panel_quota,omitempty"`
	CaddyUser       string        `json:"caddy_user,omitempty"`
	ShieldHostname  string        `json:"shield_hostname,omitempty"`
	ShieldIPs       []string      `json:"shield_ips,omitempty"`
	TrustedProxies  bool          `json:"skip_trusted_proxies,omitempty"`
	PreservedGlobal string        `json:"preserved_global,omitempty"`
	Panels          []PanelConfig `json:"panels"`
}

// PanelConfig stores one registered NextDeploy panel connection.
type PanelConfig struct {
	PanelID         string    `json:"panel_id"`
	PanelName       string    `json:"panel_name"`
	ShieldNodeID    int64     `json:"shield_node_id"`
	URL             string    `json:"url"`
	Token           string    `json:"token"`
	PollIntervalSec int       `json:"poll_interval_sec"`
	RegisteredAt    time.Time `json:"registered_at"`
	PanelCASHA256   string    `json:"panel_ca_sha256,omitempty"`
	InsecureHTTP    bool      `json:"insecure_http,omitempty"`
}

// Redacted returns a copy of PanelConfig with Token masked so it is safe to log.
func (pc PanelConfig) Redacted() PanelConfig {
	out := pc
	if out.Token != "" {
		out.Token = "[REDACTED]"
	}
	return out
}

// LoadConfig loads /etc/nd-edge/config.json strictly.
// Per E-STATE-3, a corrupt config returns *CorruptFileError without modifying
// any files on disk.
func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrConfigNotFound
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, &CorruptFileError{Path: path, Err: errors.New("empty config file")}
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, &CorruptFileError{Path: path, Err: err}
	}
	if strings.TrimSpace(cfg.EdgeID) == "" {
		return nil, &CorruptFileError{Path: path, Err: errors.New("missing edge_id")}
	}
	cfg.applyDefaults()
	return &cfg, nil
}

// SaveConfig writes cfg atomically with mode 0600.
func SaveConfig(path string, cfg *Config) error {
	if cfg == nil {
		return errors.New("nil config")
	}
	cfg.applyDefaults()
	return WriteJSONAtomic(path, cfg, 0o600)
}

func (c *Config) applyDefaults() {
	if strings.TrimSpace(c.AdminAddr) == "" {
		c.AdminAddr = DefaultAdminAddr
	}
	if c.PerPanelQuota <= 0 {
		c.PerPanelQuota = DefaultPerPanelQuota
	}
	if strings.TrimSpace(c.CaddyUser) == "" {
		c.CaddyUser = "caddy"
	}
	for i := range c.Panels {
		if c.Panels[i].PollIntervalSec <= 0 {
			c.Panels[i].PollIntervalSec = DefaultPollInterval
		}
		c.Panels[i].URL = strings.TrimRight(strings.TrimSpace(c.Panels[i].URL), "/")
	}
}

// FindPanel finds a panel by exact panel_id or panel_name.
func (c *Config) FindPanel(idOrName string) (*PanelConfig, int) {
	for i := range c.Panels {
		if c.Panels[i].PanelID == idOrName || c.Panels[i].PanelName == idOrName {
			return &c.Panels[i], i
		}
	}
	return nil, -1
}

// UpsertPanel adds or updates a panel configuration by PanelID.
func (c *Config) UpsertPanel(p PanelConfig) {
	if p.PollIntervalSec <= 0 {
		p.PollIntervalSec = DefaultPollInterval
	}
	p.URL = strings.TrimRight(strings.TrimSpace(p.URL), "/")
	for i := range c.Panels {
		if c.Panels[i].PanelID == p.PanelID {
			c.Panels[i] = p
			return
		}
	}
	c.Panels = append(c.Panels, p)
}

// RemovePanel removes a panel by panel_id or panel_name and returns it if found.
func (c *Config) RemovePanel(idOrName string) (PanelConfig, bool) {
	_, idx := c.FindPanel(idOrName)
	if idx < 0 {
		return PanelConfig{}, false
	}
	removed := c.Panels[idx]
	c.Panels = append(c.Panels[:idx], c.Panels[idx+1:]...)
	return removed, true
}
