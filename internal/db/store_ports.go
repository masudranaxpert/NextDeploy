package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var reservedSystemPorts = map[int]string{
	22:   "SSH",
	80:   "HTTP edge proxy (Caddy)",
	443:  "HTTPS edge proxy (Caddy)",
	2019: "Caddy internal admin API",
	8080: "NextDeploy panel",
}

func normalizeAllocatedPort(port int, bindIP, appID, service, protocol string, direct bool) (AllocatedPort, error) {
	if port <= 0 || port > 65535 {
		return AllocatedPort{}, fmt.Errorf("invalid port number: %d", port)
	}
	if desc, reserved := reservedSystemPorts[port]; reserved {
		return AllocatedPort{}, fmt.Errorf("port %d is reserved for %s and cannot be allocated", port, desc)
	}
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return AllocatedPort{}, errors.New("app_id required")
	}
	if bindIP == "" {
		bindIP = "0.0.0.0"
	}
	if protocol == "" {
		protocol = "tcp"
	}
	return AllocatedPort{
		Port:     port,
		BindIP:   bindIP,
		AppID:    appID,
		Service:  service,
		Protocol: protocol,
		Direct:   direct,
	}, nil
}

// AllocatePort registers a host port for an app. Fails if another app holds the port or if port is reserved.
func (s *Store) AllocatePort(ctx context.Context, port int, bindIP, appID, service, protocol string) error {
	return s.AllocatePortMode(ctx, port, bindIP, appID, service, protocol, false)
}

// AllocatePortMode registers a host port for an app with explicit direct vs gateway mode.
func (s *Store) AllocatePortMode(ctx context.Context, port int, bindIP, appID, service, protocol string, direct bool) error {
	ap, err := normalizeAllocatedPort(port, bindIP, appID, service, protocol, direct)
	if err != nil {
		return err
	}

	var existingAppID string
	err = s.db.QueryRowContext(ctx, `SELECT app_id FROM allocated_ports WHERE port = ?`, ap.Port).Scan(&existingAppID)
	now := time.Now().UTC().Format(time.RFC3339)
	directInt := 0
	if ap.Direct {
		directInt = 1
	}
	if err == nil {
		if existingAppID != ap.AppID {
			return fmt.Errorf("port %d is already allocated by app %q", ap.Port, existingAppID)
		}
		_, err = s.db.ExecContext(ctx,
			`UPDATE allocated_ports SET bind_ip = ?, service = ?, protocol = ?, direct = ?, created_at = ? WHERE port = ?`,
			ap.BindIP, ap.Service, ap.Protocol, directInt, now, ap.Port)
		return err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO allocated_ports (port, bind_ip, app_id, service, protocol, direct, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		ap.Port, ap.BindIP, ap.AppID, ap.Service, ap.Protocol, directInt, now)
	return err
}

// SyncAppPorts atomically reconciles all host port allocations for an app,
// failing before mutating DB if any requested port is reserved or held by another app.
func (s *Store) SyncAppPorts(ctx context.Context, appID string, desired []AllocatedPort) error {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return errors.New("app_id required")
	}
	normalized := make([]AllocatedPort, 0, len(desired))
	seen := make(map[int]struct{}, len(desired))
	for _, d := range desired {
		ap, err := normalizeAllocatedPort(d.Port, d.BindIP, appID, d.Service, d.Protocol, d.Direct)
		if err != nil {
			return err
		}
		if _, dup := seen[ap.Port]; dup {
			continue
		}
		seen[ap.Port] = struct{}{}
		normalized = append(normalized, ap)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, ap := range normalized {
		var existingAppID string
		err := tx.QueryRowContext(ctx, `SELECT app_id FROM allocated_ports WHERE port = ?`, ap.Port).Scan(&existingAppID)
		if err == nil && existingAppID != appID {
			return fmt.Errorf("port %d is already allocated by app %q", ap.Port, existingAppID)
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM allocated_ports WHERE app_id = ?`, appID); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, ap := range normalized {
		directInt := 0
		if ap.Direct {
			directInt = 1
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO allocated_ports (port, bind_ip, app_id, service, protocol, direct, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			ap.Port, ap.BindIP, ap.AppID, ap.Service, ap.Protocol, directInt, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ReleaseAppPorts frees all host ports allocated to an app.
func (s *Store) ReleaseAppPorts(ctx context.Context, appID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM allocated_ports WHERE app_id = ?`, appID)
	return err
}

// ReleasePort frees a specific host port.
func (s *Store) ReleasePort(ctx context.Context, port int) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM allocated_ports WHERE port = ?`, port)
	return err
}

func scanAllocatedPorts(rows *sql.Rows) ([]AllocatedPort, error) {
	var out []AllocatedPort
	for rows.Next() {
		var p AllocatedPort
		var created string
		if err := rows.Scan(&p.Port, &p.BindIP, &p.AppID, &p.Service, &p.Protocol, &p.Direct, &created); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListAllocatedPorts returns all active host port allocations.
func (s *Store) ListAllocatedPorts(ctx context.Context) ([]AllocatedPort, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT port, bind_ip, app_id, service, protocol, direct, created_at FROM allocated_ports ORDER BY port ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAllocatedPorts(rows)
}

// GetAppAllocatedPorts returns all host ports allocated to a specific app.
func (s *Store) GetAppAllocatedPorts(ctx context.Context, appID string) ([]AllocatedPort, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT port, bind_ip, app_id, service, protocol, direct, created_at FROM allocated_ports WHERE app_id = ? ORDER BY port ASC`,
		appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAllocatedPorts(rows)
}

// GetPortAllocation returns allocation info for a single port, or nil if free.
func (s *Store) GetPortAllocation(ctx context.Context, port int) (*AllocatedPort, error) {
	var p AllocatedPort
	var created string
	err := s.db.QueryRowContext(ctx,
		`SELECT port, bind_ip, app_id, service, protocol, direct, created_at FROM allocated_ports WHERE port = ?`,
		port).Scan(&p.Port, &p.BindIP, &p.AppID, &p.Service, &p.Protocol, &p.Direct, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return &p, nil
}
