package db

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

func (s *Store) UpsertTemplateAppDomain(ctx context.Context, item TemplateAppDomain) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var existingID int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM template_app_domains WHERE app_domain_id = ?`, item.AppDomainID).Scan(&existingID)
	if err == nil {
		_, err = s.db.ExecContext(ctx, `UPDATE template_app_domains SET app_id=?, template_id=?, site_slug=?, root_path=?, php_version=? WHERE app_domain_id=?`,
			item.AppID, strings.TrimSpace(item.TemplateID), strings.TrimSpace(item.SiteSlug), strings.TrimSpace(item.RootPath), strings.TrimSpace(item.PHPVersion), item.AppDomainID)
		return err
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO template_app_domains(app_domain_id, app_id, template_id, site_slug, root_path, php_version, created_at) VALUES(?,?,?,?,?,?,?)`,
		item.AppDomainID, item.AppID, strings.TrimSpace(item.TemplateID), strings.TrimSpace(item.SiteSlug), strings.TrimSpace(item.RootPath), strings.TrimSpace(item.PHPVersion), now,
	)
	return err
}

func (s *Store) GetTemplateAppDomainByDomainID(ctx context.Context, appDomainID int64) (TemplateAppDomain, error) {
	var item TemplateAppDomain
	var created string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, app_domain_id, app_id, template_id, site_slug, root_path, php_version, created_at FROM template_app_domains WHERE app_domain_id = ?`,
		appDomainID,
	).Scan(&item.ID, &item.AppDomainID, &item.AppID, &item.TemplateID, &item.SiteSlug, &item.RootPath, &item.PHPVersion, &created)
	if err != nil {
		return TemplateAppDomain{}, err
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return item, nil
}

func (s *Store) ListTemplateAppDomains(ctx context.Context, appID string) ([]TemplateAppDomain, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, app_domain_id, app_id, template_id, site_slug, root_path, php_version, created_at FROM template_app_domains WHERE app_id = ? ORDER BY id ASC`,
		appID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TemplateAppDomain
	for rows.Next() {
		var item TemplateAppDomain
		var created string
		if err := rows.Scan(&item.ID, &item.AppDomainID, &item.AppID, &item.TemplateID, &item.SiteSlug, &item.RootPath, &item.PHPVersion, &created); err != nil {
			return nil, err
		}
		item.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) DeleteTemplateAppDomainByDomainID(ctx context.Context, appDomainID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM template_app_domains WHERE app_domain_id = ?`, appDomainID)
	return err
}
