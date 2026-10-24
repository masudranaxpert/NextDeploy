package db

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// User roles
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// User statuses
const (
	UserStatusActive    = "active"
	UserStatusSuspended = "suspended"
)

// App statuses
const (
	AppStatusActive    = "active"
	AppStatusSuspended = "suspended"
)

// Collaborator roles
const (
	CollabRoleDeveloper = "developer"
	CollabRoleViewer    = "viewer"
)

// User represents a panel user account.
type User struct {
	ID                    int64     `json:"id"`
	Username              string    `json:"username"`
	PasswordHash          string    `json:"-"`
	Role                  string    `json:"role"`
	CreatedAt             time.Time `json:"created_at"`
	MaxApps               int       `json:"max_apps"`
	MaxMemoryMB           int       `json:"max_memory_mb"`
	MaxCPUs               float64   `json:"max_cpus"`
	MaxStorageMB          int       `json:"max_storage_mb"`
	BandwidthLimitMbps    int       `json:"bandwidth_limit_mbps"`
	Status                string    `json:"status"`
	AllowDomainFileServer bool      `json:"allow_domain_file_server"`
}

type App struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"created_at"`
	ComposeFile string    `json:"compose_file"`
	OwnerID     int64     `json:"owner_id"`
	Status      string    `json:"status"`
	ActiveSlot  string    `json:"active_slot,omitempty"`
	StandbySlot string    `json:"standby_slot,omitempty"`
	PinnedSlot  string    `json:"pinned_slot,omitempty"`
}

type AppCollaborator struct {
	AppID       string
	UserID      int64
	Role        string
	Permissions string
	CreatedAt   time.Time
}

type PrivateRegistry struct {
	ID                int64
	UserID            *int64
	Name              string
	ServerAddress     string
	Username          string
	PasswordEncrypted string
	CreatedAt         time.Time
}

type AppGitConfig struct {
	AppID          string    `json:"app_id"`
	GitProviderID  int64     `json:"git_provider_id"`
	Provider       string    `json:"provider"`
	RepoURL        string    `json:"repo_url"`
	RepoFullName   string    `json:"repo_full_name"`
	Branch         string    `json:"branch"`
	AuthMode       string    `json:"auth_mode"`
	Token          string    `json:"token,omitempty"`
	AppGitID       string    `json:"app_git_id,omitempty"`
	InstallationID string    `json:"installation_id,omitempty"`
	PrivateKeyPEM  string    `json:"private_key_pem,omitempty"`
	WebhookSecret  string    `json:"webhook_secret"`
	AutoDeploy     bool      `json:"auto_deploy"`
	SyncPaused     bool      `json:"sync_paused"`
	LastDeployRef  string    `json:"last_deploy_ref,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// GitProvider holds a named global Git credential (token) for a provider.
type GitProvider struct {
	ID           int64     `json:"id"`
	UserID       *int64    `json:"user_id,omitempty"`
	Name         string    `json:"name"`
	Provider     string    `json:"provider"`
	Token        string    `json:"token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    int64     `json:"expires_at,omitempty"`
	Notes        string    `json:"notes,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
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
	ID              int64     `json:"id"`
	AppID           string    `json:"app_id"`
	Domain          string    `json:"domain"`
	Service         string    `json:"service"`
	Port            int       `json:"port"`
	IsPrimary       bool      `json:"is_primary"`
	EnableHTTPS     bool      `json:"enable_https"`
	EnableWWW       bool      `json:"enable_www"`
	ServeStatic     bool      `json:"serve_static"`
	StaticPath      string    `json:"static_path"`
	StaticURLPrefix string    `json:"static_url_prefix"`
	ServeMedia      bool      `json:"serve_media"`
	MediaPath       string    `json:"media_path"`
	MediaURLPrefix  string    `json:"media_url_prefix"`
	RouteRulesJSON  string    `json:"route_rules_json"`
	CreatedAt       time.Time `json:"created_at"`
}

