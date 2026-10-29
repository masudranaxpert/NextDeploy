package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

const (
	MigrateExportQueued     = "queued"
	MigrateExportRunning    = "running"
	MigrateExportReady      = "ready"
	MigrateExportDownloaded = "downloaded"
	MigrateExportFailed     = "failed"
	MigrateExportExpired    = "expired"
)

type MigrateExport struct {
	ID             int64      `json:"id"`
	Status         string     `json:"status"`
	Token          string     `json:"token"`
	TokenHash      string     `json:"token_hash"`
	BundlePath     string     `json:"bundle_path"`
	WorkDir        string     `json:"work_dir"`
	AppIDsJSON     string     `json:"app_ids_json"`
	EstimatedBytes int64      `json:"estimated_bytes"`
	SizeBytes      int64      `json:"size_bytes"`
	ProgressLog    string     `json:"progress_log"`
	Error          string     `json:"error"`
	CreatedAt      time.Time  `json:"created_at"`
	ExpiresAt      time.Time  `json:"expires_at"`
	DownloadedAt   *time.Time `json:"downloaded_at,omitempty"`
}

func (s *Store) CreateMigrateExport(ctx context.Context, appIDs []string, estimatedBytes int64) (int64, error) {
	idsJSON, err := json.Marshal(appIDs)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO migrate_exports (status, app_ids_json, estimated_bytes, progress_log, created_at, expires_at)
		 VALUES (?, ?, ?, '', ?, ?)`,
		MigrateExportRunning, string(idsJSON), estimatedBytes, now.Format(time.RFC3339), now.Add(3*time.Hour).Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetMigrateExport(ctx context.Context, id int64) (MigrateExport, error) {
	var row MigrateExport
	var created, expires string
	var downloaded sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, status, COALESCE(token,''), COALESCE(token_hash,''), COALESCE(bundle_path,''), COALESCE(work_dir,''),
		        COALESCE(app_ids_json,'[]'), COALESCE(estimated_bytes,0), COALESCE(size_bytes,0),
		        COALESCE(progress_log,''), COALESCE(error,''), created_at, expires_at, downloaded_at
		 FROM migrate_exports WHERE id = ?`, id).
		Scan(&row.ID, &row.Status, &row.Token, &row.TokenHash, &row.BundlePath, &row.WorkDir, &row.AppIDsJSON,
			&row.EstimatedBytes, &row.SizeBytes, &row.ProgressLog, &row.Error, &created, &expires, &downloaded)
	if err != nil {
		return MigrateExport{}, err
	}
	row.CreatedAt, _ = time.Parse(time.RFC3339, created)
	row.ExpiresAt, _ = time.Parse(time.RFC3339, expires)
	if downloaded.Valid && strings.TrimSpace(downloaded.String) != "" {
		t, _ := time.Parse(time.RFC3339, downloaded.String)
		row.DownloadedAt = &t
	}
	return row, nil
}

