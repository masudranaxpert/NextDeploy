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

// AllocatePort registers a host port for an app. Fails if another app holds the port or if port is reserved.
func (s *Store) AllocatePort(ctx context.Context, port int, bindIP, appID, service, protocol string) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("invalid port number: %d", port)
	}
	if desc, reserved := reservedSystemPorts[port]; reserved {
		return fmt.Errorf("port %d is reserved for %s and cannot be allocated", port, desc)
	}
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return errors.New("app_id required")
	}
	if bindIP == "" {
		bindIP = "0.0.0.0"
	}
	if protocol == "" {
		protocol = "tcp"
	}

	var existingAppID string
	err := s.db.QueryRowContext(ctx, `SELECT app_id FROM allocated_ports WHERE port = ?`, port).Scan(&existingAppID)
	if err == nil {
		if existingAppID != appID {
			return fmt.Errorf("port %d is already allocated by app %q", port, existingAppID)
		}
		// Same app owns it; update service/bindIP/protocol
		_, err = s.db.ExecContext(ctx,
			`UPDATE allocated_ports SET bind_ip = ?, service = ?, protocol = ?, created_at = ? WHERE port = ?`,
			bindIP, service, protocol, time.Now().UTC().Format(time.RFC3339), port)
		return err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO allocated_ports (port, bind_ip, app_id, service, protocol, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		port, bindIP, appID, service, protocol, time.Now().UTC().Format(time.RFC3339))
	return err
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

// ListAllocatedPorts returns all active host port allocations.
func (s *Store) ListAllocatedPorts(ctx context.Context) ([]AllocatedPort, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT port, bind_ip, app_id, service, protocol, created_at FROM allocated_ports ORDER BY port ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AllocatedPort
	for rows.Next() {
		var p AllocatedPort
		var created string
		if err := rows.Scan(&p.Port, &p.BindIP, &p.AppID, &p.Service, &p.Protocol, &created); err != nil {
			return nil, err
		}
		t, _ := time.Parse(time.RFC3339, created)
		p.CreatedAt = t
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetAppAllocatedPorts returns all host ports allocated to a specific app.
func (s *Store) GetAppAllocatedPorts(ctx context.Context, appID string) ([]AllocatedPort, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT port, bind_ip, app_id, service, protocol, created_at FROM allocated_ports WHERE app_id = ? ORDER BY port ASC`,
		appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AllocatedPort
	for rows.Next() {
		var p AllocatedPort
		var created string
		if err := rows.Scan(&p.Port, &p.BindIP, &p.AppID, &p.Service, &p.Protocol, &created); err != nil {
			return nil, err
		}
		t, _ := time.Parse(time.RFC3339, created)
		p.CreatedAt = t
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetPortAllocation returns allocation info for a single port, or nil if free.
func (s *Store) GetPortAllocation(ctx context.Context, port int) (*AllocatedPort, error) {
	var p AllocatedPort
	var created string
	err := s.db.QueryRowContext(ctx,
		`SELECT port, bind_ip, app_id, service, protocol, created_at FROM allocated_ports WHERE port = ?`,
		port).Scan(&p.Port, &p.BindIP, &p.AppID, &p.Service, &p.Protocol, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t, _ := time.Parse(time.RFC3339, created)
	p.CreatedAt = t
	return &p, nil
}
