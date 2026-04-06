package db

import "strings"

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

	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS caddy_config (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);`); err != nil {
		return err
	}

	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);`); err != nil {
		return err
	}

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

	if _, err := s.db.Exec(`ALTER TABLE apps ADD COLUMN source_type TEXT NOT NULL DEFAULT 'files'`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}

	if _, err := s.db.Exec(`ALTER TABLE apps ADD COLUMN panel_env TEXT NOT NULL DEFAULT ''`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}

	return nil
}
