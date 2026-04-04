package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	_ "modernc.org/sqlite"
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
}

type AppGitConfig struct {
	AppID           string
	GitProviderID   int64
	Provider        string
	RepoURL         string
	RepoFullName    string
	Branch          string
	AuthMode        string
	Token           string
	AppGitID        string
	InstallationID  string
	PrivateKeyPEM   string
	WebhookSecret   string
	AutoDeploy      bool
	LastDeployRef   string
	CreatedAt       time.Time
	UpdatedAt       time.Time
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
	ProviderID        int64
	GitHubAppID       string
	ClientID          string
	ClientSecret      string
	PrivateKeyPEM     string
	WebhookSecret     string
	InstallationID    string
	AccountLogin      string
	AppSlug           string
	ManifestState     string
	CreatedViaManifest bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// AppDomain is a domain entry attached to an app with caddy routing config.
type AppDomain struct {
	ID            int64
	AppID         string
	Domain        string
	Service       string
	Port          int
	EnableHTTPS   bool
	EnableWWW     bool
	ServeStatic   bool
	StaticPath    string
	ServeMedia    bool
	MediaPath     string
	RouteRulesJSON string
	CreatedAt     time.Time
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

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	d, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(1)
	s := &Store{db: d}
	if err := s.migrate(); err != nil {
		_ = d.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS apps (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_at TEXT NOT NULL
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_apps_created ON apps(created_at);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`ALTER TABLE apps ADD COLUMN compose_file TEXT DEFAULT 'docker-compose.yml'`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS deploy_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  app_id TEXT NOT NULL,
  action TEXT NOT NULL,
  ok INTEGER NOT NULL,
  output TEXT NOT NULL,
  created_at TEXT NOT NULL
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_deploy_logs_app ON deploy_logs(app_id, created_at);`); err != nil {
		return err
	}

	// Caddy global config key-value store
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS caddy_config (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);`); err != nil {
		return err
	}

	// Generic application settings
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);`); err != nil {
		return err
	}

	// Per-app domain routing table
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS app_domains (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  app_id TEXT NOT NULL,
  domain TEXT NOT NULL,
  service TEXT NOT NULL DEFAULT '',
  port INTEGER NOT NULL DEFAULT 80,
  enable_https INTEGER NOT NULL DEFAULT 1,
  enable_www INTEGER NOT NULL DEFAULT 0,
  serve_static INTEGER NOT NULL DEFAULT 0,
  static_path TEXT NOT NULL DEFAULT '',
  serve_media INTEGER NOT NULL DEFAULT 0,
  media_path TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  FOREIGN KEY(app_id) REFERENCES apps(id) ON DELETE CASCADE
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_app_domains_app ON app_domains(app_id);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`ALTER TABLE app_domains ADD COLUMN route_rules_json TEXT NOT NULL DEFAULT '[]'`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}

	// Users table for authentication
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE COLLATE NOCASE,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'user',
  created_at TEXT NOT NULL
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);`); err != nil {
		return err
	}

	// Sessions table for cookie-based auth
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS sessions (
  token TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL,
  expires_at TEXT NOT NULL,
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);`); err != nil {
		return err
	}

	// Git-backed app source configuration
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS app_git_configs (
  app_id TEXT PRIMARY KEY,
  provider TEXT NOT NULL DEFAULT 'github',
  repo_url TEXT NOT NULL DEFAULT '',
  repo_full_name TEXT NOT NULL DEFAULT '',
  branch TEXT NOT NULL DEFAULT 'main',
  auth_mode TEXT NOT NULL DEFAULT 'public',
  token TEXT NOT NULL DEFAULT '',
  app_git_id TEXT NOT NULL DEFAULT '',
  installation_id TEXT NOT NULL DEFAULT '',
  private_key_pem TEXT NOT NULL DEFAULT '',
  webhook_secret TEXT NOT NULL DEFAULT '',
  auto_deploy INTEGER NOT NULL DEFAULT 1,
  last_deploy_ref TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(app_id) REFERENCES apps(id) ON DELETE CASCADE
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_app_git_provider ON app_git_configs(provider);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS webhook_deliveries (
  app_id TEXT NOT NULL,
  delivery_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY(app_id, delivery_id),
  FOREIGN KEY(app_id) REFERENCES apps(id) ON DELETE CASCADE
);`); err != nil {
		return err
	}

	// Global Git provider credentials (GitHub, GitLab, etc.)
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS git_providers (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  provider TEXT NOT NULL DEFAULT 'github',
  token TEXT NOT NULL DEFAULT '',
  notes TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS github_provider_details (
  provider_id INTEGER PRIMARY KEY,
  github_app_id TEXT NOT NULL DEFAULT '',
  client_id TEXT NOT NULL DEFAULT '',
  client_secret TEXT NOT NULL DEFAULT '',
  private_key_pem TEXT NOT NULL DEFAULT '',
  webhook_secret TEXT NOT NULL DEFAULT '',
  installation_id TEXT NOT NULL DEFAULT '',
  account_login TEXT NOT NULL DEFAULT '',
  app_slug TEXT NOT NULL DEFAULT '',
  manifest_state TEXT NOT NULL DEFAULT '',
  created_via_manifest INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(provider_id) REFERENCES git_providers(id) ON DELETE CASCADE
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`ALTER TABLE app_git_configs ADD COLUMN git_provider_id INTEGER NOT NULL DEFAULT 0`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}

	// Source type column for apps (files or git)
	if _, err := s.db.Exec(`ALTER TABLE apps ADD COLUMN source_type TEXT NOT NULL DEFAULT 'files'`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}

	return nil
}

