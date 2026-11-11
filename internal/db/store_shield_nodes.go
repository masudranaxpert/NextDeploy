package db

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"strings"
	"time"
)

// ShieldNode represents an Edge Shield proxy node registered with the panel.
type ShieldNode struct {
	ID                 int64                `json:"id"`
	Name               string               `json:"name"`
	IP                 string               `json:"ip"`
	Token              string               `json:"token"`
	Status             string               `json:"status"` // 'pending', 'connected', 'offline', 'unregistered'
	AssignedDomains    int                  `json:"assigned_domains"`
	LastHeartbeatAt    *time.Time           `json:"last_heartbeat_at,omitempty"`
	EdgeID             string               `json:"edge_id,omitempty"`
	EdgeVersion        string               `json:"edge_version,omitempty"`
	CaddyVersion       string               `json:"caddy_version,omitempty"`
	RegisteredAt       *time.Time           `json:"registered_at,omitempty"`
	LastSyncAt         *time.Time           `json:"last_sync_at,omitempty"`
	LastAppliedETag    string               `json:"last_applied_etag,omitempty"`
	LastError          string               `json:"last_error,omitempty"`
	EdgeStatusJSON     string               `json:"edge_status_json,omitempty"`
	SyncState          string               `json:"sync_state,omitempty"` // ok | stale | auth_failed | outdated | pending | unregistered
	ConflictsCount     int                  `json:"conflicts_count"`
	DomainStatuses     []ShieldDomainStatus `json:"domain_statuses,omitempty"`
	TokenPrev          string               `json:"-"`
	TokenPrevExpiresAt *time.Time           `json:"-"`
	CreatedAt          time.Time            `json:"created_at"`
}

