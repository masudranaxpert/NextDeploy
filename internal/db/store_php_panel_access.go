package db

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

func boolIntValue(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Store) EnsurePHPPanelAccount(ctx context.Context, userID int64, enabled bool, siteLimit, databaseLimit int) error {
	if siteLimit <= 0 {
		siteLimit = 3
	}
	if databaseLimit <= 0 {
		databaseLimit = 3
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO php_panel_accounts(user_id, enabled, site_limit, database_limit, created_at, updated_at)
VALUES(?,?,?,?,?,?)
ON CONFLICT(user_id) DO UPDATE SET
  enabled=excluded.enabled,
  site_limit=excluded.site_limit,
  database_limit=excluded.database_limit,
  updated_at=excluded.updated_at`,
		userID, boolIntValue(enabled), siteLimit, databaseLimit, now, now,
	)
	return err
}

func (s *Store) GetPHPPanelAccount(ctx context.Context, userID int64) (PHPPanelAccount, error) {
	var item PHPPanelAccount
	var enabled int
	var created, updated string
	err := s.db.QueryRowContext(ctx, `
SELECT user_id, enabled, site_limit, database_limit, created_at, updated_at
FROM php_panel_accounts
WHERE user_id = ?`, userID).
		Scan(&item.UserID, &enabled, &item.SiteLimit, &item.DatabaseLimit, &created, &updated)
	if err != nil {
		return PHPPanelAccount{}, err
	}
	item.Enabled = enabled != 0
	item.CreatedAt, _ = time.Parse(time.RFC3339, created)
	item.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return item, nil
}

func (s *Store) PHPPanelEnabledForUser(ctx context.Context, userID int64) bool {
	item, err := s.GetPHPPanelAccount(ctx, userID)
	return err == nil && item.Enabled
}

func (s *Store) CountOwnedPHPPanelSites(ctx context.Context, appID string, userID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_php_sites WHERE app_id = ? AND user_id = ?`, appID, userID).Scan(&n)
	return n, err
}

func (s *Store) CountOwnedPHPPanelDatabases(ctx context.Context, appID string, userID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM php_panel_databases WHERE app_id = ? AND user_id = ?`, appID, userID).Scan(&n)
	return n, err
}

func (s *Store) CountPHPPanelDatabases(ctx context.Context, appID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM php_panel_databases WHERE app_id = ?`, appID).Scan(&n)
	return n, err
}

func (s *Store) ListPHPPanelSitesByOwner(ctx context.Context, appID string, userID int64) ([]PHPPanelSite, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, app_id, user_id, name, slug, php_version, created_at
FROM app_php_sites
WHERE app_id = ? AND user_id = ?
ORDER BY datetime(created_at) ASC`, appID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PHPPanelSite
	for rows.Next() {
		var item PHPPanelSite
		var created string
		if err := rows.Scan(&item.ID, &item.AppID, &item.UserID, &item.Name, &item.Slug, &item.PHPVersion, &created); err != nil {
			return nil, err
		}
		item.PHPVersion = normalizePHPVersion(item.PHPVersion)
		item.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) GetPHPPanelSiteBySlugAndOwner(ctx context.Context, appID, slug string, userID int64) (PHPPanelSite, error) {
	var item PHPPanelSite
	var created string
	err := s.db.QueryRowContext(ctx, `
SELECT id, app_id, user_id, name, slug, php_version, created_at
FROM app_php_sites
WHERE app_id = ? AND slug = ? AND user_id = ?`,
		appID, strings.TrimSpace(slug), userID).
		Scan(&item.ID, &item.AppID, &item.UserID, &item.Name, &item.Slug, &item.PHPVersion, &created)
	if err != nil {
		return PHPPanelSite{}, err
	}
	item.PHPVersion = normalizePHPVersion(item.PHPVersion)
	item.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return item, nil
}

func (s *Store) CreateOwnedPHPPanelSite(ctx context.Context, appID string, userID int64, name, slug, phpVersion string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO app_php_sites(app_id, user_id, name, slug, php_version, created_at) VALUES(?,?,?,?,?,?)`,
		appID, userID, strings.TrimSpace(name), strings.TrimSpace(slug), normalizePHPVersion(phpVersion), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateOwnedPHPPanelSiteVersion(ctx context.Context, appID, slug string, userID int64, phpVersion string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE app_php_sites SET php_version = ? WHERE app_id = ? AND slug = ? AND user_id = ?`,
		normalizePHPVersion(phpVersion), appID, strings.TrimSpace(slug), userID)
	return err
}

func (s *Store) DeleteOwnedPHPPanelSite(ctx context.Context, appID, slug string, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM app_php_sites WHERE app_id = ? AND slug = ? AND user_id = ?`, appID, strings.TrimSpace(slug), userID)
	return err
}