func (s *Store) GetMigrateExportByTokenHash(ctx context.Context, hash string) (MigrateExport, error) {
	var row MigrateExport
	var created, expires string
	var downloaded sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, status, COALESCE(token,''), COALESCE(token_hash,''), COALESCE(bundle_path,''), COALESCE(work_dir,''),
		        COALESCE(app_ids_json,'[]'), COALESCE(estimated_bytes,0), COALESCE(size_bytes,0),
		        COALESCE(progress_log,''), COALESCE(error,''), created_at, expires_at, downloaded_at
		 FROM migrate_exports WHERE token_hash = ? OR token = ?`, hash, hash).
		Scan(&row.ID, &row.Status, &row.Token, &row.TokenHash, &row.BundlePath, &row.WorkDir, &row.AppIDsJSON,
			&row.EstimatedBytes, &row.SizeBytes, &row.ProgressLog, &row.Error, &created, &expires, &downloaded)
	if err != nil {
		return MigrateExport{}, err
	}
	row.CreatedAt, _ = time.Parse(time.RFC3339, created)
	row.ExpiresAt, _ = time.Parse(time.RFC3339, expires)
	if downloaded.Valid && strings.TrimSpace(downloaded.String) != "" {
		t, _ := time.Parse(time.RFC3339, downloaded.String)
		row.DownloadedAt = &t
	}
	return row, nil
}

func (s *Store) AppendMigrateExportLog(ctx context.Context, id int64, line string) error {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	row, err := s.GetMigrateExport(ctx, id)
	if err != nil {
		return err
	}
	log := strings.TrimSpace(row.ProgressLog)
	if log != "" {
		log += "\n"
	}
	log += time.Now().UTC().Format("2006-01-02 15:04:05") + " " + line
	if len(log) > 65536 {
		log = log[len(log)-65536:]
	}
	_, err = s.db.ExecContext(ctx, `UPDATE migrate_exports SET progress_log = ? WHERE id = ?`, log, id)
	return err
}

func (s *Store) UpdateMigrateExportReady(ctx context.Context, id int64, bundlePath string, sizeBytes int64, token, tokenHash string, expires time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE migrate_exports SET status = ?, bundle_path = ?, size_bytes = ?, token = ?, token_hash = ?, expires_at = ? WHERE id = ?`,
		MigrateExportReady, bundlePath, sizeBytes, token, tokenHash, expires.UTC().Format(time.RFC3339), id)
	return err
}

func (s *Store) FailMigrateExport(ctx context.Context, id int64, errMsg string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE migrate_exports SET status = ?, error = ? WHERE id = ?`, MigrateExportFailed, errMsg, id)
	return err
}

func (s *Store) MarkMigrateExportDownloaded(ctx context.Context, id int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `UPDATE migrate_exports SET status = ?, downloaded_at = ? WHERE id = ?`, MigrateExportDownloaded, now, id)
	return err
}

func (s *Store) MarkMigrateExportExpired(id int64) error {
	_, err := s.db.Exec(`UPDATE migrate_exports SET status = ? WHERE id = ?`, MigrateExportExpired, id)
	return err
}

func (s *Store) ExpireMigrateExport(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE migrate_exports SET status = ? WHERE id = ?`, MigrateExportExpired, id)
	return err
}

func (s *Store) HasRunningMigrateExport(ctx context.Context) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM migrate_exports WHERE status = ?`, MigrateExportRunning).Scan(&n)
	return n > 0, err
}

func (s *Store) DeleteMigrateExport(ctx context.Context, id int64) (MigrateExport, error) {
	row, err := s.GetMigrateExport(ctx, id)
	if err != nil {
		return MigrateExport{}, err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM migrate_exports WHERE id = ?`, id)
	return row, err
}

func (s *Store) ListSupersededMigrateExports(ctx context.Context, keepID int64) ([]MigrateExport, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, status, COALESCE(token,''), COALESCE(token_hash,''), COALESCE(bundle_path,''), COALESCE(work_dir,''),
		        COALESCE(app_ids_json,'[]'), COALESCE(estimated_bytes,0), COALESCE(size_bytes,0),
		        COALESCE(progress_log,''), COALESCE(error,''), created_at, expires_at, downloaded_at
		 FROM migrate_exports WHERE id != ? AND status IN (?, ?)`,
		keepID, MigrateExportReady, MigrateExportDownloaded)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMigrateExports(rows)
}

func (s *Store) GetRunningMigrateExport(ctx context.Context) (MigrateExport, error) {
	var row MigrateExport
	var created, expires string
	var downloaded sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, status, COALESCE(token,''), COALESCE(token_hash,''), COALESCE(bundle_path,''), COALESCE(work_dir,''),
		        COALESCE(app_ids_json,'[]'), COALESCE(estimated_bytes,0), COALESCE(size_bytes,0),
		        COALESCE(progress_log,''), COALESCE(error,''), created_at, expires_at, downloaded_at
		 FROM migrate_exports WHERE status = ? ORDER BY id DESC LIMIT 1`, MigrateExportRunning).
		Scan(&row.ID, &row.Status, &row.Token, &row.TokenHash, &row.BundlePath, &row.WorkDir, &row.AppIDsJSON,
			&row.EstimatedBytes, &row.SizeBytes, &row.ProgressLog, &row.Error, &created, &expires, &downloaded)
	if err != nil {
		return MigrateExport{}, err
	}
	row.CreatedAt, _ = time.Parse(time.RFC3339, created)
	row.ExpiresAt, _ = time.Parse(time.RFC3339, expires)
	if downloaded.Valid && strings.TrimSpace(downloaded.String) != "" {
		t, _ := time.Parse(time.RFC3339, downloaded.String)
		row.DownloadedAt = &t
	}
	return row, nil
}