// ShieldDomainStatus stores the latest per-host state reported by a Shield node.
type ShieldDomainStatus struct {
	NodeID       int64      `json:"node_id"`
	Host         string     `json:"host"`
	State        string     `json:"state"`
	Detail       string     `json:"detail,omitempty"`
	CertNotAfter *time.Time `json:"cert_not_after,omitempty"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func addColumnIfMissing(db *sql.DB, table, column, colDef string) error {
	var hasCol int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('` + table + `') WHERE name = ?`, column).Scan(&hasCol); err != nil {
		return err
	}
	if hasCol == 0 {
		if _, err := db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + colDef); err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
				return err
			}
		}
	}
	return nil
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

	if err := addColumnIfMissing(s.db, "users", "default_shield_id", "INTEGER DEFAULT NULL"); err != nil {
		return err
	}
	if err := addColumnIfMissing(s.db, "app_domains", "shield_node_id", "INTEGER DEFAULT NULL"); err != nil {
		return err
	}
	if err := addColumnIfMissing(s.db, "app_domains", "pending_origin_apply", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := addColumnIfMissing(s.db, "app_domains", "trusted_proxy", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := addColumnIfMissing(s.db, "proxy_routes", "shield_node_id", "INTEGER DEFAULT NULL"); err != nil {
		return err
	}
	if err := addColumnIfMissing(s.db, "proxy_routes", "pending_origin_apply", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := addColumnIfMissing(s.db, "proxy_routes", "trusted_proxy", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}

	// §2.2 edge v2 columns on shield_nodes
	for _, col := range []struct {
		name string
		def  string
	}{
		{"edge_id", "TEXT"},
		{"edge_version", "TEXT"},
		{"caddy_version", "TEXT"},
		{"registered_at", "DATETIME"},
		{"last_sync_at", "DATETIME"},
		{"last_applied_etag", "TEXT"},
		{"last_error", "TEXT"},
		{"edge_status_json", "TEXT"},
		{"token_prev", "TEXT"},
		{"token_prev_expires_at", "DATETIME"},
	} {
		if err := addColumnIfMissing(s.db, "shield_nodes", col.name, col.def); err != nil {
			return err
		}
	}

	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS shield_domain_status (
		node_id INTEGER NOT NULL,
		host TEXT NOT NULL,
		state TEXT NOT NULL,
		detail TEXT,
		cert_not_after DATETIME,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (node_id, host)
	);`); err != nil {
		return err
	}

	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS deleted_shield_tokens (
		token TEXT PRIMARY KEY,
		token_prev TEXT,
		token_prev_expires_at DATETIME,
		node_id INTEGER NOT NULL,
		deleted_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`); err != nil {
		return err
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

func parseDBTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func computeShieldSyncState(n *ShieldNode, now time.Time) string {
	if n.Status == "unregistered" {
		return "unregistered"
	}
	lowErr := strings.ToLower(n.LastError)
	if strings.Contains(lowErr, "auth_failed") {
		return "auth_failed"
	}
	if strings.Contains(lowErr, "panel_outdated") {
		return "outdated"
	}
	if n.LastSyncAt != nil {
		if now.Sub(*n.LastSyncAt) > 5*time.Minute {
			return "stale"
		}
		return "ok"
	}
	if n.EdgeID == "" && n.Status == "pending" {
		return "pending"
	}
	if n.LastHeartbeatAt != nil && now.Sub(*n.LastHeartbeatAt) > 10*time.Minute {
		return "stale"
	}
	if n.Status == "connected" {
		return "ok"
	}
	return "pending"
}

const shieldNodeSelectCols = `
	n.id, n.name, n.ip, n.token, n.status, n.last_heartbeat_at,
	COALESCE(n.edge_id, ''), COALESCE(n.edge_version, ''), COALESCE(n.caddy_version, ''),
	n.registered_at, n.last_sync_at, COALESCE(n.last_applied_etag, ''), COALESCE(n.last_error, ''),
	COALESCE(n.edge_status_json, ''), COALESCE(n.token_prev, ''), n.token_prev_expires_at,
	n.created_at,
	(SELECT COUNT(1) FROM app_domains WHERE shield_node_id = n.id) +
	(SELECT COUNT(1) FROM proxy_routes WHERE shield_node_id = n.id) AS assigned_domains,
	(SELECT COUNT(1) FROM shield_domain_status WHERE node_id = n.id AND state = 'conflict') AS conflicts_count
`

func scanShieldNode(sc interface{ Scan(...any) error }, now time.Time) (ShieldNode, error) {
	var n ShieldNode
	var lastHb, regAt, lastSync, prevExp sql.NullString
	var createdStr string
	err := sc.Scan(
		&n.ID, &n.Name, &n.IP, &n.Token, &n.Status, &lastHb,
		&n.EdgeID, &n.EdgeVersion, &n.CaddyVersion,
		&regAt, &lastSync, &n.LastAppliedETag, &n.LastError,
		&n.EdgeStatusJSON, &n.TokenPrev, &prevExp,
		&createdStr, &n.AssignedDomains, &n.ConflictsCount,
	)
	if err != nil {
		return n, err
	}
	if lastHb.Valid {
		if t, ok := parseDBTime(lastHb.String); ok {
			n.LastHeartbeatAt = &t
			if n.Status == "connected" && now.Sub(t) > 10*time.Minute {
				n.Status = "offline"
			}
		}
	}
	if regAt.Valid {
		if t, ok := parseDBTime(regAt.String); ok {
			n.RegisteredAt = &t
		}
	}
	if lastSync.Valid {
		if t, ok := parseDBTime(lastSync.String); ok {
			n.LastSyncAt = &t
		}
	}
	if prevExp.Valid {
		if t, ok := parseDBTime(prevExp.String); ok {
			n.TokenPrevExpiresAt = &t
		}
	}
	if t, ok := parseDBTime(createdStr); ok {
		n.CreatedAt = t
	}
	n.SyncState = computeShieldSyncState(&n, now)
	return n, nil
}

func (s *Store) ListShieldNodes(ctx context.Context) ([]ShieldNode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+shieldNodeSelectCols+` FROM shield_nodes n ORDER BY n.id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []ShieldNode
	now := time.Now().UTC()
	for rows.Next() {
		n, err := scanShieldNode(rows, now)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

func (s *Store) GetShieldNode(ctx context.Context, id int64) (*ShieldNode, error) {
	now := time.Now().UTC()
	n, err := scanShieldNode(s.db.QueryRowContext(ctx, `SELECT `+shieldNodeSelectCols+` FROM shield_nodes n WHERE n.id = ?`, id), now)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// GetShieldNodeByToken authenticates a Shield token in constant time, accepting
// token_prev until token_prev_expires_at.
func (s *Store) GetShieldNodeByToken(ctx context.Context, token string) (*ShieldNode, error) {
	if token == "" {
		return nil, sql.ErrNoRows
	}
	nodes, err := s.ListShieldNodes(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	presented := []byte(token)
	var matched *ShieldNode
	for i := range nodes {
		n := &nodes[i]
		curMatch := subtle.ConstantTimeCompare(presented, []byte(n.Token)) == 1
		prevValid := n.TokenPrev != "" && n.TokenPrevExpiresAt != nil && now.Before(*n.TokenPrevExpiresAt)
		prevMatch := prevValid && subtle.ConstantTimeCompare(presented, []byte(n.TokenPrev)) == 1
		if (curMatch || prevMatch) && matched == nil {
			copyNode := *n
			matched = &copyNode
		}
	}
	if matched == nil {
		return nil, sql.ErrNoRows
	}
	return matched, nil
}

// IsDeletedShieldToken checks whether the presented token matches a deleted Shield node tombstone.
func (s *Store) IsDeletedShieldToken(ctx context.Context, token string) (bool, error) {
	if token == "" {
		return false, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT token, COALESCE(token_prev,''), token_prev_expires_at FROM deleted_shield_tokens`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	now := time.Now().UTC()
	presented := []byte(token)
	matched := false
	for rows.Next() {
		var tok, prev string
		var prevExp sql.NullString
		if err := rows.Scan(&tok, &prev, &prevExp); err != nil {
			return false, err
		}
		if subtle.ConstantTimeCompare(presented, []byte(tok)) == 1 {
			matched = true
		}
		if prev != "" && prevExp.Valid {
			if expT, ok := parseDBTime(prevExp.String); ok && now.Before(expT) {
				if subtle.ConstantTimeCompare(presented, []byte(prev)) == 1 {
					matched = true
				}
			}
		}
	}
	return matched, rows.Err()
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
	// Record tombstone so polling Shield nodes receive 410 Gone instead of 401.
	var tok, prev string
	var prevExp sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT token, COALESCE(token_prev,''), token_prev_expires_at FROM shield_nodes WHERE id = ?`, id).Scan(&tok, &prev, &prevExp); err == nil && tok != "" {
		var expArg interface{}
		if prevExp.Valid && prevExp.String != "" {
			expArg = prevExp.String
		}
		_, _ = s.db.ExecContext(ctx, `
			INSERT INTO deleted_shield_tokens (token, token_prev, token_prev_expires_at, node_id, deleted_at)
			VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(token) DO UPDATE SET token_prev=excluded.token_prev, token_prev_expires_at=excluded.token_prev_expires_at, node_id=excluded.node_id, deleted_at=CURRENT_TIMESTAMP
		`, tok, prev, expArg, id)
	}

	// Unlink from users, domains, proxy routes, and domain status
	_, _ = s.db.ExecContext(ctx, `UPDATE users SET default_shield_id = NULL WHERE default_shield_id = ?`, id)
	_, _ = s.db.ExecContext(ctx, `UPDATE app_domains SET shield_node_id = NULL, pending_origin_apply = 0 WHERE shield_node_id = ?`, id)
	_, _ = s.db.ExecContext(ctx, `UPDATE proxy_routes SET shield_node_id = NULL, pending_origin_apply = 0 WHERE shield_node_id = ?`, id)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM shield_domain_status WHERE node_id = ?`, id)

	_, err := s.db.ExecContext(ctx, `DELETE FROM shield_nodes WHERE id = ?`, id)
	return err
}

// RotateShieldNodeToken generates a new token and preserves the old token as token_prev for the grace period.
func (s *Store) RotateShieldNodeToken(ctx context.Context, id int64, grace time.Duration) (*ShieldNode, error) {
	if grace <= 0 {
		grace = 10 * time.Minute
	}
	cur, err := s.GetShieldNode(ctx, id)
	if err != nil {
		return nil, err
	}
	newTok := GenerateShieldToken()
	expiresAt := time.Now().UTC().Add(grace).Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `
		UPDATE shield_nodes SET token = ?, token_prev = ?, token_prev_expires_at = ? WHERE id = ?
	`, newTok, cur.Token, expiresAt, id); err != nil {
		return nil, err
	}
	return s.GetShieldNode(ctx, id)
}

func (s *Store) RegisterShieldNode(ctx context.Context, id int64, edgeID, edgeVersion, caddyVersion string) error {
	nowStr := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		UPDATE shield_nodes
		SET edge_id = ?,
		    edge_version = ?,
		    caddy_version = ?,
		    registered_at = CASE WHEN COALESCE(edge_id, '') != ? OR registered_at IS NULL THEN ? ELSE registered_at END,
		    status = 'connected',
		    last_heartbeat_at = ?
		WHERE id = ?
	`, edgeID, edgeVersion, caddyVersion, edgeID, nowStr, nowStr, id)
	return err
}

func (s *Store) UnregisterShieldNode(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE shield_nodes SET status = 'unregistered', edge_id = '' WHERE id = ?
	`, id)
	return err
}

