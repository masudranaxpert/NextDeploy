package migrate

import "time"

type BundleManifest struct {
	Version     int               `json:"version"`
	Type        string            `json:"type"`
	ExportedAt  string            `json:"exported_at"`
	ExportID    int64             `json:"export_id,omitempty"`
	AppIDs      []string          `json:"app_ids"`
	Checksums   map[string]string `json:"checksums,omitempty"`
	Snapshot    string            `json:"snapshot"`
	AppsArchive string            `json:"apps_dir"`
}

type PanelSnapshot struct {
	Apps          []AppSnapshot          `json:"apps"`
	Users         []UserSnapshot         `json:"users,omitempty"`
	GitProviders  []GitProviderSnapshot  `json:"git_providers,omitempty"`
	Collaborators []CollaboratorSnapshot `json:"collaborators,omitempty"`
	Domains       []DomainSnapshot       `json:"domains"`
	GitConfigs    []GitSnapshot          `json:"git_configs,omitempty"`
	PanelEnvs     map[string]string      `json:"panel_envs,omitempty"`
	Registries    []RegistrySnapshot     `json:"registries,omitempty"`
	SourcePanel   string                 `json:"source_panel,omitempty"`
}

type UserSnapshot struct {
	ID                    int64   `json:"id"`
	Username              string  `json:"username"`
	PasswordHash          string  `json:"password_hash"`
	Role                  string  `json:"role"`
	CreatedAt             string  `json:"created_at"`
	MaxApps               int     `json:"max_apps"`
	MaxMemoryMB           int     `json:"max_memory_mb"`
	MaxCPUs               float64 `json:"max_cpus"`
	MaxStorageMB          int     `json:"max_storage_mb"`
	Status                string  `json:"status"`
	AllowDomainFileServer bool    `json:"allow_domain_file_server"`
}

type GitProviderSnapshot struct {
	ID           int64                  `json:"id"`
	UserID       *int64                 `json:"user_id,omitempty"`
	Name         string                 `json:"name"`
	Provider     string                 `json:"provider"`
	Token        string                 `json:"token"`
	RefreshToken string                 `json:"refresh_token,omitempty"`
	ExpiresAt    int64                  `json:"expires_at,omitempty"`
	Notes        string                 `json:"notes,omitempty"`
	CreatedAt    string                 `json:"created_at"`
	UpdatedAt    string                 `json:"updated_at"`
	GitHubDetail *GitHubProviderSnap    `json:"github_detail,omitempty"`
}

type GitHubProviderSnap struct {
	GitHubAppID        string `json:"github_app_id"`
	ClientID           string `json:"client_id"`
	ClientSecret       string `json:"client_secret"`
	PrivateKeyPEM      string `json:"private_key_pem"`
	WebhookSecret      string `json:"webhook_secret"`
	InstallationID     string `json:"installation_id"`
	AccountLogin       string `json:"account_login"`
	AppSlug            string `json:"app_slug"`
	ManifestState      string `json:"manifest_state"`
	CreatedViaManifest bool   `json:"created_via_manifest"`
}

type CollaboratorSnapshot struct {
	AppID  string `json:"app_id"`
	UserID int64  `json:"user_id"`
	Role   string `json:"role"`
}

type AppSnapshot struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ComposeFile string `json:"compose_file"`
	OwnerID     int64  `json:"owner_id"`
	Status      string `json:"status"`
	SourceType  string `json:"source_type,omitempty"`
	Archive     string `json:"archive"`
}

type DomainSnapshot struct {
	AppID           string `json:"app_id"`
	Domain          string `json:"domain"`
	Service         string `json:"service"`
	Port            int    `json:"port"`
	EnableHTTPS     bool   `json:"enable_https"`
	EnableWWW       bool   `json:"enable_www"`
	ServeStatic     bool   `json:"serve_static"`
	StaticPath      string `json:"static_path"`
	StaticURLPrefix string `json:"static_url_prefix"`
	ServeMedia      bool   `json:"serve_media"`
	MediaPath       string `json:"media_path"`
	MediaURLPrefix  string `json:"media_url_prefix"`
	RouteRulesJSON  string `json:"route_rules_json"`
}

type GitSnapshot struct {
	AppID          string `json:"app_id"`
	GitProviderID  int64  `json:"git_provider_id"`
	Provider       string `json:"provider"`
	RepoURL        string `json:"repo_url"`
	RepoFullName   string `json:"repo_full_name"`
	Branch         string `json:"branch"`
	AuthMode       string `json:"auth_mode"`
	Token          string `json:"token,omitempty"`
	AppGitID       string `json:"app_git_id,omitempty"`
	InstallationID string `json:"installation_id,omitempty"`
	PrivateKeyPEM  string `json:"private_key_pem,omitempty"`
	WebhookSecret  string `json:"webhook_secret,omitempty"`
	AutoDeploy     bool   `json:"auto_deploy"`
	LastDeployRef  string `json:"last_deploy_ref,omitempty"`
}

type RegistrySnapshot struct {
	UserID            *int64 `json:"user_id,omitempty"`
	Name              string `json:"name"`
	ServerAddress     string `json:"server_address"`
	Username          string `json:"username"`
	PasswordEncrypted string `json:"password_encrypted"`
}

func newBundleManifest(exportID int64, appIDs []string) BundleManifest {
	return BundleManifest{
		Version:     BundleVersion,
		Type:        BundleType,
		ExportedAt:  time.Now().UTC().Format(time.RFC3339),
		ExportID:    exportID,
		AppIDs:      appIDs,
		Checksums:   map[string]string{},
		Snapshot:    snapshotName,
		AppsArchive: appsDirName + "/",
	}
}