func (s *Store) HasOwnedPHPPanelSite(ctx context.Context, appID, slug string, userID int64) bool {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM app_php_sites WHERE app_id = ? AND slug = ? AND user_id = ? LIMIT 1`, appID, strings.TrimSpace(slug), userID).Scan(&id)
	return err == nil && id > 0
}

func (s *Store) UpsertPHPPanelDomainOwner(ctx context.Context, appDomainID int64, appID string, userID int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO php_panel_domain_owners(app_domain_id, app_id, user_id, created_at)
VALUES(?,?,?,?)
ON CONFLICT(app_domain_id) DO UPDATE SET app_id=excluded.app_id, user_id=excluded.user_id`,
		appDomainID, appID, userID, now)
	return err
}

func (s *Store) ListPHPPanelDomainOwners(ctx context.Context, appID string, userID int64) ([]PHPPanelDomainOwner, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, app_domain_id, app_id, user_id, created_at
FROM php_panel_domain_owners
WHERE app_id = ? AND user_id = ?
ORDER BY id ASC`, appID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PHPPanelDomainOwner
	for rows.Next() {
		var item PHPPanelDomainOwner
		var created string
		if err := rows.Scan(&item.ID, &item.AppDomainID, &item.AppID, &item.UserID, &created); err != nil {
			return nil, err
		}
		item.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) GetPHPPanelDomainOwnerByDomainID(ctx context.Context, appDomainID int64) (PHPPanelDomainOwner, error) {
	var item PHPPanelDomainOwner
	var created string
	err := s.db.QueryRowContext(ctx, `
SELECT id, app_domain_id, app_id, user_id, created_at
FROM php_panel_domain_owners
WHERE app_domain_id = ?`, appDomainID).
		Scan(&item.ID, &item.AppDomainID, &item.AppID, &item.UserID, &created)
	if err != nil {
		return PHPPanelDomainOwner{}, err
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return item, nil
}

func (s *Store) DeletePHPPanelDomainOwnerByDomainID(ctx context.Context, appDomainID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM php_panel_domain_owners WHERE app_domain_id = ?`, appDomainID)
	return err
}

