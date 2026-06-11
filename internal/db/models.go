package db

import (
	"errors"
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
	ID           int64
	Username     string
	PasswordHash string
	Role         string
	CreatedAt    time.Time
	MaxApps      int
	MaxMemoryMB  int
	MaxCPUs      float64
	Status       string
}

type App struct {
	ID          string
	Name        string
	CreatedAt   time.Time
	ComposeFile string
	OwnerID     int64
	Status      string
}

type AppCollaborator struct {
	AppID     string
	UserID    int64
	Role      string
	CreatedAt time.Time
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
	ID           int64
	Name         string
	Provider     string
	Token        string
	RefreshToken string
	ExpiresAt    int64 // Unix timestamp
	Notes        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
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
	ID              int64
	AppID           string
	Domain          string
	Service         string
	Port            int
	EnableHTTPS     bool
	EnableWWW       bool
	ServeStatic     bool
	StaticPath      string
	StaticURLPrefix string
	ServeMedia      bool
	MediaPath       string
	MediaURLPrefix  string
	RouteRulesJSON  string
	CreatedAt       time.Time
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
	ID        int64
	Action    string
	OK        bool
	Output    string
	CreatedAt time.Time
}

// BackupDestination represents a cloud storage destination for backups
type BackupDestination struct {
	ID        int64
	Name      string
	Provider  string
	Config    string
	CreatedAt string
}

// BackupSchedule represents a scheduled backup configuration
type BackupSchedule struct {
	ID             int64
	AppID          string
	DestinationID  int64
	BackupType     string
	VolumeNames    string
	CronExpression string
	RetentionCount int
	Enabled        bool
	LastRun        string
	CreatedAt      string
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
	ID         int64
	UserID     int64
	Username   string
	Action     string
	TargetType string
	TargetID   string
	IPAddress  string
	UserAgent  string
	Details    string
	CreatedAt  time.Time
}
