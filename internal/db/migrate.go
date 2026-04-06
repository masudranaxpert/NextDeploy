package db

import "strings"

// migrate runs all DDL migrations in dependency order.
// Rules:
//   - Always CREATE TABLE IF NOT EXISTS first (safe to re-run).
//   - ALTER TABLE for new columns after table creation (duplicate-column errors are silently ignored).
//   - Foreign-key parent tables MUST be created before child tables.
//   - Backfill INSERT/UPDATE statements run last so referenced rows already exist.
//
// Order of table creation:
//   apps → deploy_logs, caddy_config, settings
//   apps → app_domains → template_app_domains
//   apps → app_php_sites
//   apps → app_git_configs → webhook_deliveries
//   git_providers → github_provider_details
//   users → sessions
//   users → php_panel_accounts
//   users + app_domains → php_panel_domain_owners
//   users + apps → php_panel_databases, php_panel_db_users → php_panel_db_grants
//   users → php_panel_impersonations
func (s *Store) migrate() error {
	// ── core: apps ──────────────────────────────────────────────────────────────
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS apps (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  created_at TEXT NOT NULL
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_apps_created ON apps(created_at);`); err != nil {
		return err
	}
	for _, col := range []string{
		`ALTER TABLE apps ADD COLUMN compose_file  TEXT NOT NULL DEFAULT 'docker-compose.yml'`,
		`ALTER TABLE apps ADD COLUMN template_id   TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE apps ADD COLUMN source_type   TEXT NOT NULL DEFAULT 'files'`,
		`ALTER TABLE apps ADD COLUMN panel_env     TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := s.db.Exec(col); err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
				return err
			}
		}
	}

	// ── deploy_logs ──────────────────────────────────────────────────────────────
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS deploy_logs (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  app_id     TEXT NOT NULL,
  action     TEXT NOT NULL,
  ok         INTEGER NOT NULL,
  output     TEXT NOT NULL,
  created_at TEXT NOT NULL
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_deploy_logs_app ON deploy_logs(app_id, created_at);`); err != nil {
		return err
	}

	// ── caddy_config, settings ────────────────────────────────────────────────────
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS caddy_config (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);`); err != nil {
		return err
	}

	// ── app_domains (child of apps) ───────────────────────────────────────────────
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS app_domains (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  app_id        TEXT    NOT NULL,
  domain        TEXT    NOT NULL,
  service       TEXT    NOT NULL DEFAULT '',
  port          INTEGER NOT NULL DEFAULT 80,
  enable_https  INTEGER NOT NULL DEFAULT 1,
  enable_www    INTEGER NOT NULL DEFAULT 0,
  serve_static  INTEGER NOT NULL DEFAULT 0,
  static_path   TEXT    NOT NULL DEFAULT '',
  serve_media   INTEGER NOT NULL DEFAULT 0,
  media_path    TEXT    NOT NULL DEFAULT '',
  created_at    TEXT    NOT NULL,
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

	// ── app_php_sites (child of apps) ─────────────────────────────────────────────
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS app_php_sites (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  app_id      TEXT    NOT NULL,
  name        TEXT    NOT NULL,
  slug        TEXT    NOT NULL,
  php_version TEXT    NOT NULL DEFAULT '8.3',
  created_at  TEXT    NOT NULL,
  FOREIGN KEY(app_id) REFERENCES apps(id) ON DELETE CASCADE
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_app_php_sites_slug ON app_php_sites(app_id, slug);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`ALTER TABLE app_php_sites ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_app_php_sites_user ON app_php_sites(app_id, user_id);`); err != nil {
		return err
	}

	// ── template_app_domains (child of apps + app_domains) ───────────────────────
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS template_app_domains (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  app_domain_id INTEGER NOT NULL UNIQUE,
  app_id        TEXT    NOT NULL,
  template_id   TEXT    NOT NULL DEFAULT '',
  site_slug     TEXT    NOT NULL DEFAULT '',
  root_path     TEXT    NOT NULL DEFAULT '',
  php_version   TEXT    NOT NULL DEFAULT '',
  created_at    TEXT    NOT NULL,
  FOREIGN KEY(app_domain_id) REFERENCES app_domains(id) ON DELETE CASCADE,
  FOREIGN KEY(app_id)        REFERENCES apps(id)        ON DELETE CASCADE
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_template_app_domains_app ON template_app_domains(app_id);`); err != nil {
		return err
	}

	// ── git_providers ─────────────────────────────────────────────────────────────
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS git_providers (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  name       TEXT NOT NULL UNIQUE,
  provider   TEXT NOT NULL DEFAULT 'github',
  token      TEXT NOT NULL DEFAULT '',
  notes      TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`); err != nil {
		return err
	}

	// ── github_provider_details (child of git_providers) ─────────────────────────
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS github_provider_details (
  provider_id          INTEGER PRIMARY KEY,
  github_app_id        TEXT    NOT NULL DEFAULT '',
  client_id            TEXT    NOT NULL DEFAULT '',
  client_secret        TEXT    NOT NULL DEFAULT '',
  private_key_pem      TEXT    NOT NULL DEFAULT '',
  webhook_secret       TEXT    NOT NULL DEFAULT '',
  installation_id      TEXT    NOT NULL DEFAULT '',
  account_login        TEXT    NOT NULL DEFAULT '',
  app_slug             TEXT    NOT NULL DEFAULT '',
  manifest_state       TEXT    NOT NULL DEFAULT '',
  created_via_manifest INTEGER NOT NULL DEFAULT 0,
  created_at           TEXT    NOT NULL,
  updated_at           TEXT    NOT NULL,
  FOREIGN KEY(provider_id) REFERENCES git_providers(id) ON DELETE CASCADE
);`); err != nil {
		return err
	}

	// ── app_git_configs (child of apps) ──────────────────────────────────────────
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS app_git_configs (
  app_id           TEXT PRIMARY KEY,
  provider         TEXT    NOT NULL DEFAULT 'github',
  repo_url         TEXT    NOT NULL DEFAULT '',
  repo_full_name   TEXT    NOT NULL DEFAULT '',
  branch           TEXT    NOT NULL DEFAULT 'main',
  auth_mode        TEXT    NOT NULL DEFAULT 'public',
  token            TEXT    NOT NULL DEFAULT '',
  app_git_id       TEXT    NOT NULL DEFAULT '',
  installation_id  TEXT    NOT NULL DEFAULT '',
  private_key_pem  TEXT    NOT NULL DEFAULT '',
  webhook_secret   TEXT    NOT NULL DEFAULT '',
  auto_deploy      INTEGER NOT NULL DEFAULT 1,
  last_deploy_ref  TEXT    NOT NULL DEFAULT '',
  created_at       TEXT    NOT NULL,
  updated_at       TEXT    NOT NULL,
  FOREIGN KEY(app_id) REFERENCES apps(id) ON DELETE CASCADE
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_app_git_provider ON app_git_configs(provider);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`ALTER TABLE app_git_configs ADD COLUMN git_provider_id INTEGER NOT NULL DEFAULT 0`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}

	// ── webhook_deliveries (child of apps) ────────────────────────────────────────
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS webhook_deliveries (
  app_id      TEXT NOT NULL,
  delivery_id TEXT NOT NULL,
  created_at  TEXT NOT NULL,
  PRIMARY KEY(app_id, delivery_id),
  FOREIGN KEY(app_id) REFERENCES apps(id) ON DELETE CASCADE
);`); err != nil {
		return err
	}

	// ── users (independent root table) ───────────────────────────────────────────
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  username      TEXT NOT NULL UNIQUE COLLATE NOCASE,
  password_hash TEXT NOT NULL,
  role          TEXT NOT NULL DEFAULT 'user',
  created_at    TEXT NOT NULL
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);`); err != nil {
		return err
	}

	// ── sessions (child of users) ─────────────────────────────────────────────────
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS sessions (
  token      TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL,
  expires_at TEXT    NOT NULL,
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);`); err != nil {
		return err
	}

	// ── php_panel_accounts (child of users) ──────────────────────────────────────
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS php_panel_accounts (
  user_id        INTEGER PRIMARY KEY,
  enabled        INTEGER NOT NULL DEFAULT 0,
  site_limit     INTEGER NOT NULL DEFAULT 3,
  database_limit INTEGER NOT NULL DEFAULT 3,
  created_at     TEXT    NOT NULL,
  updated_at     TEXT    NOT NULL,
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);`); err != nil {
		return err
	}

	// ── php_panel_domain_owners (child of app_domains + apps + users) ─────────────
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS php_panel_domain_owners (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  app_domain_id INTEGER NOT NULL UNIQUE,
  app_id        TEXT    NOT NULL,
  user_id       INTEGER NOT NULL,
  created_at    TEXT    NOT NULL,
  FOREIGN KEY(app_domain_id) REFERENCES app_domains(id) ON DELETE CASCADE,
  FOREIGN KEY(app_id)        REFERENCES apps(id)        ON DELETE CASCADE,
  FOREIGN KEY(user_id)       REFERENCES users(id)       ON DELETE CASCADE
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_php_panel_domain_owners_user ON php_panel_domain_owners(app_id, user_id);`); err != nil {
		return err
	}

	// ── php_panel_databases (child of apps + users) ───────────────────────────────
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS php_panel_databases (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  app_id        TEXT    NOT NULL,
  user_id       INTEGER NOT NULL,
  database_name TEXT    NOT NULL,
  created_at    TEXT    NOT NULL,
  FOREIGN KEY(app_id)  REFERENCES apps(id)  ON DELETE CASCADE,
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_php_panel_databases_name ON php_panel_databases(app_id, database_name);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_php_panel_databases_user ON php_panel_databases(app_id, user_id);`); err != nil {
		return err
	}

	// ── php_panel_db_users (child of apps + users) ────────────────────────────────
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS php_panel_db_users (
  id                 INTEGER PRIMARY KEY AUTOINCREMENT,
  app_id             TEXT    NOT NULL,
  user_id            INTEGER NOT NULL,
  username           TEXT    NOT NULL,
  host               TEXT    NOT NULL,
  password_encrypted TEXT    NOT NULL DEFAULT '',
  created_at         TEXT    NOT NULL,
  updated_at         TEXT    NOT NULL,
  FOREIGN KEY(app_id)  REFERENCES apps(id)  ON DELETE CASCADE,
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_php_panel_db_users_unique ON php_panel_db_users(app_id, username, host);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_php_panel_db_users_user ON php_panel_db_users(app_id, user_id);`); err != nil {
		return err
	}

	// ── php_panel_db_grants (child of php_panel_db_users) ────────────────────────
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS php_panel_db_grants (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  db_user_id      INTEGER NOT NULL,
  database_name   TEXT    NOT NULL,
  privileges_json TEXT    NOT NULL DEFAULT '[]',
  created_at      TEXT    NOT NULL,
  updated_at      TEXT    NOT NULL,
  FOREIGN KEY(db_user_id) REFERENCES php_panel_db_users(id) ON DELETE CASCADE
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_php_panel_db_grants_unique ON php_panel_db_grants(db_user_id, database_name);`); err != nil {
		return err
	}

	// ── php_panel_impersonations (child of users x2) ─────────────────────────────
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS php_panel_impersonations (
  token        TEXT PRIMARY KEY,
  admin_user_id INTEGER NOT NULL,
  user_id       INTEGER NOT NULL,
  expires_at    TEXT    NOT NULL,
  created_at    TEXT    NOT NULL,
  FOREIGN KEY(admin_user_id) REFERENCES users(id) ON DELETE CASCADE,
  FOREIGN KEY(user_id)       REFERENCES users(id) ON DELETE CASCADE
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_php_panel_impersonations_exp ON php_panel_impersonations(expires_at);`); err != nil {
		return err
	}

	// ── backfill: ensure every user has a php_panel_accounts row ─────────────────
	if _, err := s.db.Exec(`
INSERT INTO php_panel_accounts(user_id, enabled, site_limit, database_limit, created_at, updated_at)
SELECT u.id,
       CASE WHEN u.role = 'admin' THEN 1 ELSE 0 END,
       3, 3,
       strftime('%Y-%m-%dT%H:%M:%fZ','now'),
       strftime('%Y-%m-%dT%H:%M:%fZ','now')
FROM users u
WHERE NOT EXISTS (SELECT 1 FROM php_panel_accounts a WHERE a.user_id = u.id);`); err != nil {
		return err
	}

	// ── backfill: assign orphan php sites to first admin ─────────────────────────
	if _, err := s.db.Exec(`
UPDATE app_php_sites
SET user_id = COALESCE(
  (SELECT id FROM users WHERE role = 'admin' ORDER BY id ASC LIMIT 1), 0)
WHERE user_id = 0;`); err != nil {
		return err
	}

	// ── backfill: assign orphan template domains to site owner / first admin ──────
	if _, err := s.db.Exec(`
INSERT INTO php_panel_domain_owners(app_domain_id, app_id, user_id, created_at)
SELECT tad.app_domain_id,
       tad.app_id,
       COALESCE(
         (SELECT aps.user_id FROM app_php_sites aps
          WHERE aps.app_id = tad.app_id AND aps.slug = tad.site_slug LIMIT 1),
         (SELECT id FROM users WHERE role = 'admin' ORDER BY id ASC LIMIT 1),
         0),
       strftime('%Y-%m-%dT%H:%M:%fZ','now')
FROM template_app_domains tad
WHERE NOT EXISTS (
  SELECT 1 FROM php_panel_domain_owners pdo WHERE pdo.app_domain_id = tad.app_domain_id);`); err != nil {
		return err
	}

	return nil
}