// ── Caddy config ──────────────────────────────────────────────────────────────

func (s *Store) GetCaddyConfig(ctx context.Context, key string) string {
	var v string
	_ = s.db.QueryRowContext(ctx, `SELECT value FROM caddy_config WHERE key = ?`, key).Scan(&v)
	return v
}

func (s *Store) SetCaddyConfig(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO caddy_config(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value)
	return err
}

func (s *Store) GetAllCaddyConfig(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM caddy_config`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (s *Store) GetSetting(ctx context.Context, key string) string {
	var v string
	_ = s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	return v
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value)
	return err
}

func (s *Store) GetAllSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// ── App domains ───────────────────────────────────────────────────────────────

func (s *Store) ListAppDomains(ctx context.Context, appID string) ([]AppDomain, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,app_id,domain,service,port,enable_https,enable_www,serve_static,static_path,serve_media,media_path,COALESCE(route_rules_json,'[]'),created_at
		 FROM app_domains WHERE app_id=? ORDER BY id ASC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AppDomain
	for rows.Next() {
		var d AppDomain
		var https, www, static, media int
		var created string
		if err := rows.Scan(&d.ID, &d.AppID, &d.Domain, &d.Service, &d.Port,
			&https, &www, &static, &d.StaticPath, &media, &d.MediaPath, &d.RouteRulesJSON, &created); err != nil {
			return nil, err
		}
		d.EnableHTTPS = https != 0
		d.EnableWWW = www != 0
		d.ServeStatic = static != 0
		d.ServeMedia = media != 0
		t, _ := time.Parse(time.RFC3339, created)
		d.CreatedAt = t
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) GetAppDomain(ctx context.Context, id int64) (AppDomain, error) {
	var d AppDomain
	var https, www, static, media int
	var created string
	err := s.db.QueryRowContext(ctx,
		`SELECT id,app_id,domain,service,port,enable_https,enable_www,serve_static,static_path,serve_media,media_path,COALESCE(route_rules_json,'[]'),created_at
		 FROM app_domains WHERE id=?`, id).Scan(
		&d.ID, &d.AppID, &d.Domain, &d.Service, &d.Port,
		&https, &www, &static, &d.StaticPath, &media, &d.MediaPath, &d.RouteRulesJSON, &created)
	if err != nil {
		return AppDomain{}, err
	}
	d.EnableHTTPS = https != 0
	d.EnableWWW = www != 0
	d.ServeStatic = static != 0
	d.ServeMedia = media != 0
	t, _ := time.Parse(time.RFC3339, created)
	d.CreatedAt = t
	return d, nil
}

func (s *Store) CreateAppDomain(ctx context.Context, d AppDomain) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO app_domains(app_id,domain,service,port,enable_https,enable_www,serve_static,static_path,serve_media,media_path,route_rules_json,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.AppID, d.Domain, d.Service, d.Port,
		boolInt(d.EnableHTTPS), boolInt(d.EnableWWW),
		boolInt(d.ServeStatic), d.StaticPath,
		boolInt(d.ServeMedia), d.MediaPath, normalizeRulesJSON(d.RouteRulesJSON),
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateAppDomain(ctx context.Context, d AppDomain) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE app_domains SET domain=?,service=?,port=?,enable_https=?,enable_www=?,serve_static=?,static_path=?,serve_media=?,media_path=?,route_rules_json=?
		 WHERE id=?`,
		d.Domain, d.Service, d.Port,
		boolInt(d.EnableHTTPS), boolInt(d.EnableWWW),
		boolInt(d.ServeStatic), d.StaticPath,
		boolInt(d.ServeMedia), d.MediaPath, normalizeRulesJSON(d.RouteRulesJSON),
		d.ID)
	return err
}

