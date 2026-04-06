package db

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

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
