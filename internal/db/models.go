package db

import (
	"encoding/json"
	"strings"
	"time"
)

// User roles
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// User represents a panel user account.
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	Role         string
	CreatedAt    time.Time
}

type App struct {
	ID          string
	Name        string
	CreatedAt   time.Time
	ComposeFile string
	TemplateID  string
}

type AppGitConfig struct {
	AppID          string
	GitProviderID  int64
	Provider       string
	RepoURL        string
	RepoFullName   string
	Branch         string
	AuthMode       string
	Token          string
	AppGitID       string
	InstallationID string
	PrivateKeyPEM  string
	WebhookSecret  string
	AutoDeploy     bool
	LastDeployRef  string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// GitProvider holds a named global Git credential (token) for a provider.
type GitProvider struct {
	ID        int64
	Name      string
	Provider  string
	Token     string
	Notes     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type GitHubProviderDetail struct {
	ProviderID         int64
	GitHubAppID        string
	ClientID           string
	ClientSecret       string
	PrivateKeyPEM      string
	WebhookSecret      string
	InstallationID     string
	AccountLogin       string
	AppSlug            string
	ManifestState      string
	CreatedViaManifest bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// AppDomain is a domain entry attached to an app with caddy routing config.
type AppDomain struct {
	ID             int64
	AppID          string
	Domain         string
	Service        string
	Port           int
	EnableHTTPS    bool
	EnableWWW      bool
	ServeStatic    bool
	StaticPath     string
	ServeMedia     bool
	MediaPath      string
	RouteRulesJSON string
	CreatedAt      time.Time
}

type PHPPanelSite struct {
	ID         int64
	AppID      string
	UserID     int64
	Name       string
	Slug       string
	PHPVersion string
	CreatedAt  time.Time
}

type TemplateAppDomain struct {
	ID          int64
	AppDomainID int64
	AppID       string
	TemplateID  string
	SiteSlug    string
	RootPath    string
	PHPVersion  string
	CreatedAt   time.Time
}

type PHPPanelAccount struct {
	UserID         int64
	Enabled        bool
	SiteLimit      int
	DatabaseLimit  int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type PHPPanelDomainOwner struct {
	ID          int64
	AppDomainID int64
	AppID       string
	UserID       int64
	CreatedAt   time.Time
}

type PHPPanelDatabase struct {
	ID           int64
	AppID        string
	UserID       int64
	DatabaseName string
	CreatedAt    time.Time
}

type PHPPanelDBUser struct {
	ID                int64
	AppID             string
	UserID            int64
	Username          string
	Host              string
	PasswordEncrypted string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type PHPPanelDBGrant struct {
	ID            int64
	DBUserID       int64
	DatabaseName   string
	PrivilegesJSON string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type PHPPanelImpersonation struct {
	Token       string
	AdminUserID int64
	UserID      int64
	ExpiresAt   time.Time
	CreatedAt   time.Time
}

type AppDomainRoute struct {
	Priority int    `json:"priority"`
	Path     string `json:"path"`
	Root     string `json:"root"`
}

func (d AppDomain) RouteRules() []AppDomainRoute {
	var out []AppDomainRoute
	raw := strings.TrimSpace(d.RouteRulesJSON)
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	return out
}

// CaddyConfig holds global caddy settings stored in DB.
type CaddyConfig struct {
	Key   string
	Value string
}

// DeployLog is a single stored compose run output (last N kept per app).
type DeployLog struct {
	ID        int64
	Action    string
	OK        bool
	Output    string
	CreatedAt time.Time
}