// NormalizeCaddyPathMatcherFromURLPrefix builds a Caddy path matcher from a user-defined public prefix (e.g. /assets, files/uploads). No defaults.
func NormalizeCaddyPathMatcherFromURLPrefix(user string) (matcher string, ok bool) {
	s := strings.TrimSpace(user)
	if s == "" {
		return "", false
	}
	if !strings.HasPrefix(s, "/") {
		s = "/" + strings.TrimLeft(s, "/")
	}
	s = strings.TrimRight(s, "/")
	if s == "" || s == "/" {
		return "", false
	}
	return s + "/*", true
}

func formatURLPrefixForDisplay(user string) string {
	s := strings.TrimSpace(user)
	if s == "" {
		return ""
	}
	if !strings.HasPrefix(s, "/") {
		s = "/" + strings.TrimLeft(s, "/")
	}
	return strings.TrimRight(s, "/")
}

func (d AppDomain) DisplayStaticURLPrefix() string {
	return formatURLPrefixForDisplay(d.StaticURLPrefix)
}

func (d AppDomain) DisplayMediaURLPrefix() string {
	return formatURLPrefixForDisplay(d.MediaURLPrefix)
}

// ValidateAppDomainFileServing ensures URL prefix + filesystem root are set together when serving files (no hardcoded paths).
func ValidateAppDomainFileServing(d *AppDomain) error {
	if d == nil {
		return nil
	}
	if d.ServeStatic {
		if strings.TrimSpace(d.StaticPath) == "" {
			return errors.New("static filesystem root is required when static files are enabled")
		}
		if strings.TrimSpace(d.StaticURLPrefix) == "" {
			return errors.New("static URL prefix is required when static files are enabled")
		}
		if _, ok := NormalizeCaddyPathMatcherFromURLPrefix(d.StaticURLPrefix); !ok {
			return errors.New("invalid static URL prefix (use a path like /assets or /public/files)")
		}
	}
	if d.ServeMedia {
		if strings.TrimSpace(d.MediaPath) == "" {
			return errors.New("media filesystem root is required when user media is enabled")
		}
		if strings.TrimSpace(d.MediaURLPrefix) == "" {
			return errors.New("media URL prefix is required when user media is enabled")
		}
		if _, ok := NormalizeCaddyPathMatcherFromURLPrefix(d.MediaURLPrefix); !ok {
			return errors.New("invalid media URL prefix (use a path like /media or /uploads)")
		}
	}
	if d.ServeStatic && d.ServeMedia {
		ps, okS := NormalizeCaddyPathMatcherFromURLPrefix(d.StaticURLPrefix)
		pm, okM := NormalizeCaddyPathMatcherFromURLPrefix(d.MediaURLPrefix)
		if okS && okM && ps == pm {
			return errors.New("static and media cannot use the same public URL prefix; use different paths (e.g. /assets and /media)")
		}
	}
	return nil
}

type AppDomainRoute struct {
	Priority int    `json:"priority"`
	Path     string `json:"path"`
	Root     string `json:"root"`
	Direct   bool   `json:"direct,omitempty"`
}

func (r AppDomainRoute) EffectiveDirect() bool {
	return r.Direct
}