func (s *Store) ListRunningMigrateExports(ctx context.Context) ([]MigrateExport, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, status, COALESCE(token,''), COALESCE(token_hash,''), COALESCE(bundle_path,''), COALESCE(work_dir,''),
		        COALESCE(app_ids_json,'[]'), COALESCE(estimated_bytes,0), COALESCE(size_bytes,0),
		        COALESCE(progress_log,''), COALESCE(error,''), created_at, expires_at, downloaded_at
		 FROM migrate_exports WHERE status = ?`, MigrateExportRunning)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMigrateExports(rows)
}

func (s *Store) ListActiveMigrateBundleExports() ([]MigrateExport, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := s.db.Query(
		`SELECT id, status, COALESCE(token,''), COALESCE(token_hash,''), COALESCE(bundle_path,''), COALESCE(work_dir,''),
		        COALESCE(app_ids_json,'[]'), COALESCE(estimated_bytes,0), COALESCE(size_bytes,0),
		        COALESCE(progress_log,''), COALESCE(error,''), created_at, expires_at, downloaded_at
		 FROM migrate_exports WHERE status IN (?, ?) AND expires_at >= ?`,
		MigrateExportReady, MigrateExportDownloaded, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMigrateExports(rows)
}

func (s *Store) ListRecentMigrateExports(ctx context.Context, limit int) ([]MigrateExport, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, status, COALESCE(token,''), COALESCE(token_hash,''), COALESCE(bundle_path,''), COALESCE(work_dir,''),
		        COALESCE(app_ids_json,'[]'), COALESCE(estimated_bytes,0), COALESCE(size_bytes,0),
		        COALESCE(progress_log,''), COALESCE(error,''), created_at, expires_at, downloaded_at
		 FROM migrate_exports ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMigrateExports(rows)
}

func (s *Store) ListExpiredMigrateExports() ([]MigrateExport, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := s.db.Query(
		`SELECT id, status, COALESCE(token,''), COALESCE(token_hash,''), COALESCE(bundle_path,''), COALESCE(work_dir,''),
		        COALESCE(app_ids_json,'[]'), COALESCE(estimated_bytes,0), COALESCE(size_bytes,0),
		        COALESCE(progress_log,''), COALESCE(error,''), created_at, expires_at, downloaded_at
		 FROM migrate_exports WHERE status IN (?, ?) AND expires_at < ?`,
		MigrateExportReady, MigrateExportDownloaded, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMigrateExports(rows)
}

func (s *Store) FailStaleMigrateExports() error {
	cutoff := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	rows, err := s.db.Query(
		`SELECT id, COALESCE(work_dir,''), COALESCE(bundle_path,'') FROM migrate_exports WHERE status = ? AND created_at < ?`,
		MigrateExportRunning, cutoff)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var workDir, bundlePath string
		if err := rows.Scan(&id, &workDir, &bundlePath); err != nil {
			continue
		}
		_, _ = s.db.Exec(`UPDATE migrate_exports SET status = ?, error = ? WHERE id = ?`,
			MigrateExportFailed, "export interrupted (panel restarted)", id)
	}
	return nil
}

func scanMigrateExports(rows *sql.Rows) ([]MigrateExport, error) {
	var out []MigrateExport
	for rows.Next() {
		var row MigrateExport
		var created, expires string
		var downloaded sql.NullString
		if err := rows.Scan(&row.ID, &row.Status, &row.Token, &row.TokenHash, &row.BundlePath, &row.WorkDir, &row.AppIDsJSON,
			&row.EstimatedBytes, &row.SizeBytes, &row.ProgressLog, &row.Error, &created, &expires, &downloaded); err != nil {
			return nil, err
		}
		row.CreatedAt, _ = time.Parse(time.RFC3339, created)
		row.ExpiresAt, _ = time.Parse(time.RFC3339, expires)
		if downloaded.Valid && strings.TrimSpace(downloaded.String) != "" {
			t, _ := time.Parse(time.RFC3339, downloaded.String)
			row.DownloadedAt = &t
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) InsertPHPSiteMigrate(ctx context.Context, site PHPSite) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if !site.CreatedAt.IsZero() {
		now = site.CreatedAt.UTC().Format(time.RFC3339)
	}
	settings := site.SettingsJSON
	if strings.TrimSpace(settings) == "" {
		settings = "{}"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO php_sites (app_id, owner_id, site_uid, site_type, php_version, root_dir, settings_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		site.AppID, site.OwnerID, site.UID, site.SiteType, site.PHPVersion, site.RootDir, settings, now)
	return err
}

func (s *Store) InsertPHPDatabaseMigrate(ctx context.Context, d PHPDatabase) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if !d.CreatedAt.IsZero() {
		now = d.CreatedAt.UTC().Format(time.RFC3339)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO php_databases (app_id, db_name, db_user, created_at)
		 VALUES (?, ?, ?, ?)`,
		d.AppID, d.DBName, d.DBUser, now)
	return err
}

