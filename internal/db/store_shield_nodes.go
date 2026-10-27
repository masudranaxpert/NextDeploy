package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"strings"
	"time"
)

type ShieldNode struct {
	ID              int64      `json:"id"`
	Name            string     `json:"name"`
	IP              string     `json:"ip"`
	Token           string     `json:"token"`
	Status          string     `json:"status"` // 'pending', 'connected', 'offline'
	AssignedDomains int        `json:"assigned_domains"`
	LastHeartbeatAt *time.Time `json:"last_heartbeat_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

func (s *Store) migrateShieldNodes() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS shield_nodes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		ip TEXT NOT NULL,
		token TEXT NOT NULL UNIQUE,
		status TEXT NOT NULL DEFAULT 'pending',
		last_heartbeat_at DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`); err != nil {
		return err
	}

	var hasCol int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name = 'default_shield_id'`).Scan(&hasCol)
	if hasCol == 0 {
		_, _ = s.db.Exec(`ALTER TABLE users ADD COLUMN default_shield_id INTEGER DEFAULT NULL`)
	}

	hasCol = 0
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('app_domains') WHERE name = 'shield_node_id'`).Scan(&hasCol)
	if hasCol == 0 {
		_, _ = s.db.Exec(`ALTER TABLE app_domains ADD COLUMN shield_node_id INTEGER DEFAULT NULL`)
	}

	hasCol = 0
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxy_routes') WHERE name = 'shield_node_id'`).Scan(&hasCol)
	if hasCol == 0 {
		_, _ = s.db.Exec(`ALTER TABLE proxy_routes ADD COLUMN shield_node_id INTEGER DEFAULT NULL`)
	}

	// Auto-heal existing shielded records so UI toggle accurately reflects HTTPS
	_, _ = s.db.Exec(`UPDATE app_domains SET enable_https = 1 WHERE shield_node_id IS NOT NULL AND enable_https = 0`)
	_, _ = s.db.Exec(`UPDATE proxy_routes SET enable_https = 1 WHERE shield_node_id IS NOT NULL AND enable_https = 0`)

	return nil
}

func GenerateShieldToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return "shld_" + hex.EncodeToString(b)
}

func (s *Store) ListShieldNodes(ctx context.Context) ([]ShieldNode, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT n.id, n.name, n.ip, n.token, n.status, n.last_heartbeat_at, n.created_at,
		       (SELECT COUNT(1) FROM app_domains WHERE shield_node_id = n.id) +
		       (SELECT COUNT(1) FROM proxy_routes WHERE shield_node_id = n.id) as assigned_domains
		FROM shield_nodes n
		ORDER BY n.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []ShieldNode
	now := time.Now().UTC()
	for rows.Next() {
		var n ShieldNode
		var lastHb sql.NullString
		var createdStr string
		if err := rows.Scan(&n.ID, &n.Name, &n.IP, &n.Token, &n.Status, &lastHb, &createdStr, &n.AssignedDomains); err != nil {
			return nil, err
		}
		if lastHb.Valid && lastHb.String != "" {
			t, err := time.Parse("2006-01-02 15:04:05", lastHb.String)
			if err != nil {
				t, _ = time.Parse(time.RFC3339, lastHb.String)
			}
			n.LastHeartbeatAt = &t
			// Auto mark offline if no heartbeat in 10 minutes
			if n.Status == "connected" && now.Sub(t) > 10*time.Minute {
				n.Status = "offline"
			}
		}
		t, _ := time.Parse("2006-01-02 15:04:05", createdStr)
		if t.IsZero() {
			t, _ = time.Parse(time.RFC3339, createdStr)
		}
		n.CreatedAt = t
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

func (s *Store) GetShieldNode(ctx context.Context, id int64) (*ShieldNode, error) {
	var n ShieldNode
	var lastHb sql.NullString
	var createdStr string
	err := s.db.QueryRowContext(ctx, `
		SELECT n.id, n.name, n.ip, n.token, n.status, n.last_heartbeat_at, n.created_at,
		       (SELECT COUNT(1) FROM app_domains WHERE shield_node_id = n.id) +
		       (SELECT COUNT(1) FROM proxy_routes WHERE shield_node_id = n.id) as assigned_domains
		FROM shield_nodes n WHERE n.id = ?
	`, id).Scan(&n.ID, &n.Name, &n.IP, &n.Token, &n.Status, &lastHb, &createdStr, &n.AssignedDomains)
	if err != nil {
		return nil, err
	}
	if lastHb.Valid && lastHb.String != "" {
		t, err := time.Parse("2006-01-02 15:04:05", lastHb.String)
		if err != nil {
			t, _ = time.Parse(time.RFC3339, lastHb.String)
		}
		n.LastHeartbeatAt = &t
	}
	t, _ := time.Parse("2006-01-02 15:04:05", createdStr)
	if t.IsZero() {
		t, _ = time.Parse(time.RFC3339, createdStr)
	}
	n.CreatedAt = t
	return &n, nil
}

func (s *Store) GetShieldNodeByToken(ctx context.Context, token string) (*ShieldNode, error) {
	var n ShieldNode
	var lastHb sql.NullString
	var createdStr string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, ip, token, status, last_heartbeat_at, created_at
		FROM shield_nodes WHERE token = ?
	`, token).Scan(&n.ID, &n.Name, &n.IP, &n.Token, &n.Status, &lastHb, &createdStr)
	if err != nil {
		return nil, err
	}
	if lastHb.Valid && lastHb.String != "" {
		t, err := time.Parse("2006-01-02 15:04:05", lastHb.String)
		if err != nil {
			t, _ = time.Parse(time.RFC3339, lastHb.String)
		}
		n.LastHeartbeatAt = &t
	}
	t, _ := time.Parse("2006-01-02 15:04:05", createdStr)
	if t.IsZero() {
		t, _ = time.Parse(time.RFC3339, createdStr)
	}
	n.CreatedAt = t
	return &n, nil
}

func (s *Store) CreateShieldNode(ctx context.Context, n *ShieldNode) (int64, error) {
	if n.Token == "" {
		n.Token = GenerateShieldToken()
	}
	if n.Status == "" {
		n.Status = "pending"
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO shield_nodes (name, ip, token, status, created_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, strings.TrimSpace(n.Name), strings.TrimSpace(n.IP), n.Token, n.Status)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateShieldNode(ctx context.Context, n *ShieldNode) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE shield_nodes SET name = ?, ip = ? WHERE id = ?
	`, strings.TrimSpace(n.Name), strings.TrimSpace(n.IP), n.ID)
	return err
}

func (s *Store) DeleteShieldNode(ctx context.Context, id int64) error {
	// Unlink from users, domains and proxy routes
	_, _ = s.db.ExecContext(ctx, `UPDATE users SET default_shield_id = NULL WHERE default_shield_id = ?`, id)
	_, _ = s.db.ExecContext(ctx, `UPDATE app_domains SET shield_node_id = NULL WHERE shield_node_id = ?`, id)
	_, _ = s.db.ExecContext(ctx, `UPDATE proxy_routes SET shield_node_id = NULL WHERE shield_node_id = ?`, id)

	_, err := s.db.ExecContext(ctx, `DELETE FROM shield_nodes WHERE id = ?`, id)
	return err
}

func (s *Store) UpdateShieldNodeHeartbeat(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE shield_nodes SET status = 'connected', last_heartbeat_at = CURRENT_TIMESTAMP WHERE id = ?
	`, id)
	return err
}

func (s *Store) UpdateShieldNodeStatus(ctx context.Context, id int64, status string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE shield_nodes SET status = ? WHERE id = ?
	`, status, id)
	return err
}
