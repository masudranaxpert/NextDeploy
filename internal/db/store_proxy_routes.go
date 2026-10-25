package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ProxyRoute is an admin-defined rule: send a domain to another address, or redirect it.
type ProxyRoute struct {
	ID            int64  `json:"id"`
	Kind          string `json:"kind"`            // proxy | redirect
	Domain        string `json:"domain"`          // the host name visitors use
	Target        string `json:"target"`          // proxy: http(s)://host:port ; redirect: full URL
	RedirectCode  int    `json:"redirect_code"`   // 301 | 302 | 307 | 308
	KeepPath      bool   `json:"keep_path"`       // redirect: append the visited path and query
	EnableHTTPS   bool   `json:"enable_https"`    // automatic certificate for the domain
	EnableWWW     bool   `json:"enable_www"`      // also answer on www.<domain>
	SkipTLSVerify bool   `json:"skip_tls_verify"` // proxy to an https target with a self-signed certificate
	Enabled       bool   `json:"enabled"`
	Note          string `json:"note"`
	CreatedAt     string `json:"created_at"`
}

func (s *Store) migrateProxyRoutes() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS proxy_routes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  kind TEXT NOT NULL,
  domain TEXT NOT NULL COLLATE NOCASE UNIQUE,
  target TEXT NOT NULL,
  redirect_code INTEGER NOT NULL DEFAULT 0,
  keep_path INTEGER NOT NULL DEFAULT 0,
  enable_https INTEGER NOT NULL DEFAULT 1,
  enable_www INTEGER NOT NULL DEFAULT 0,
  skip_tls_verify INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 1,
  note TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);`)
	return err
}

const proxyRouteCols = `id, kind, domain, target, redirect_code, keep_path, enable_https, enable_www, skip_tls_verify, enabled, note, created_at`

func scanProxyRoute(sc interface{ Scan(...any) error }) (ProxyRoute, error) {
	var r ProxyRoute
	var keep, https, www, skip, en int
	err := sc.Scan(&r.ID, &r.Kind, &r.Domain, &r.Target, &r.RedirectCode, &keep, &https, &www, &skip, &en, &r.Note, &r.CreatedAt)
	r.KeepPath, r.EnableHTTPS, r.EnableWWW, r.SkipTLSVerify, r.Enabled = keep == 1, https == 1, www == 1, skip == 1, en == 1
	return r, err
}

func (s *Store) ListProxyRoutes(ctx context.Context) ([]ProxyRoute, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+proxyRouteCols+` FROM proxy_routes ORDER BY domain`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProxyRoute{}
	for rows.Next() {
		r, err := scanProxyRoute(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetProxyRoute(ctx context.Context, id int64) (ProxyRoute, error) {
	return scanProxyRoute(s.db.QueryRowContext(ctx, `SELECT `+proxyRouteCols+` FROM proxy_routes WHERE id = ?`, id))
}

var ErrDomainTaken = errors.New("this domain is already used")

// DomainInUse reports whether a host name is already served by an app, a proxy route or the panel.
// exceptRouteID lets a route keep its own domain while it is edited.
func (s *Store) DomainInUse(ctx context.Context, domain string, exceptRouteID int64) (string, bool) {
	d := strings.ToLower(strings.TrimSpace(domain))
	if d == "" {
		return "", false
	}
	var x string
	if err := s.db.QueryRowContext(ctx, `SELECT app_id FROM app_domains WHERE lower(domain) IN (?, ?, ?) LIMIT 1`, d, "www."+d, strings.TrimPrefix(d, "www.")).Scan(&x); err == nil {
		return "an app", true
	}
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM proxy_routes WHERE lower(domain) IN (?, ?, ?) AND id != ? LIMIT 1`, d, "www."+d, strings.TrimPrefix(d, "www."), exceptRouteID).Scan(&x); err == nil {
		return "another route", true
	}
	if pd := strings.ToLower(strings.TrimSpace(s.GetSetting(ctx, "panel_domain"))); pd != "" && (pd == d || pd == "www."+d || "www."+pd == d) {
		return "the panel", true
	}
	return "", false
}

func (s *Store) SaveProxyRoute(ctx context.Context, r ProxyRoute) (int64, error) {
	b := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	if r.ID == 0 {
		res, err := s.db.ExecContext(ctx, `INSERT INTO proxy_routes (kind, domain, target, redirect_code, keep_path, enable_https, enable_www, skip_tls_verify, enabled, note, created_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?)`, r.Kind, r.Domain, r.Target, r.RedirectCode, b(r.KeepPath), b(r.EnableHTTPS), b(r.EnableWWW), b(r.SkipTLSVerify), b(r.Enabled), r.Note, time.Now().UTC().Format(time.RFC3339))
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE proxy_routes SET kind=?, domain=?, target=?, redirect_code=?, keep_path=?, enable_https=?, enable_www=?, skip_tls_verify=?, enabled=?, note=? WHERE id=?`,
		r.Kind, r.Domain, r.Target, r.RedirectCode, b(r.KeepPath), b(r.EnableHTTPS), b(r.EnableWWW), b(r.SkipTLSVerify), b(r.Enabled), r.Note, r.ID)
	return r.ID, err
}

func (s *Store) DeleteProxyRoute(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM proxy_routes WHERE id = ?`, id)
	return err
}

var _ = sql.ErrNoRows