func (s *Store) UpdateShieldNodeEdgeStatus(ctx context.Context, id int64, edgeVersion, caddyVersion, appliedETag, lastError, rawStatusJSON string, statuses []ShieldDomainStatus) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	nowStr := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx, `
		UPDATE shield_nodes
		SET status = 'connected',
		    last_heartbeat_at = ?,
		    last_sync_at = ?,
		    edge_version = CASE WHEN ? != '' THEN ? ELSE edge_version END,
		    caddy_version = CASE WHEN ? != '' THEN ? ELSE caddy_version END,
		    last_applied_etag = CASE WHEN ? != '' THEN ? ELSE last_applied_etag END,
		    last_error = ?,
		    edge_status_json = ?
		WHERE id = ?
	`, nowStr, nowStr, edgeVersion, edgeVersion, caddyVersion, caddyVersion, appliedETag, appliedETag, lastError, rawStatusJSON, id); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM shield_domain_status WHERE node_id = ?`, id); err != nil {
		return err
	}
	for _, st := range statuses {
		var certStr interface{}
		if st.CertNotAfter != nil && !st.CertNotAfter.IsZero() {
			certStr = st.CertNotAfter.UTC().Format(time.RFC3339)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO shield_domain_status (node_id, host, state, detail, cert_not_after, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(node_id, host) DO UPDATE SET
				state = excluded.state,
				detail = excluded.detail,
				cert_not_after = excluded.cert_not_after,
				updated_at = excluded.updated_at
		`, id, strings.ToLower(strings.TrimSpace(st.Host)), st.State, st.Detail, certStr, nowStr); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListShieldDomainStatuses(ctx context.Context, nodeID int64) ([]ShieldDomainStatus, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT node_id, host, state, COALESCE(detail,''), cert_not_after, updated_at
		FROM shield_domain_status WHERE node_id = ? ORDER BY host ASC
	`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanShieldDomainStatusRows(rows)
}

func (s *Store) ListAllShieldDomainStatuses(ctx context.Context) ([]ShieldDomainStatus, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT node_id, host, state, COALESCE(detail,''), cert_not_after, updated_at
		FROM shield_domain_status ORDER BY node_id ASC, host ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanShieldDomainStatusRows(rows)
}

func scanShieldDomainStatusRows(rows *sql.Rows) ([]ShieldDomainStatus, error) {
	var out []ShieldDomainStatus
	for rows.Next() {
		var st ShieldDomainStatus
		var certNull, updNull sql.NullString
		if err := rows.Scan(&st.NodeID, &st.Host, &st.State, &st.Detail, &certNull, &updNull); err != nil {
			return nil, err
		}
		if certNull.Valid {
			if t, ok := parseDBTime(certNull.String); ok {
				st.CertNotAfter = &t
			}
		}
		if updNull.Valid {
			if t, ok := parseDBTime(updNull.String); ok {
				st.UpdatedAt = t
			}
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Store) UpdateShieldNodeHeartbeat(ctx context.Context, id int64) error {
	nowStr := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		UPDATE shield_nodes SET status = 'connected', last_heartbeat_at = ? WHERE id = ?
	`, nowStr, id)
	return err
}

func (s *Store) UpdateShieldNodeStatus(ctx context.Context, id int64, status string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE shield_nodes SET status = ? WHERE id = ?
	`, status, id)
	return err
}
