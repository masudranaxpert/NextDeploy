package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PHP hosting: a PHP site is a regular app (row in apps) plus a row in php_sites.
// Keeping the PHP data in separate tables leaves every existing app query untouched.

const phpSiteUIDBase = 20000

var ErrAppLimitReached = errors.New("maximum app limit reached")

type PHPSite struct {
	AppID        string    `json:"app_id"`
	OwnerID      int64     `json:"owner_id"`
	UID          int       `json:"uid"`
	SiteType     string    `json:"site_type"` // php | wordpress | static
	PHPVersion   string    `json:"php_version"`
	RootDir      string    `json:"root_dir"`
	SettingsJSON string    `json:"settings_json"`
	CreatedAt    time.Time `json:"created_at"`
}

type PHPDatabase struct {
	ID        int64     `json:"id"`
	AppID     string    `json:"app_id"`
	DBName    string    `json:"db_name"`
	DBUser    string    `json:"db_user"`
	CreatedAt time.Time `json:"created_at"`
}

type PHPCronJob struct {
	ID         int64     `json:"id"`
	AppID      string    `json:"app_id"`
	Schedule   string    `json:"schedule"`
	Command    string    `json:"command"`
	Enabled    bool      `json:"enabled"`
	LastRunAt  string    `json:"last_run_at"`
	LastStatus string    `json:"last_status"`
	LastOutput string    `json:"last_output"`
	CreatedAt  time.Time `json:"created_at"`
}

type PHPUserLimits struct {
	UserID       int64 `json:"user_id"`
	MaxDatabases int   `json:"max_databases"`
	MaxCronJobs  int   `json:"max_cron_jobs"`
}

const (
	DefaultPHPMaxDatabases = 5
	DefaultPHPMaxCronJobs  = 10
)