func (s *Store) DeleteAppDomain(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM app_domains WHERE id=?`, id)
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func normalizeRulesJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "[]"
	}
	var out []AppDomainRoute
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return "[]"
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func (s *Store) CreateApp(ctx context.Context, id, name string) error {
	if id == "" || name == "" {
		return errors.New("invalid app")
	}
	exists, err := s.AppNameExists(ctx, name)
	if err != nil {
		return err
	}
	if exists {
		return errors.New("app name already exists")
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO apps (id, name, created_at, compose_file) VALUES (?, ?, ?, ?)`,
		id, name, time.Now().UTC().Format(time.RFC3339), "docker-compose.yml")
	return err
}

func (s *Store) AppNameExists(ctx context.Context, name string) (bool, error) {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return false, nil
	}
	var found string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM apps WHERE lower(name) = ? LIMIT 1`, name).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return found != "", nil
}

func (s *Store) ListApps(ctx context.Context) ([]App, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, created_at, COALESCE(compose_file,'') FROM apps ORDER BY datetime(created_at) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []App
	for rows.Next() {
		var a App
		var created string
		if err := rows.Scan(&a.ID, &a.Name, &created, &a.ComposeFile); err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339, created)
		if err != nil {
			t = time.Time{}
		}
		a.CreatedAt = t
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) GetApp(ctx context.Context, id string) (App, error) {
	var a App
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT id, name, created_at, COALESCE(compose_file,'') FROM apps WHERE id = ?`, id).Scan(&a.ID, &a.Name, &created, &a.ComposeFile)
	if err != nil {
		return App{}, err
	}
	t, _ := time.Parse(time.RFC3339, created)
	a.CreatedAt = t
	if strings.TrimSpace(a.ComposeFile) == "" {
		a.ComposeFile = "docker-compose.yml"
	}
	return a, nil
}

func (s *Store) UpdateComposeFile(ctx context.Context, id, composeFile string) error {
	composeFile = strings.TrimSpace(composeFile)
	if composeFile == "" {
		composeFile = "docker-compose.yml"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE apps SET compose_file = ? WHERE id = ?`, composeFile, id)
	return err
}

func (s *Store) InsertDeployLog(ctx context.Context, appID, action string, ok bool, output string) error {
	if appID == "" || action == "" {
		return errors.New("invalid deploy log")
	}
	const maxOut = 65536
	if len(output) > maxOut {
		output = output[:maxOut] + "\n… (truncated)"
	}
	okInt := 0
	if ok {
		okInt = 1
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO deploy_logs (app_id, action, ok, output, created_at) VALUES (?, ?, ?, ?, ?)`,
		appID, action, okInt, output, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return err
	}
	return s.trimDeployLogs(ctx, appID, 5)
}

func (s *Store) trimDeployLogs(ctx context.Context, appID string, keep int) error {
	_, err := s.db.ExecContext(ctx, `
DELETE FROM deploy_logs WHERE app_id = ? AND id NOT IN (
  SELECT id FROM (
    SELECT id FROM deploy_logs WHERE app_id = ? ORDER BY datetime(created_at) DESC, id DESC LIMIT ?
  )
)`, appID, appID, keep)
	return err
}

func (s *Store) ListDeployLogs(ctx context.Context, appID string, limit int) ([]DeployLog, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, action, ok, output, created_at FROM deploy_logs WHERE app_id = ? ORDER BY datetime(created_at) DESC, id DESC LIMIT ?`,
		appID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeployLog
	for rows.Next() {
		var d DeployLog
		var ok int
		var created string
		if err := rows.Scan(&d.ID, &d.Action, &ok, &d.Output, &created); err != nil {
			return nil, err
		}
		d.OK = ok != 0
		t, _ := time.Parse(time.RFC3339, created)
		d.CreatedAt = t
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) ClearDeployLogs(ctx context.Context, appID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM deploy_logs WHERE app_id = ?`, appID)
	return err
}