func (d AppDomain) EffectiveRouteRules() []AppDomainRoute {
	var out []AppDomainRoute
	p := 1
	if d.ServeStatic && strings.TrimSpace(d.StaticPath) != "" {
		path, ok := NormalizeCaddyPathMatcherFromURLPrefix(d.StaticURLPrefix)
		if ok {
			out = append(out, AppDomainRoute{Priority: p, Path: path, Root: strings.TrimSpace(d.StaticPath), Direct: true})
			p++
		}
	}
	if d.ServeMedia && strings.TrimSpace(d.MediaPath) != "" {
		path, ok := NormalizeCaddyPathMatcherFromURLPrefix(d.MediaURLPrefix)
		if ok {
			out = append(out, AppDomainRoute{Priority: p, Path: path, Root: strings.TrimSpace(d.MediaPath), Direct: true})
		}
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
	ID          int64     `json:"id"`
	Action      string    `json:"action"`
	OK          bool      `json:"ok"`
	Output      string    `json:"output"`
	Slot        string    `json:"slot,omitempty"`
	CommitSHA   string    `json:"commit_sha,omitempty"`
	ImageDigest string    `json:"image_digest,omitempty"`
	EnvSnapshot string    `json:"env_snapshot,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// AllocatedPort records a host port reserved by NextDeploy proxy gateway.
type AllocatedPort struct {
	Port      int       `json:"port"`
	BindIP    string    `json:"bind_ip"`
	AppID     string    `json:"app_id"`
	Service   string    `json:"service"`
	Protocol  string    `json:"protocol"`
	CreatedAt time.Time `json:"created_at"`
}

// BackupDestination represents a cloud storage destination for backups
type BackupDestination struct {
	ID        int64
	UserID    *int64
	Name      string
	Provider  string
	Config    string
	CreatedAt string
}

// BackupSchedule represents a scheduled backup configuration
type BackupSchedule struct {
	ID              int64
	AppID           string
	DestinationID   int64
	BackupType      string
	VolumeNames     string
	CronExpression  string
	RetentionCount  int
	Enabled         bool
	PauseContainers bool
	LastRun         string
	CreatedAt       string
}

// BackupHistory represents a completed backup
type BackupHistory struct {
	ID            int64
	AppID         string
	DestinationID int64
	BackupType    string
	VolumeName    string
	RemotePath    string
	Status        string
	ErrorMessage  string
	SizeBytes     int64
	Log           string
	CreatedAt     string
	// RemoteMissingCode: -1 unknown, 0 present on remote, 1 missing (cached; refreshed at most hourly).
	RemoteMissingCode int    `json:"-"`
	RemoteCheckedAt   string `json:"-"`
}

// ShouldRecheckRemotePresence returns true if cloud presence should be verified again (unknown or stale >1h).
func (h BackupHistory) ShouldRecheckRemotePresence(now time.Time) bool {
	if h.Status != "completed" || strings.TrimSpace(h.RemotePath) == "" {
		return false
	}
	if h.RemoteMissingCode < 0 || strings.TrimSpace(h.RemoteCheckedAt) == "" {
		return true
	}
	t, err := ParseRemoteCheckedAt(h.RemoteCheckedAt)
	if err != nil {
		return true
	}
	return now.Sub(t) >= time.Hour
}

// ParseRemoteCheckedAt parses SQLite datetime('now') values stored in backup_history.remote_checked_at.
func ParseRemoteCheckedAt(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, errors.New("empty remote_checked_at")
	}
	return time.ParseInLocation("2006-01-02 15:04:05", s, time.UTC)
}

type AuditLog struct {
	ID         int64     `json:"id"`
	UserID     int64     `json:"user_id"`
	Username   string    `json:"username"`
	Action     string    `json:"action"`
	TargetType string    `json:"target_type"`
	TargetID   string    `json:"target_id"`
	IPAddress  string    `json:"ip_address"`
	UserAgent  string    `json:"user_agent"`
	Details    string    `json:"details"`
	CreatedAt  time.Time `json:"created_at"`
}

// CLISession represents an active nd CLI connection.
type CLISession struct {
	ID          string
	UserID      int64
	Hostname    string
	OS          string
	Arch        string
	Version     string
	TokenName   string
	TokenPrefix string
	LastSeen    time.Time
	CreatedAt   time.Time
}

// IsActive returns true if the session sent a heartbeat within the last 5 minutes.
func (s CLISession) IsActive() bool {
	return time.Since(s.LastSeen) < 5*time.Minute
}

// LastSeenHuman returns a friendly relative timestamp.
func (s CLISession) LastSeenHuman() string {
	d := time.Since(s.LastSeen)
	if d < 0 || d < time.Minute {
		return "Just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return s.LastSeen.Format("Jan 2, 15:04")
}

type WebhookDelivery struct {
	AppID      string `json:"app_id"`
	DeliveryID string `json:"delivery_id"`
	Provider   string `json:"provider"`
	Repo       string `json:"repo"`
	Event      string `json:"event"`
	SHA        string `json:"sha"`
	Status     string `json:"status"`
	Latency    string `json:"latency"`
	CreatedAt  string `json:"created_at"`
}