func (s *Store) CreatePHPPanelDatabase(ctx context.Context, appID string, userID int64, databaseName string) (int64, error) {
	if existing, err := s.GetPHPPanelDatabaseByName(ctx, appID, databaseName); err == nil {
		if existing.UserID == userID {
			return existing.ID, nil
		}
	}
	res, err := s.db.ExecContext(ctx, `
INSERT INTO php_panel_databases(app_id, user_id, database_name, created_at)
VALUES(?,?,?,?)`,
		appID, userID, strings.TrimSpace(databaseName), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListPHPPanelDatabasesByOwner(ctx context.Context, appID string, userID int64) ([]PHPPanelDatabase, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, app_id, user_id, database_name, created_at
FROM php_panel_databases
WHERE app_id = ? AND user_id = ?
ORDER BY database_name ASC`, appID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PHPPanelDatabase
	for rows.Next() {
		var item PHPPanelDatabase
		var created string
		if err := rows.Scan(&item.ID, &item.AppID, &item.UserID, &item.DatabaseName, &created); err != nil {
			return nil, err
		}
		item.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) GetPHPPanelDatabaseByName(ctx context.Context, appID, databaseName string) (PHPPanelDatabase, error) {
	var item PHPPanelDatabase
	var created string
	err := s.db.QueryRowContext(ctx, `
SELECT id, app_id, user_id, database_name, created_at
FROM php_panel_databases
WHERE app_id = ? AND database_name = ?`,
		appID, strings.TrimSpace(databaseName)).
		Scan(&item.ID, &item.AppID, &item.UserID, &item.DatabaseName, &created)
	if err != nil {
		return PHPPanelDatabase{}, err
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return item, nil
}

func (s *Store) DeleteOwnedPHPPanelDatabase(ctx context.Context, appID string, userID int64, databaseName string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM php_panel_databases WHERE app_id = ? AND user_id = ? AND database_name = ?`, appID, userID, strings.TrimSpace(databaseName))
	return err
}

func (s *Store) UpsertPHPPanelDBUser(ctx context.Context, item PHPPanelDBUser) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	var existingID int64
	var existingOwner int64
	err := s.db.QueryRowContext(ctx, `SELECT id, user_id FROM php_panel_db_users WHERE app_id = ? AND username = ? AND host = ?`,
		item.AppID, strings.TrimSpace(item.Username), strings.TrimSpace(item.Host)).Scan(&existingID, &existingOwner)
	if err == nil {
		if existingOwner != item.UserID {
			return 0, sql.ErrNoRows
		}
		_, err = s.db.ExecContext(ctx, `
UPDATE php_panel_db_users
SET user_id = ?, password_encrypted = ?, updated_at = ?
WHERE id = ?`,
			item.UserID, item.PasswordEncrypted, now, existingID)
		return existingID, err
	}
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx, `
INSERT INTO php_panel_db_users(app_id, user_id, username, host, password_encrypted, created_at, updated_at)
VALUES(?,?,?,?,?,?,?)`,
		item.AppID, item.UserID, strings.TrimSpace(item.Username), strings.TrimSpace(item.Host), item.PasswordEncrypted, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListPHPPanelDBUsersByOwner(ctx context.Context, appID string, userID int64) ([]PHPPanelDBUser, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, app_id, user_id, username, host, password_encrypted, created_at, updated_at
FROM php_panel_db_users
WHERE app_id = ? AND user_id = ?
ORDER BY username ASC, host ASC`, appID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PHPPanelDBUser
	for rows.Next() {
		var item PHPPanelDBUser
		var created, updated string
		if err := rows.Scan(&item.ID, &item.AppID, &item.UserID, &item.Username, &item.Host, &item.PasswordEncrypted, &created, &updated); err != nil {
			return nil, err
		}
		item.CreatedAt, _ = time.Parse(time.RFC3339, created)
		item.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) GetPHPPanelDBUserByID(ctx context.Context, id int64) (PHPPanelDBUser, error) {
	var item PHPPanelDBUser
	var created, updated string
	err := s.db.QueryRowContext(ctx, `
SELECT id, app_id, user_id, username, host, password_encrypted, created_at, updated_at
FROM php_panel_db_users
WHERE id = ?`, id).
		Scan(&item.ID, &item.AppID, &item.UserID, &item.Username, &item.Host, &item.PasswordEncrypted, &created, &updated)
	if err != nil {
		return PHPPanelDBUser{}, err
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339, created)
	item.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return item, nil
}

func (s *Store) GetPHPPanelDBUserByNameHost(ctx context.Context, appID, username, host string) (PHPPanelDBUser, error) {
	var item PHPPanelDBUser
	var created, updated string
	err := s.db.QueryRowContext(ctx, `
SELECT id, app_id, user_id, username, host, password_encrypted, created_at, updated_at
FROM php_panel_db_users
WHERE app_id = ? AND username = ? AND host = ?`,
		appID, strings.TrimSpace(username), strings.TrimSpace(host)).
		Scan(&item.ID, &item.AppID, &item.UserID, &item.Username, &item.Host, &item.PasswordEncrypted, &created, &updated)
	if err != nil {
		return PHPPanelDBUser{}, err
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339, created)
	item.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return item, nil
}

func (s *Store) DeleteOwnedPHPPanelDBUser(ctx context.Context, appID string, userID int64, username, host string) error {
	_, err := s.db.ExecContext(ctx, `
DELETE FROM php_panel_db_users
WHERE app_id = ? AND user_id = ? AND username = ? AND host = ?`,
		appID, userID, strings.TrimSpace(username), strings.TrimSpace(host))
	return err
}

func (s *Store) UpsertPHPPanelDBGrant(ctx context.Context, dbUserID int64, databaseName, privilegesJSON string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var existingID int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM php_panel_db_grants WHERE db_user_id = ? AND database_name = ?`, dbUserID, strings.TrimSpace(databaseName)).Scan(&existingID)
	if err == nil {
		_, err = s.db.ExecContext(ctx, `
UPDATE php_panel_db_grants SET privileges_json = ?, updated_at = ? WHERE id = ?`,
			privilegesJSON, now, existingID)
		return err
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO php_panel_db_grants(db_user_id, database_name, privileges_json, created_at, updated_at)
VALUES(?,?,?,?,?)`,
		dbUserID, strings.TrimSpace(databaseName), privilegesJSON, now, now)
	return err
}

func (s *Store) ListPHPPanelDBGrantsForUser(ctx context.Context, dbUserID int64) ([]PHPPanelDBGrant, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, db_user_id, database_name, privileges_json, created_at, updated_at
FROM php_panel_db_grants
WHERE db_user_id = ?
ORDER BY database_name ASC`, dbUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PHPPanelDBGrant
	for rows.Next() {
		var item PHPPanelDBGrant
		var created, updated string
		if err := rows.Scan(&item.ID, &item.DBUserID, &item.DatabaseName, &item.PrivilegesJSON, &created, &updated); err != nil {
			return nil, err
		}
		item.CreatedAt, _ = time.Parse(time.RFC3339, created)
		item.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) DeletePHPPanelDBGrant(ctx context.Context, dbUserID int64, databaseName string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM php_panel_db_grants WHERE db_user_id = ? AND database_name = ?`, dbUserID, strings.TrimSpace(databaseName))
	return err
}

func (s *Store) CreatePHPPanelImpersonation(ctx context.Context, token string, adminUserID, userID int64, expiresAt time.Time) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO php_panel_impersonations(token, admin_user_id, user_id, expires_at, created_at)
VALUES(?,?,?,?,?)`,
		token, adminUserID, userID, expiresAt.UTC().Format(time.RFC3339), now)
	return err
}

func (s *Store) GetPHPPanelImpersonation(ctx context.Context, token string) (PHPPanelImpersonation, error) {
	var item PHPPanelImpersonation
	var expiresAt, createdAt string
	err := s.db.QueryRowContext(ctx, `
SELECT token, admin_user_id, user_id, expires_at, created_at
FROM php_panel_impersonations
WHERE token = ?`, token).
		Scan(&item.Token, &item.AdminUserID, &item.UserID, &expiresAt, &createdAt)
	if err != nil {
		return PHPPanelImpersonation{}, err
	}
	item.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
	item.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return item, nil
}

func (s *Store) DeletePHPPanelImpersonation(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM php_panel_impersonations WHERE token = ?`, token)
	return err
}

// DeleteAllPHPPanelDataForUser removes every PHP Panel row owned by a user:
// sites, domain ownership rows, databases, db_users (+ their grants), account, and impersonations.
// It returns the list of site slugs and database names so the caller can clean up disk/MySQL.
func (s *Store) DeleteAllPHPPanelDataForUser(ctx context.Context, appID string, userID int64) (slugs []string, dbNames []string, dbUsers []PHPPanelDBUser, err error) {
	// 1. collect sites
	siteRows, _ := s.ListPHPPanelSitesByOwner(ctx, appID, userID)
	for _, site := range siteRows {
		slugs = append(slugs, site.Slug)
	}

	// 2. collect databases
	dbRows, _ := s.ListPHPPanelDatabasesByOwner(ctx, appID, userID)
	for _, d := range dbRows {
		dbNames = append(dbNames, d.DatabaseName)
	}

	// 3. collect db_users
	dbUsers, _ = s.ListPHPPanelDBUsersByOwner(ctx, appID, userID)

	// 4. delete db_grants for every db_user
	for _, u := range dbUsers {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM php_panel_db_grants WHERE db_user_id = ?`, u.ID)
	}

	// 5. delete domain ownership rows (app_domains + template_app_domains via cascade FK or explicit)
	domainOwners, _ := s.ListPHPPanelDomainOwners(ctx, appID, userID)
	for _, o := range domainOwners {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM template_app_domains WHERE app_domain_id = ?`, o.AppDomainID)
		_, _ = s.db.ExecContext(ctx, `DELETE FROM php_panel_domain_owners WHERE app_domain_id = ?`, o.AppDomainID)
		_, _ = s.db.ExecContext(ctx, `DELETE FROM app_domains WHERE id = ?`, o.AppDomainID)
	}

	// 6. delete sites, databases, db_users, account, impersonations
	_, _ = s.db.ExecContext(ctx, `DELETE FROM app_php_sites WHERE app_id = ? AND user_id = ?`, appID, userID)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM php_panel_databases WHERE app_id = ? AND user_id = ?`, appID, userID)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM php_panel_db_users WHERE app_id = ? AND user_id = ?`, appID, userID)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM php_panel_accounts WHERE user_id = ?`, userID)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM php_panel_impersonations WHERE user_id = ? OR admin_user_id = ?`, userID, userID)

	return slugs, dbNames, dbUsers, nil
}