func (s *Store) DeleteApp(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("invalid id")
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM webhook_deliveries WHERE app_id = ?`, id); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM app_git_configs WHERE app_id = ?`, id); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM app_domains WHERE app_id = ?`, id); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM deploy_logs WHERE app_id = ?`, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM apps WHERE id = ?`, id)
	return err
}

// ── App git config ───────────────────────────────────────────────────────────

func (s *Store) GetAppGitConfig(ctx context.Context, appID string) (AppGitConfig, error) {
	var g AppGitConfig
	var auto int
	var created, updated string
	err := s.db.QueryRowContext(ctx, `
SELECT app_id, COALESCE(git_provider_id,0), provider, repo_url, repo_full_name, branch, auth_mode, token,
       app_git_id, installation_id, private_key_pem, webhook_secret, auto_deploy,
       last_deploy_ref, created_at, updated_at
FROM app_git_configs WHERE app_id = ?`, appID).Scan(
		&g.AppID, &g.GitProviderID, &g.Provider, &g.RepoURL, &g.RepoFullName, &g.Branch, &g.AuthMode, &g.Token,
		&g.AppGitID, &g.InstallationID, &g.PrivateKeyPEM, &g.WebhookSecret, &auto,
		&g.LastDeployRef, &created, &updated,
	)
	if err != nil {
		return AppGitConfig{}, err
	}
	g.AutoDeploy = auto != 0
	g.CreatedAt, _ = time.Parse(time.RFC3339, created)
	g.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return g, nil
}

func (s *Store) UpsertAppGitConfig(ctx context.Context, g AppGitConfig) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(g.Provider) == "" {
		g.Provider = "github"
	}
	if strings.TrimSpace(g.Branch) == "" {
		g.Branch = "main"
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO app_git_configs (
  app_id, git_provider_id, provider, repo_url, repo_full_name, branch, auth_mode, token,
  app_git_id, installation_id, private_key_pem, webhook_secret, auto_deploy,
  last_deploy_ref, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(app_id) DO UPDATE SET
  git_provider_id=excluded.git_provider_id,
  provider=excluded.provider,
  repo_url=excluded.repo_url,
  repo_full_name=excluded.repo_full_name,
  branch=excluded.branch,
  auth_mode=excluded.auth_mode,
  token=excluded.token,
  app_git_id=excluded.app_git_id,
  installation_id=excluded.installation_id,
  private_key_pem=excluded.private_key_pem,
  webhook_secret=excluded.webhook_secret,
  auto_deploy=excluded.auto_deploy,
  last_deploy_ref=excluded.last_deploy_ref,
  updated_at=excluded.updated_at`,
		g.AppID, g.GitProviderID, g.Provider, g.RepoURL, g.RepoFullName, g.Branch, g.AuthMode, g.Token,
		g.AppGitID, g.InstallationID, g.PrivateKeyPEM, g.WebhookSecret, boolInt(g.AutoDeploy),
		g.LastDeployRef, now, now,
	)
	return err
}

func (s *Store) DeleteAppGitConfig(ctx context.Context, appID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM app_git_configs WHERE app_id = ?`, appID)
	return err
}

func (s *Store) ListGitApps(ctx context.Context) ([]AppGitConfig, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT app_id, COALESCE(git_provider_id,0), provider, repo_url, repo_full_name, branch, auth_mode, token,
       app_git_id, installation_id, private_key_pem, webhook_secret, auto_deploy,
       last_deploy_ref, created_at, updated_at
FROM app_git_configs ORDER BY datetime(updated_at) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AppGitConfig
	for rows.Next() {
		var g AppGitConfig
		var auto int
		var created, updated string
		if err := rows.Scan(
			&g.AppID, &g.GitProviderID, &g.Provider, &g.RepoURL, &g.RepoFullName, &g.Branch, &g.AuthMode, &g.Token,
			&g.AppGitID, &g.InstallationID, &g.PrivateKeyPEM, &g.WebhookSecret, &auto,
			&g.LastDeployRef, &created, &updated,
		); err != nil {
			return nil, err
		}
		g.AutoDeploy = auto != 0
		g.CreatedAt, _ = time.Parse(time.RFC3339, created)
		g.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) MarkWebhookDelivery(ctx context.Context, appID, deliveryID string) (bool, error) {
	if strings.TrimSpace(appID) == "" || strings.TrimSpace(deliveryID) == "" {
		return false, errors.New("invalid webhook delivery")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO webhook_deliveries(app_id, delivery_id, created_at) VALUES (?, ?, ?)`,
		appID, deliveryID, time.Now().UTC().Format(time.RFC3339))
	if err == nil {
		return true, nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "unique") {
		return false, nil
	}
	return false, err
}

