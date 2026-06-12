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
	// Add refresh_token and expires_at columns for OAuth token refresh (GitLab)
	if _, err := s.db.Exec(`ALTER TABLE git_providers ADD COLUMN refresh_token TEXT NOT NULL DEFAULT ''`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	if _, err := s.db.Exec(`ALTER TABLE git_providers ADD COLUMN expires_at INTEGER NOT NULL DEFAULT 0`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	if _, err := s.db.Exec(`ALTER TABLE git_providers ADD COLUMN user_id INTEGER REFERENCES users(id) ON DELETE CASCADE`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
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

	if _, err := s.db.Exec(`ALTER TABLE app_domains ADD COLUMN static_url_prefix TEXT NOT NULL DEFAULT '/static'`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	if _, err := s.db.Exec(`ALTER TABLE app_domains ADD COLUMN media_url_prefix TEXT NOT NULL DEFAULT '/media'`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}

	// Backup system tables
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS backup_destinations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  provider TEXT NOT NULL,
  config TEXT NOT NULL DEFAULT '{}',
  is_default INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`ALTER TABLE backup_destinations ADD COLUMN user_id INTEGER REFERENCES users(id) ON DELETE CASCADE`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}

	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS backup_schedules (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  app_id TEXT NOT NULL,
  destination_id INTEGER NOT NULL,
  backup_type TEXT NOT NULL,
  volume_names TEXT NOT NULL DEFAULT '',
  cron_schedule TEXT NOT NULL DEFAULT '',
  retention_count INTEGER NOT NULL DEFAULT 5,
  enabled INTEGER NOT NULL DEFAULT 1,
  last_run_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(app_id) REFERENCES apps(id) ON DELETE CASCADE,
  FOREIGN KEY(destination_id) REFERENCES backup_destinations(id) ON DELETE CASCADE
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_backup_schedules_app ON backup_schedules(app_id);`); err != nil {
		return err
	}

	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS backup_history (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  app_id TEXT NOT NULL,
  destination_id INTEGER NOT NULL,
  backup_type TEXT NOT NULL,
  volume_name TEXT NOT NULL DEFAULT '',
  remote_path TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  error_message TEXT NOT NULL DEFAULT '',
  size_bytes INTEGER NOT NULL DEFAULT 0,
  log TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  FOREIGN KEY(app_id) REFERENCES apps(id) ON DELETE CASCADE,
  FOREIGN KEY(destination_id) REFERENCES backup_destinations(id) ON DELETE CASCADE
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_backup_history_app ON backup_history(app_id, created_at);`); err != nil {
		return err
	}

	// Check if backup_history has old schema and migrate
	var hasFileNameColumn int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('backup_history') WHERE name = 'file_name'`).Scan(&hasFileNameColumn)
	
	if hasFileNameColumn > 0 {
		// Old schema detected - drop and recreate (safe since backup feature is new)
		_, _ = s.db.Exec(`DROP TABLE IF EXISTS backup_history`)
		_, _ = s.db.Exec(`
			CREATE TABLE backup_history (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				app_id TEXT NOT NULL,
				destination_id INTEGER NOT NULL,
				backup_type TEXT NOT NULL,
				remote_path TEXT NOT NULL DEFAULT '',
				status TEXT NOT NULL,
				error_message TEXT NOT NULL DEFAULT '',
				size_bytes INTEGER NOT NULL DEFAULT 0,
				log TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				FOREIGN KEY(app_id) REFERENCES apps(id) ON DELETE CASCADE,
				FOREIGN KEY(destination_id) REFERENCES backup_destinations(id) ON DELETE CASCADE
			)
		`)
		_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_backup_history_app ON backup_history(app_id, created_at)`)
	}
	
	// Add log column if missing
	var hasLogColumn int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('backup_history') WHERE name = 'log'`).Scan(&hasLogColumn)
	if hasLogColumn == 0 {
		_, _ = s.db.Exec(`ALTER TABLE backup_history ADD COLUMN log TEXT NOT NULL DEFAULT ''`)
	}

	var hasRemoteMissing int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('backup_history') WHERE name = 'remote_missing'`).Scan(&hasRemoteMissing)
	if hasRemoteMissing == 0 {
		_, _ = s.db.Exec(`ALTER TABLE backup_history ADD COLUMN remote_missing INTEGER NOT NULL DEFAULT -1`)
	}
	var hasRemoteChecked int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('backup_history') WHERE name = 'remote_checked_at'`).Scan(&hasRemoteChecked)
	if hasRemoteChecked == 0 {
		_, _ = s.db.Exec(`ALTER TABLE backup_history ADD COLUMN remote_checked_at TEXT NOT NULL DEFAULT ''`)
	}
	var hasScheduleVolumeNames int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('backup_schedules') WHERE name = 'volume_names'`).Scan(&hasScheduleVolumeNames)
	if hasScheduleVolumeNames == 0 {
		_, _ = s.db.Exec(`ALTER TABLE backup_schedules ADD COLUMN volume_names TEXT NOT NULL DEFAULT ''`)
	}
	var hasBackupVolumeName int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('backup_history') WHERE name = 'volume_name'`).Scan(&hasBackupVolumeName)
	if hasBackupVolumeName == 0 {
		_, _ = s.db.Exec(`ALTER TABLE backup_history ADD COLUMN volume_name TEXT NOT NULL DEFAULT ''`)
	}
	var hasSchedulePause int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('backup_schedules') WHERE name = 'pause_containers'`).Scan(&hasSchedulePause)
	if hasSchedulePause == 0 {
		_, _ = s.db.Exec(`ALTER TABLE backup_schedules ADD COLUMN pause_containers INTEGER NOT NULL DEFAULT 0`)
	}

	// Rename legacy 'full' backup rows to 'app'. The old "Full app" type only
	// packed workspace files (no Docker volumes), which is what the new "app"
	// type does. The new "full" type packs app + volumes together, so keeping
	// old rows under the "full" label would make them look restorable with
	// the new pipeline when they are not. We only flip the label for rows
	// that pre-date the multi-archive format (checked by whether any
	// newer-style full manifest info is present — since we cannot inspect the
	// archive here, we migrate everything once at startup).
	var legacyFullMigrated int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM settings WHERE key = 'backup_full_app_rename_done'`).Scan(&legacyFullMigrated)
	if legacyFullMigrated == 0 {
		_, _ = s.db.Exec(`UPDATE backup_history SET backup_type = 'app' WHERE backup_type = 'full'`)
		_, _ = s.db.Exec(`UPDATE backup_schedules SET backup_type = 'app' WHERE backup_type = 'full'`)
		_, _ = s.db.Exec(`INSERT OR REPLACE INTO settings(key, value) VALUES ('backup_full_app_rename_done', '1')`)
	}

	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS audit_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER,
  username TEXT,
  action TEXT NOT NULL,
  target_type TEXT NOT NULL,
  target_id TEXT NOT NULL,
  ip_address TEXT,
  user_agent TEXT,
  details TEXT,
  created_at TEXT NOT NULL
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_audit_logs_created ON audit_logs(created_at);`); err != nil {
		return err
	}

	// Multi-user & sandboxing schema migrations
	if _, err := s.db.Exec(`ALTER TABLE apps ADD COLUMN owner_id INTEGER REFERENCES users(id) ON DELETE SET NULL`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	if _, err := s.db.Exec(`ALTER TABLE apps ADD COLUMN status TEXT NOT NULL DEFAULT 'active'`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	if _, err := s.db.Exec(`ALTER TABLE users ADD COLUMN max_apps INTEGER NOT NULL DEFAULT 5`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	if _, err := s.db.Exec(`ALTER TABLE users ADD COLUMN max_memory_mb INTEGER NOT NULL DEFAULT 2048`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	if _, err := s.db.Exec(`ALTER TABLE users ADD COLUMN max_cpus REAL NOT NULL DEFAULT 2.0`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	if _, err := s.db.Exec(`ALTER TABLE users ADD COLUMN status TEXT NOT NULL DEFAULT 'active'`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	if _, err := s.db.Exec(`ALTER TABLE users ADD COLUMN max_storage_mb INTEGER NOT NULL DEFAULT 5120`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	if _, err := s.db.Exec(`ALTER TABLE users ADD COLUMN allow_domain_file_server INTEGER NOT NULL DEFAULT 0`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}

	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS app_collaborators (
  app_id TEXT NOT NULL,
  user_id INTEGER NOT NULL,
  role TEXT NOT NULL DEFAULT 'developer',
  created_at TEXT NOT NULL,
  PRIMARY KEY (app_id, user_id),
  FOREIGN KEY(app_id) REFERENCES apps(id) ON DELETE CASCADE,
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);`); err != nil {
		return err
	}

	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS private_registries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER,
  name TEXT NOT NULL,
  server_address TEXT NOT NULL,
  username TEXT NOT NULL,
  password_encrypted TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);`); err != nil {
		return err
	}

	var legacyOwnerMigrated int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM settings WHERE key = 'apps_owner_migration_done'`).Scan(&legacyOwnerMigrated)
	if legacyOwnerMigrated == 0 {
		_, _ = s.db.Exec(`UPDATE apps SET owner_id = (SELECT id FROM users WHERE role = 'admin' ORDER BY id LIMIT 1) WHERE owner_id IS NULL`)
		_, _ = s.db.Exec(`INSERT OR REPLACE INTO settings(key, value) VALUES ('apps_owner_migration_done', '1')`)
	}

	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS migrate_exports (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  status TEXT NOT NULL DEFAULT 'queued',
  token_hash TEXT NOT NULL DEFAULT '',
  bundle_path TEXT NOT NULL DEFAULT '',
  work_dir TEXT NOT NULL DEFAULT '',
  app_ids_json TEXT NOT NULL DEFAULT '[]',
  estimated_bytes INTEGER NOT NULL DEFAULT 0,
  size_bytes INTEGER NOT NULL DEFAULT 0,
  progress_log TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  downloaded_at TEXT
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_migrate_exports_status ON migrate_exports(status, expires_at);`); err != nil {
		return err
	}

	return nil
}