func (s *Store) migratePHPHosting() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS php_sites (
  app_id TEXT PRIMARY KEY,
  owner_id INTEGER NOT NULL,
  site_uid INTEGER NOT NULL UNIQUE,
  site_type TEXT NOT NULL DEFAULT 'php',
  php_version TEXT NOT NULL DEFAULT '',
  root_dir TEXT NOT NULL DEFAULT '',
  settings_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  FOREIGN KEY(app_id) REFERENCES apps(id) ON DELETE CASCADE
);`,
		`CREATE INDEX IF NOT EXISTS idx_php_sites_owner ON php_sites(owner_id, php_version);`,
		`CREATE TABLE IF NOT EXISTS php_databases (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  app_id TEXT NOT NULL,
  db_name TEXT NOT NULL UNIQUE,
  db_user TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  FOREIGN KEY(app_id) REFERENCES apps(id) ON DELETE CASCADE
);`,
		`CREATE INDEX IF NOT EXISTS idx_php_databases_app ON php_databases(app_id);`,
		`CREATE TABLE IF NOT EXISTS php_cron_jobs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  app_id TEXT NOT NULL,
  schedule TEXT NOT NULL,
  command TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  last_run_at TEXT NOT NULL DEFAULT '',
  last_status TEXT NOT NULL DEFAULT '',
  last_output TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  FOREIGN KEY(app_id) REFERENCES apps(id) ON DELETE CASCADE
);`,
		`CREATE INDEX IF NOT EXISTS idx_php_cron_app ON php_cron_jobs(app_id);`,
		`CREATE TABLE IF NOT EXISTS php_db_users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  app_id TEXT NOT NULL,
  user_name TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL,
  FOREIGN KEY(app_id) REFERENCES apps(id) ON DELETE CASCADE
);`,
		`CREATE INDEX IF NOT EXISTS idx_php_db_users_app ON php_db_users(app_id);`,
		`CREATE TABLE IF NOT EXISTS php_db_grants (
  db_id INTEGER NOT NULL,
  user_id INTEGER NOT NULL,
  access TEXT NOT NULL DEFAULT 'all',
  PRIMARY KEY(db_id, user_id),
  FOREIGN KEY(db_id) REFERENCES php_databases(id) ON DELETE CASCADE,
  FOREIGN KEY(user_id) REFERENCES php_db_users(id) ON DELETE CASCADE
);`,
		`CREATE TABLE IF NOT EXISTS php_user_limits (
  user_id INTEGER PRIMARY KEY,
  max_databases INTEGER NOT NULL DEFAULT 5,
  max_cron_jobs INTEGER NOT NULL DEFAULT 10
);`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	return s.migratePHPDatabaseUsers()
}

// migratePHPDatabaseUsers upgrades the first schema, where every database had exactly one user
// (db_user UNIQUE), to independent databases and users joined by grants.
func (s *Store) migratePHPDatabaseUsers() error {
	var ddl string
	_ = s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='php_databases'`).Scan(&ddl)
	if !strings.Contains(ddl, "db_user TEXT NOT NULL UNIQUE") {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	steps := []string{
		`CREATE TABLE php_databases_new (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  app_id TEXT NOT NULL,
  db_name TEXT NOT NULL UNIQUE,
  db_user TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  FOREIGN KEY(app_id) REFERENCES apps(id) ON DELETE CASCADE
)`,
		`INSERT INTO php_databases_new (id, app_id, db_name, db_user, created_at) SELECT id, app_id, db_name, db_user, created_at FROM php_databases`,
		`INSERT OR IGNORE INTO php_db_users (app_id, user_name, created_at) SELECT app_id, db_user, created_at FROM php_databases WHERE db_user != ''`,
		`DROP TABLE php_databases`,
		`ALTER TABLE php_databases_new RENAME TO php_databases`,
		`CREATE INDEX IF NOT EXISTS idx_php_databases_app ON php_databases(app_id)`,
		`INSERT OR IGNORE INTO php_db_grants (db_id, user_id, access) SELECT d.id, u.id, 'all' FROM php_databases d JOIN php_db_users u ON u.user_name = d.db_user WHERE d.db_user != ''`,
	}
	for _, q := range steps {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CreateAppAtomic enforces the owner's app limit and inserts the app inside a single
// write transaction (BEGIN IMMEDIATE), so concurrent requests cannot both pass the
// count check. maxApps <= 0 means unlimited. When site is non-nil the php_sites row is
// created in the same transaction and its UID allocated.
func (s *Store) CreateAppAtomic(ctx context.Context, id, name string, ownerID int64, maxApps int, site *PHPSite) error {
	if id == "" || name == "" {
		return errors.New("invalid app")
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()

	var found string
	err = conn.QueryRowContext(ctx, `SELECT id FROM apps WHERE lower(name) = ? AND owner_id = ? LIMIT 1`, strings.ToLower(strings.TrimSpace(name)), ownerID).Scan(&found)
	if err == nil && found != "" {
		return errors.New("app name already exists")
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if maxApps > 0 {
		var n int
		if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM apps WHERE owner_id = ?`, ownerID).Scan(&n); err != nil {
			return err
		}
		if n >= maxApps {
			return ErrAppLimitReached
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := conn.ExecContext(ctx, `INSERT INTO apps (id, name, created_at, compose_file, owner_id, status) VALUES (?, ?, ?, ?, ?, ?)`,
		id, name, now, "docker-compose.yml", ownerID, "active"); err != nil {
		return err
	}
	if site != nil {
		var maxUID sql.NullInt64
		if err := conn.QueryRowContext(ctx, `SELECT MAX(site_uid) FROM php_sites`).Scan(&maxUID); err != nil {
			return err
		}
		uid := phpSiteUIDBase
		if maxUID.Valid && int(maxUID.Int64) >= phpSiteUIDBase {
			uid = int(maxUID.Int64) + 1
		}
		site.UID = uid
		site.AppID = id
		site.OwnerID = ownerID
		if site.SettingsJSON == "" {
			site.SettingsJSON = "{}"
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO php_sites (app_id, owner_id, site_uid, site_type, php_version, root_dir, settings_json, created_at) VALUES (?,?,?,?,?,?,?,?)`,
			id, ownerID, uid, site.SiteType, site.PHPVersion, site.RootDir, site.SettingsJSON, now); err != nil {
			return err
		}
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return err
	}
	committed = true
	return nil
}

func scanPHPSite(row interface{ Scan(...any) error }) (PHPSite, error) {
	var st PHPSite
	var created string
	if err := row.Scan(&st.AppID, &st.OwnerID, &st.UID, &st.SiteType, &st.PHPVersion, &st.RootDir, &st.SettingsJSON, &created); err != nil {
		return PHPSite{}, err
	}
	st.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return st, nil
}

const phpSiteCols = `app_id, owner_id, site_uid, site_type, php_version, root_dir, settings_json, created_at`

func (s *Store) GetPHPSite(ctx context.Context, appID string) (PHPSite, error) {
	return scanPHPSite(s.db.QueryRowContext(ctx, `SELECT `+phpSiteCols+` FROM php_sites WHERE app_id = ?`, appID))
}

func (s *Store) IsPHPSite(ctx context.Context, appID string) bool {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM php_sites WHERE app_id = ?`, appID).Scan(&one)
	return err == nil
}

func (s *Store) ListPHPSites(ctx context.Context) ([]PHPSite, error) {
	return s.listPHPSites(ctx, `SELECT `+phpSiteCols+` FROM php_sites ORDER BY site_uid`)
}

func (s *Store) ListPHPSitesByOwner(ctx context.Context, ownerID int64) ([]PHPSite, error) {
	return s.listPHPSites(ctx, `SELECT `+phpSiteCols+` FROM php_sites WHERE owner_id = ? ORDER BY site_uid`, ownerID)
}

func (s *Store) ListPHPSitesForRuntime(ctx context.Context, ownerID int64, version string) ([]PHPSite, error) {
	return s.listPHPSites(ctx, `SELECT `+phpSiteCols+` FROM php_sites WHERE owner_id = ? AND php_version = ? ORDER BY site_uid`, ownerID, version)
}

func (s *Store) listPHPSites(ctx context.Context, q string, args ...any) ([]PHPSite, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PHPSite
	for rows.Next() {
		st, err := scanPHPSite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Store) UpdatePHPSite(ctx context.Context, st PHPSite) error {
	_, err := s.db.ExecContext(ctx, `UPDATE php_sites SET owner_id=?, php_version=?, root_dir=?, settings_json=? WHERE app_id=?`,
		st.OwnerID, st.PHPVersion, st.RootDir, st.SettingsJSON, st.AppID)
	return err
}

func (s *Store) CountPHPSitesByVersion(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT php_version, COUNT(*) FROM php_sites WHERE php_version != '' GROUP BY php_version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var v string
		var n int
		if err := rows.Scan(&v, &n); err != nil {
			return nil, err
		}
		out[v] = n
	}
	return out, rows.Err()
}

// ---- databases ----

func (s *Store) CountPHPDatabasesForOwner(ctx context.Context, ownerID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM php_databases d JOIN apps a ON a.id = d.app_id WHERE a.owner_id = ?`, ownerID).Scan(&n)
	return n, err
}

func (s *Store) CreatePHPDatabase(ctx context.Context, d PHPDatabase) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO php_databases (app_id, db_name, db_user, created_at) VALUES (?,?,?,?)`,
		d.AppID, d.DBName, d.DBUser, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListPHPDatabases(ctx context.Context, appID string) ([]PHPDatabase, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, app_id, db_name, db_user, created_at FROM php_databases WHERE app_id = ? ORDER BY id`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PHPDatabase
	for rows.Next() {
		var d PHPDatabase
		var created string
		if err := rows.Scan(&d.ID, &d.AppID, &d.DBName, &d.DBUser, &created); err != nil {
			return nil, err
		}
		d.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) GetPHPDatabase(ctx context.Context, appID string, id int64) (PHPDatabase, error) {
	var d PHPDatabase
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT id, app_id, db_name, db_user, created_at FROM php_databases WHERE id = ? AND app_id = ?`, id, appID).
		Scan(&d.ID, &d.AppID, &d.DBName, &d.DBUser, &created)
	if err != nil {
		return PHPDatabase{}, err
	}
	d.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return d, nil
}

func (s *Store) DeletePHPDatabase(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM php_databases WHERE id = ?`, id)
	return err
}

// ---- cron ----

func (s *Store) CountPHPCronJobsForOwner(ctx context.Context, ownerID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM php_cron_jobs j JOIN apps a ON a.id = j.app_id WHERE a.owner_id = ?`, ownerID).Scan(&n)
	return n, err
}

func (s *Store) CreatePHPCronJob(ctx context.Context, j PHPCronJob) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO php_cron_jobs (app_id, schedule, command, enabled, created_at) VALUES (?,?,?,?,?)`,
		j.AppID, j.Schedule, j.Command, boolToInt(j.Enabled), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

const phpCronCols = `id, app_id, schedule, command, enabled, last_run_at, last_status, last_output, created_at`

func scanPHPCron(row interface{ Scan(...any) error }) (PHPCronJob, error) {
	var j PHPCronJob
	var en int
	var created string
	if err := row.Scan(&j.ID, &j.AppID, &j.Schedule, &j.Command, &en, &j.LastRunAt, &j.LastStatus, &j.LastOutput, &created); err != nil {
		return PHPCronJob{}, err
	}
	j.Enabled = en != 0
	j.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return j, nil
}

func (s *Store) ListPHPCronJobs(ctx context.Context, appID string) ([]PHPCronJob, error) {
	return s.listPHPCron(ctx, `SELECT `+phpCronCols+` FROM php_cron_jobs WHERE app_id = ? ORDER BY id`, appID)
}

func (s *Store) ListAllEnabledPHPCronJobs(ctx context.Context) ([]PHPCronJob, error) {
	return s.listPHPCron(ctx, `SELECT `+phpCronCols+` FROM php_cron_jobs WHERE enabled = 1 ORDER BY id`)
}

func (s *Store) listPHPCron(ctx context.Context, q string, args ...any) ([]PHPCronJob, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PHPCronJob
	for rows.Next() {
		j, err := scanPHPCron(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) GetPHPCronJob(ctx context.Context, appID string, id int64) (PHPCronJob, error) {
	return scanPHPCron(s.db.QueryRowContext(ctx, `SELECT `+phpCronCols+` FROM php_cron_jobs WHERE id = ? AND app_id = ?`, id, appID))
}

func (s *Store) UpdatePHPCronJob(ctx context.Context, j PHPCronJob) error {
	_, err := s.db.ExecContext(ctx, `UPDATE php_cron_jobs SET schedule=?, command=?, enabled=? WHERE id=? AND app_id=?`,
		j.Schedule, j.Command, boolToInt(j.Enabled), j.ID, j.AppID)
	return err
}

func (s *Store) RecordPHPCronRun(ctx context.Context, id int64, status, output string) error {
	if len(output) > 4096 {
		output = output[len(output)-4096:]
	}
	_, err := s.db.ExecContext(ctx, `UPDATE php_cron_jobs SET last_run_at=?, last_status=?, last_output=? WHERE id=?`,
		time.Now().UTC().Format(time.RFC3339), status, output, id)
	return err
}

func (s *Store) DeletePHPCronJob(ctx context.Context, appID string, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM php_cron_jobs WHERE id = ? AND app_id = ?`, id, appID)
	return err
}

// ---- per-user limits ----

func (s *Store) GetPHPUserLimits(ctx context.Context, userID int64) PHPUserLimits {
	l := PHPUserLimits{UserID: userID, MaxDatabases: DefaultPHPMaxDatabases, MaxCronJobs: DefaultPHPMaxCronJobs}
	_ = s.db.QueryRowContext(ctx, `SELECT max_databases, max_cron_jobs FROM php_user_limits WHERE user_id = ?`, userID).
		Scan(&l.MaxDatabases, &l.MaxCronJobs)
	return l
}

func (s *Store) SetPHPUserLimits(ctx context.Context, l PHPUserLimits) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO php_user_limits (user_id, max_databases, max_cron_jobs) VALUES (?,?,?)
ON CONFLICT(user_id) DO UPDATE SET max_databases=excluded.max_databases, max_cron_jobs=excluded.max_cron_jobs`,
		l.UserID, l.MaxDatabases, l.MaxCronJobs)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

var _ = fmt.Sprintf

// ---- database users and grants ----

type PHPDBUser struct {
	ID        int64     `json:"id"`
	AppID     string    `json:"app_id"`
	UserName  string    `json:"user_name"`
	CreatedAt time.Time `json:"created_at"`
}

type PHPDBGrant struct {
	DBID   int64  `json:"db_id"`
	UserID int64  `json:"user_id"`
	Access string `json:"access"` // all | read
}

func (s *Store) CountPHPDBUsersForOwner(ctx context.Context, ownerID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM php_db_users u JOIN apps a ON a.id = u.app_id WHERE a.owner_id = ?`, ownerID).Scan(&n)
	return n, err
}

func (s *Store) CreatePHPDBUser(ctx context.Context, appID, userName string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO php_db_users (app_id, user_name, created_at) VALUES (?,?,?)`,
		appID, userName, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListPHPDBUsers(ctx context.Context, appID string) ([]PHPDBUser, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, app_id, user_name, created_at FROM php_db_users WHERE app_id = ? ORDER BY id`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PHPDBUser{}
	for rows.Next() {
		var u PHPDBUser
		var created string
		if err := rows.Scan(&u.ID, &u.AppID, &u.UserName, &created); err != nil {
			return nil, err
		}
		u.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) GetPHPDBUser(ctx context.Context, appID string, id int64) (PHPDBUser, error) {
	var u PHPDBUser
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT id, app_id, user_name, created_at FROM php_db_users WHERE id = ? AND app_id = ?`, id, appID).
		Scan(&u.ID, &u.AppID, &u.UserName, &created)
	if err != nil {
		return PHPDBUser{}, err
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return u, nil
}

func (s *Store) DeletePHPDBUser(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM php_db_users WHERE id = ?`, id)
	return err
}

// ListPHPGrants returns every grant between the site's databases and users.
func (s *Store) ListPHPGrants(ctx context.Context, appID string) ([]PHPDBGrant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT g.db_id, g.user_id, g.access FROM php_db_grants g
JOIN php_databases d ON d.id = g.db_id WHERE d.app_id = ? ORDER BY g.db_id, g.user_id`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PHPDBGrant{}
	for rows.Next() {
		var g PHPDBGrant
		if err := rows.Scan(&g.DBID, &g.UserID, &g.Access); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ReplacePHPUserGrants replaces all grants of one user.
func (s *Store) ReplacePHPUserGrants(ctx context.Context, userID int64, grants []PHPDBGrant) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM php_db_grants WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for _, g := range grants {
		if _, err := tx.ExecContext(ctx, `INSERT INTO php_db_grants (db_id, user_id, access) VALUES (?,?,?)`, g.DBID, userID, g.Access); err != nil {
			return err
		}
	}
	return tx.Commit()
}