// ── Users ─────────────────────────────────────────────────────────────────────

func (s *Store) UserCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(ctx context.Context, username, passwordHash, role string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users(username,password_hash,role,created_at) VALUES(?,?,?,?)`,
		username, passwordHash, role, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (User, error) {
	var u User
	var created string
	err := s.db.QueryRowContext(ctx,
		`SELECT id,username,password_hash,role,created_at FROM users WHERE username=? COLLATE NOCASE`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &created)
	if err != nil {
		return User{}, err
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return u, nil
}

func (s *Store) GetUserByID(ctx context.Context, id int64) (User, error) {
	var u User
	var created string
	err := s.db.QueryRowContext(ctx,
		`SELECT id,username,password_hash,role,created_at FROM users WHERE id=?`, id).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &created)
	if err != nil {
		return User{}, err
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return u, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,username,password_hash,role,created_at FROM users ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		var created string
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &created); err != nil {
			return nil, err
		}
		u.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) UpdateUserPassword(ctx context.Context, id int64, passwordHash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=?`, passwordHash, id)
	return err
}

func (s *Store) UpdateUserRole(ctx context.Context, id int64, role string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET role=? WHERE id=?`, role, id)
	return err
}

func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id=?`, id)
	return err
}

// ── Sessions ──────────────────────────────────────────────────────────────────

func (s *Store) CreateSession(ctx context.Context, token string, userID int64, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions(token,user_id,expires_at) VALUES(?,?,?)`,
		token, userID, expiresAt.UTC().Format(time.RFC3339))
	return err
}

func (s *Store) GetSession(ctx context.Context, token string) (userID int64, expiresAt time.Time, err error) {
	var expiresStr string
	err = s.db.QueryRowContext(ctx,
		`SELECT user_id,expires_at FROM sessions WHERE token=?`, token).
		Scan(&userID, &expiresStr)
	if err != nil {
		return 0, time.Time{}, err
	}
	expiresAt, _ = time.Parse(time.RFC3339, expiresStr)
	return userID, expiresAt, nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token=?`, token)
	return err
}

func (s *Store) PruneExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at < ?`, time.Now().UTC().Format(time.RFC3339))
	return err
}

// ── Git Providers ─────────────────────────────────────────────────────────────

func (s *Store) ListGitProviders(ctx context.Context) ([]GitProvider, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,provider,token,notes,created_at,updated_at FROM git_providers ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GitProvider
	for rows.Next() {
		var p GitProvider
		var ca, ua string
		if err := rows.Scan(&p.ID, &p.Name, &p.Provider, &p.Token, &p.Notes, &ca, &ua); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339, ca)
		p.UpdatedAt, _ = time.Parse(time.RFC3339, ua)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetGitProvider(ctx context.Context, id int64) (GitProvider, error) {
	var p GitProvider
	var ca, ua string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,provider,token,notes,created_at,updated_at FROM git_providers WHERE id=?`, id).
		Scan(&p.ID, &p.Name, &p.Provider, &p.Token, &p.Notes, &ca, &ua)
	if err != nil {
		return GitProvider{}, err
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, ca)
	p.UpdatedAt, _ = time.Parse(time.RFC3339, ua)
	return p, nil
}

