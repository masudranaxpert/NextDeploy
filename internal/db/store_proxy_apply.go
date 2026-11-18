package db

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"time"
)

// SettingProxyEngine is the settings table key holding the active reverse-proxy engine.
const SettingProxyEngine = "proxy_engine"

// ProxyApply records one reverse-proxy render, validation, and apply execution.
type ProxyApply struct {
	ID         int64     `json:"id"`
	Generation string    `json:"generation"`
	Driver     string    `json:"driver"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`
	AppliedAt  time.Time `json:"applied_at"`
}

func (s *Store) migrateProxyTables() error {
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS proxy_apply (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  generation TEXT NOT NULL,
  driver TEXT NOT NULL,
  status TEXT NOT NULL,
  error TEXT NOT NULL DEFAULT '',
  applied_at TEXT NOT NULL
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_proxy_apply_applied_at ON proxy_apply(applied_at);`); err != nil {
		return err
	}
	return s.seedProxyEngine(context.Background())
}

func initialProxyEngineFromEnv() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ND_PROXY_ENGINE"))) {
	case "caddy", "angie", "ferron":
		return strings.ToLower(strings.TrimSpace(os.Getenv("ND_PROXY_ENGINE")))
	default:
		return "caddy"
	}
}

func (s *Store) seedProxyEngine(ctx context.Context) error {
	var existing string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, SettingProxyEngine).Scan(&existing)
	if err == nil {
		if strings.TrimSpace(existing) != "" {
			return nil
		}
		_, err = s.db.ExecContext(ctx, `UPDATE settings SET value = ? WHERE key = ? AND trim(value) = ''`, initialProxyEngineFromEnv(), SettingProxyEngine)
		return err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO NOTHING`,
		SettingProxyEngine, initialProxyEngineFromEnv())
	return err
}

// RecordProxyApply inserts a proxy apply history entry and returns its row ID.
func (s *Store) RecordProxyApply(ctx context.Context, generation, driver, status, applyErr string) (int64, error) {
	driver = strings.TrimSpace(driver)
	status = strings.TrimSpace(status)
	if driver == "" || status == "" {
		return 0, errors.New("driver and status are required")
	}
	const maxErrLen = 65536
	if len(applyErr) > maxErrLen {
		applyErr = applyErr[:maxErrLen] + "\n… (truncated)"
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO proxy_apply(generation, driver, status, error, applied_at) VALUES(?, ?, ?, ?, ?)`,
		strings.TrimSpace(generation), driver, status, applyErr, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func scanProxyApply(row interface{ Scan(...any) error }) (ProxyApply, error) {
	var p ProxyApply
	var applied string
	if err := row.Scan(&p.ID, &p.Generation, &p.Driver, &p.Status, &p.Error, &applied); err != nil {
		return ProxyApply{}, err
	}
	if t, err := time.Parse(time.RFC3339Nano, applied); err == nil {
		p.AppliedAt = t
	} else if t, err := time.Parse(time.RFC3339, applied); err == nil {
		p.AppliedAt = t
	}
	return p, nil
}

// LatestProxyApply returns the most recent proxy apply record, or (nil, nil) if none exist.
func (s *Store) LatestProxyApply(ctx context.Context) (*ProxyApply, error) {
	p, err := scanProxyApply(s.db.QueryRowContext(ctx,
		`SELECT id, generation, driver, status, error, applied_at FROM proxy_apply ORDER BY id DESC LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ListProxyApplies returns recent proxy apply records ordered newest-first.
func (s *Store) ListProxyApplies(ctx context.Context, limit int) ([]ProxyApply, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, generation, driver, status, error, applied_at FROM proxy_apply ORDER BY id DESC LIMIT ?`,
		limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProxyApply{}
	for rows.Next() {
		p, err := scanProxyApply(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
