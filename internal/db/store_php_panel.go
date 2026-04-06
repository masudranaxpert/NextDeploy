package db

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

func normalizePHPVersion(v string) string {
	switch strings.TrimSpace(v) {
	case "7.4", "8.1", "8.2", "8.3":
		return strings.TrimSpace(v)
	default:
		return "8.3"
	}
}

func (s *Store) ListPHPPanelSites(ctx context.Context, appID string) ([]PHPPanelSite, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, app_id, user_id, name, slug, php_version, created_at FROM app_php_sites WHERE app_id = ? ORDER BY datetime(created_at) ASC`, appID)
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

func (s *Store) GetPHPPanelSiteBySlug(ctx context.Context, appID, slug string) (PHPPanelSite, error) {
	var item PHPPanelSite
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT id, app_id, user_id, name, slug, php_version, created_at FROM app_php_sites WHERE app_id = ? AND slug = ?`, appID, strings.TrimSpace(slug)).
		Scan(&item.ID, &item.AppID, &item.UserID, &item.Name, &item.Slug, &item.PHPVersion, &created)
	if err != nil {
		return PHPPanelSite{}, err
	}
	item.PHPVersion = normalizePHPVersion(item.PHPVersion)
	item.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return item, nil
}

func (s *Store) CreatePHPPanelSite(ctx context.Context, appID, name, slug, phpVersion string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO app_php_sites(app_id, user_id, name, slug, php_version, created_at) VALUES(?,?,?,?,?,?)`,
		appID,
		0,
		strings.TrimSpace(name),
		strings.TrimSpace(slug),
		normalizePHPVersion(phpVersion),
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdatePHPPanelSiteVersion(ctx context.Context, appID, slug, phpVersion string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE app_php_sites SET php_version = ? WHERE app_id = ? AND slug = ?`,
		normalizePHPVersion(phpVersion),
		appID,
		strings.TrimSpace(slug),
	)
	return err
}

func (s *Store) DeletePHPPanelSite(ctx context.Context, appID, slug string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM app_php_sites WHERE app_id = ? AND slug = ?`, appID, strings.TrimSpace(slug))
	return err
}

func (s *Store) HasPHPPanelSite(ctx context.Context, appID, slug string) bool {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM app_php_sites WHERE app_id = ? AND slug = ? LIMIT 1`, appID, strings.TrimSpace(slug)).Scan(&id)
	return err == nil && id > 0
}

func (s *Store) GetTemplateAppByTemplateID(ctx context.Context, templateID string) (App, error) {
	var app App
	var created string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, created_at, COALESCE(compose_file,''), COALESCE(template_id,'') FROM apps WHERE template_id = ? ORDER BY datetime(created_at) DESC LIMIT 1`,
		strings.TrimSpace(templateID),
	).Scan(&app.ID, &app.Name, &created, &app.ComposeFile, &app.TemplateID)
	if err != nil {
		return App{}, err
	}
	app.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return app, nil
}

func IsNotFound(err error) bool {
	return err == sql.ErrNoRows
}
