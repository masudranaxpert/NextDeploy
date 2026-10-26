package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (s *Store) ListAppDomains(ctx context.Context, appID string) ([]AppDomain, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,app_id,domain,service,port,COALESCE(is_primary,0),enable_https,enable_www,serve_static,static_path,static_url_prefix,serve_media,media_path,media_url_prefix,COALESCE(route_rules_json,'[]'),shield_node_id,created_at
		 FROM app_domains WHERE app_id=?
		 ORDER BY COALESCE(is_primary,0) DESC, id ASC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AppDomain
	for rows.Next() {
		var d AppDomain
		var isPrim, https, www, static, media int
		var shieldID sql.NullInt64
		var created string
		if err := rows.Scan(&d.ID, &d.AppID, &d.Domain, &d.Service, &d.Port, &isPrim,
			&https, &www, &static, &d.StaticPath, &d.StaticURLPrefix, &media, &d.MediaPath, &d.MediaURLPrefix, &d.RouteRulesJSON, &shieldID, &created); err != nil {
			return nil, err
		}
		if shieldID.Valid {
			d.ShieldNodeID = &shieldID.Int64
		}
		d.IsPrimary = isPrim != 0
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
	var isPrim, https, www, static, media int
	var shieldID sql.NullInt64
	var created string
	err := s.db.QueryRowContext(ctx,
		`SELECT id,app_id,domain,service,port,COALESCE(is_primary,0),enable_https,enable_www,serve_static,static_path,static_url_prefix,serve_media,media_path,media_url_prefix,COALESCE(route_rules_json,'[]'),shield_node_id,created_at
		 FROM app_domains WHERE id=?`, id).Scan(
		&d.ID, &d.AppID, &d.Domain, &d.Service, &d.Port, &isPrim,
		&https, &www, &static, &d.StaticPath, &d.StaticURLPrefix, &media, &d.MediaPath, &d.MediaURLPrefix, &d.RouteRulesJSON, &shieldID, &created)
	if err != nil {
		return AppDomain{}, err
	}
	if shieldID.Valid {
		d.ShieldNodeID = &shieldID.Int64
	}
	d.IsPrimary = isPrim != 0
	d.EnableHTTPS = https != 0
	d.EnableWWW = www != 0
	d.ServeStatic = static != 0
	d.ServeMedia = media != 0
	t, _ := time.Parse(time.RFC3339, created)
	d.CreatedAt = t
	return d, nil
}

func (s *Store) CreateAppDomain(ctx context.Context, d AppDomain) (int64, error) {
	cleanDomain := strings.TrimSpace(strings.ToLower(d.Domain))
	if cleanDomain != "" {
		var existingAppID, siteName string
		err := s.db.QueryRowContext(ctx, `SELECT ad.app_id, COALESCE(a.name,'') FROM app_domains ad LEFT JOIN apps a ON a.id = ad.app_id WHERE LOWER(ad.domain) = ? LIMIT 1`, cleanDomain).Scan(&existingAppID, &siteName)
		if err == nil && existingAppID != d.AppID {
			if siteName != "" {
				return 0, fmt.Errorf("domain %q is already used by site %q", d.Domain, siteName)
			}
			return 0, fmt.Errorf("domain %q is not available", d.Domain)
		}
	}

	if d.IsPrimary {
		_, _ = s.db.ExecContext(ctx, `UPDATE app_domains SET is_primary=0 WHERE app_id=?`, d.AppID)
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO app_domains(app_id,domain,service,port,is_primary,enable_https,enable_www,serve_static,static_path,static_url_prefix,serve_media,media_path,media_url_prefix,route_rules_json,shield_node_id,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.AppID, d.Domain, d.Service, d.Port,
		boolInt(d.IsPrimary),
		boolInt(d.EnableHTTPS), boolInt(d.EnableWWW),
		boolInt(d.ServeStatic), d.StaticPath, d.StaticURLPrefix,
		boolInt(d.ServeMedia), d.MediaPath, d.MediaURLPrefix, normalizeRulesJSON(d.RouteRulesJSON),
		d.ShieldNodeID,
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateAppDomain(ctx context.Context, d AppDomain) error {
	cleanDomain := strings.TrimSpace(strings.ToLower(d.Domain))
	if cleanDomain != "" {
		var existingAppID, siteName string
		err := s.db.QueryRowContext(ctx, `SELECT ad.app_id, COALESCE(a.name,'') FROM app_domains ad LEFT JOIN apps a ON a.id = ad.app_id WHERE LOWER(ad.domain) = ? AND ad.id != ? LIMIT 1`, cleanDomain, d.ID).Scan(&existingAppID, &siteName)
		if err == nil && existingAppID != d.AppID {
			if siteName != "" {
				return fmt.Errorf("domain %q is already used by site %q", d.Domain, siteName)
			}
			return fmt.Errorf("domain %q is not available", d.Domain)
		}
	}

	if d.IsPrimary {
		_, _ = s.db.ExecContext(ctx, `UPDATE app_domains SET is_primary=0 WHERE app_id=? AND id!=?`, d.AppID, d.ID)
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE app_domains SET domain=?,service=?,port=?,is_primary=?,enable_https=?,enable_www=?,serve_static=?,static_path=?,static_url_prefix=?,serve_media=?,media_path=?,media_url_prefix=?,route_rules_json=?,shield_node_id=?
		 WHERE id=?`,
		d.Domain, d.Service, d.Port,
		boolInt(d.IsPrimary),
		boolInt(d.EnableHTTPS), boolInt(d.EnableWWW),
		boolInt(d.ServeStatic), d.StaticPath, d.StaticURLPrefix,
		boolInt(d.ServeMedia), d.MediaPath, d.MediaURLPrefix, normalizeRulesJSON(d.RouteRulesJSON),
		d.ShieldNodeID,
		d.ID)
	return err
}

func (s *Store) SetPrimaryAppDomain(ctx context.Context, appID string, domainID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE app_domains SET is_primary=0 WHERE app_id=?`, appID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE app_domains SET is_primary=1 WHERE app_id=? AND id=?`, appID, domainID); err != nil {
		return err
	}
	return tx.Commit()
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