func (s *Store) InsertPHPCronJobMigrate(ctx context.Context, j PHPCronJob) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if !j.CreatedAt.IsZero() {
		now = j.CreatedAt.UTC().Format(time.RFC3339)
	}
	en := 0
	if j.Enabled {
		en = 1
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO php_cron_jobs (app_id, schedule, command, enabled, last_run_at, last_status, last_output, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		j.AppID, j.Schedule, j.Command, en, j.LastRunAt, j.LastStatus, j.LastOutput, now)
	return err
}

func (s *Store) ClearPanelForMigrateImport(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmts := []string{
		`DELETE FROM sessions`,
		`DELETE FROM app_collaborators`,
		`DELETE FROM app_domains`,
		`DELETE FROM app_git_configs`,
		`DELETE FROM apps`,
		`DELETE FROM private_registries`,
		`DELETE FROM github_provider_details`,
		`DELETE FROM git_providers`,
		`DELETE FROM users`,
	}
	for _, q := range stmts {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) InsertUserMigrate(ctx context.Context, u User) error {
	allow := 0
	if u.AllowDomainFileServer {
		allow = 1
	}
	created := u.CreatedAt.UTC().Format(time.RFC3339)
	if u.CreatedAt.IsZero() {
		created = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO users(id,username,password_hash,role,created_at,max_apps,max_memory_mb,max_cpus,max_storage_mb,status,allow_domain_file_server) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		u.ID, u.Username, u.PasswordHash, u.Role, created, u.MaxApps, u.MaxMemoryMB, u.MaxCPUs, u.MaxStorageMB, u.Status, allow)
	return err
}

func (s *Store) SyncUsersIDSequence(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sqlite_sequence SET seq=(SELECT COALESCE(MAX(id),0) FROM users) WHERE name='users'`)
	return err
}

func (s *Store) InsertGitProviderMigrate(ctx context.Context, p GitProvider) error {
	ca := p.CreatedAt.UTC().Format(time.RFC3339)
	ua := p.UpdatedAt.UTC().Format(time.RFC3339)
	if p.CreatedAt.IsZero() {
		ca = time.Now().UTC().Format(time.RFC3339)
	}
	if p.UpdatedAt.IsZero() {
		ua = ca
	}
	var uid interface{}
	if p.UserID != nil {
		uid = *p.UserID
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO git_providers(id,user_id,name,provider,token,refresh_token,expires_at,notes,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		p.ID, uid, p.Name, p.Provider, p.Token, p.RefreshToken, p.ExpiresAt, p.Notes, ca, ua)
	return err
}

func (s *Store) SyncGitProvidersIDSequence(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sqlite_sequence SET seq=(SELECT COALESCE(MAX(id),0) FROM git_providers) WHERE name='git_providers'`)
	return err
}