func (s *Store) CreateGitProvider(ctx context.Context, name, provider, token, notes string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO git_providers(name,provider,token,notes,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
		name, provider, token, notes, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateGitProvider(ctx context.Context, id int64, name, provider, token, notes string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE git_providers SET name=?,provider=?,token=?,notes=?,updated_at=? WHERE id=?`,
		name, provider, token, notes, now, id)
	return err
}

func (s *Store) DeleteGitProvider(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM git_providers WHERE id=?`, id)
	return err
}

func (s *Store) GetGitHubProviderDetail(ctx context.Context, providerID int64) (GitHubProviderDetail, error) {
	var d GitHubProviderDetail
	var created, updated string
	var createdVia int
	err := s.db.QueryRowContext(ctx, `
SELECT provider_id, github_app_id, client_id, client_secret, private_key_pem, webhook_secret,
       installation_id, account_login, app_slug, manifest_state, created_via_manifest, created_at, updated_at
FROM github_provider_details WHERE provider_id=?`, providerID).Scan(
		&d.ProviderID, &d.GitHubAppID, &d.ClientID, &d.ClientSecret, &d.PrivateKeyPEM, &d.WebhookSecret,
		&d.InstallationID, &d.AccountLogin, &d.AppSlug, &d.ManifestState, &createdVia, &created, &updated,
	)
	if err != nil {
		return GitHubProviderDetail{}, err
	}
	d.CreatedViaManifest = createdVia != 0
	d.CreatedAt, _ = time.Parse(time.RFC3339, created)
	d.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return d, nil
}

func (s *Store) GetGitHubProviderDetailByManifestState(ctx context.Context, state string) (GitHubProviderDetail, error) {
	var d GitHubProviderDetail
	var created, updated string
	var createdVia int
	err := s.db.QueryRowContext(ctx, `
SELECT provider_id, github_app_id, client_id, client_secret, private_key_pem, webhook_secret,
       installation_id, account_login, app_slug, manifest_state, created_via_manifest, created_at, updated_at
FROM github_provider_details WHERE manifest_state=?`, state).Scan(
		&d.ProviderID, &d.GitHubAppID, &d.ClientID, &d.ClientSecret, &d.PrivateKeyPEM, &d.WebhookSecret,
		&d.InstallationID, &d.AccountLogin, &d.AppSlug, &d.ManifestState, &createdVia, &created, &updated,
	)
	if err != nil {
		return GitHubProviderDetail{}, err
	}
	d.CreatedViaManifest = createdVia != 0
	d.CreatedAt, _ = time.Parse(time.RFC3339, created)
	d.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return d, nil
}

func (s *Store) UpsertGitHubProviderDetail(ctx context.Context, d GitHubProviderDetail) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO github_provider_details (
  provider_id, github_app_id, client_id, client_secret, private_key_pem, webhook_secret,
  installation_id, account_login, app_slug, manifest_state, created_via_manifest, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(provider_id) DO UPDATE SET
  github_app_id=excluded.github_app_id,
  client_id=excluded.client_id,
  client_secret=excluded.client_secret,
  private_key_pem=excluded.private_key_pem,
  webhook_secret=excluded.webhook_secret,
  installation_id=excluded.installation_id,
  account_login=excluded.account_login,
  app_slug=excluded.app_slug,
  manifest_state=excluded.manifest_state,
  created_via_manifest=excluded.created_via_manifest,
  updated_at=excluded.updated_at`,
		d.ProviderID, d.GitHubAppID, d.ClientID, d.ClientSecret, d.PrivateKeyPEM, d.WebhookSecret,
		d.InstallationID, d.AccountLogin, d.AppSlug, d.ManifestState, boolInt(d.CreatedViaManifest), now, now,
	)
	return err
}

func (s *Store) ListGitHubProviderDetails(ctx context.Context) ([]GitHubProviderDetail, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT provider_id, github_app_id, client_id, client_secret, private_key_pem, webhook_secret,
       installation_id, account_login, app_slug, manifest_state, created_via_manifest, created_at, updated_at
FROM github_provider_details ORDER BY provider_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GitHubProviderDetail
	for rows.Next() {
		var d GitHubProviderDetail
		var created, updated string
		var createdVia int
		if err := rows.Scan(
			&d.ProviderID, &d.GitHubAppID, &d.ClientID, &d.ClientSecret, &d.PrivateKeyPEM, &d.WebhookSecret,
			&d.InstallationID, &d.AccountLogin, &d.AppSlug, &d.ManifestState, &createdVia, &created, &updated,
		); err != nil {
			return nil, err
		}
		d.CreatedViaManifest = createdVia != 0
		d.CreatedAt, _ = time.Parse(time.RFC3339, created)
		d.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetAppSourceType returns the source type for an app ("files" or "git").
func (s *Store) GetAppSourceType(ctx context.Context, appID string) string {
	var v string
	_ = s.db.QueryRowContext(ctx, `SELECT source_type FROM apps WHERE id=?`, appID).Scan(&v)
	if v == "" {
		return "files"
	}
	return v
}

// SetAppSourceType updates the source_type for an app.
func (s *Store) SetAppSourceType(ctx context.Context, appID, sourceType string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE apps SET source_type=? WHERE id=?`, sourceType, appID)
	return err
}
