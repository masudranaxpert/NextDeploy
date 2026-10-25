package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// AppLimits is the memory and CPU ceiling an owner set for one project. 0 means "no ceiling of
// its own": the project then shares whatever the owner's account allows.
type AppLimits struct {
	AppID    string  `json:"app_id"`
	MemoryMB int     `json:"memory_mb"`
	CPUs     float64 `json:"cpus"`
}

// GetAppLimits returns the project's limits; a project without any has the zero value.
func (s *Store) GetAppLimits(ctx context.Context, appID string) (AppLimits, error) {
	l := AppLimits{AppID: appID}
	err := s.db.QueryRowContext(ctx, `SELECT memory_mb, cpus FROM app_limits WHERE app_id = ?`, appID).Scan(&l.MemoryMB, &l.CPUs)
	if errors.Is(err, sql.ErrNoRows) {
		return l, nil
	}
	return l, err
}

// SetAppLimits stores the limits, or forgets them when both are 0.
func (s *Store) SetAppLimits(ctx context.Context, l AppLimits) error {
	if l.MemoryMB <= 0 && l.CPUs <= 0 {
		_, err := s.db.ExecContext(ctx, `DELETE FROM app_limits WHERE app_id = ?`, l.AppID)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO app_limits (app_id, memory_mb, cpus, updated_at) VALUES (?,?,?,?)
		 ON CONFLICT(app_id) DO UPDATE SET memory_mb = excluded.memory_mb, cpus = excluded.cpus, updated_at = excluded.updated_at`,
		l.AppID, l.MemoryMB, l.CPUs, time.Now().UTC().Format(time.RFC3339))
	return err
}

// ListAppLimits returns every project that has a ceiling of its own.
func (s *Store) ListAppLimits(ctx context.Context) ([]AppLimits, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT app_id, memory_mb, cpus FROM app_limits`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AppLimits
	for rows.Next() {
		var l AppLimits
		if err := rows.Scan(&l.AppID, &l.MemoryMB, &l.CPUs); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
