package db

import (
	"database/sql"
	"time"
)

// UpsertCLISession inserts or updates a CLI session heartbeat.
func (s *Store) UpsertCLISession(sess CLISession) error {
	_, err := s.db.Exec(`
INSERT INTO cli_sessions (id, user_id, hostname, os, arch, version, token_name, token_prefix, last_seen, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  hostname     = excluded.hostname,
  os           = excluded.os,
  arch         = excluded.arch,
  version      = excluded.version,
  token_name   = CASE WHEN excluded.token_name != '' THEN excluded.token_name ELSE cli_sessions.token_name END,
  token_prefix = CASE WHEN excluded.token_prefix != '' THEN excluded.token_prefix ELSE cli_sessions.token_prefix END,
  last_seen    = excluded.last_seen`,
		sess.ID, sess.UserID, sess.Hostname, sess.OS, sess.Arch, sess.Version,
		sess.TokenName, sess.TokenPrefix,
		sess.LastSeen.UTC().Format("2006-01-02 15:04:05"),
		sess.CreatedAt.UTC().Format("2006-01-02 15:04:05"),
	)
	return err
}

// ListCLISessions returns all sessions active within the last 7 days.
func (s *Store) ListCLISessions() ([]CLISession, error) {
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	rows, err := s.db.Query(`
SELECT id, user_id, hostname, os, arch, version, COALESCE(token_name, ''), COALESCE(token_prefix, ''), last_seen, created_at
FROM cli_sessions
WHERE last_seen >= ?
ORDER BY last_seen DESC`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CLISession
	for rows.Next() {
		var sess CLISession
		var lastSeen, createdAt string
		if err := rows.Scan(&sess.ID, &sess.UserID, &sess.Hostname, &sess.OS, &sess.Arch, &sess.Version, &sess.TokenName, &sess.TokenPrefix, &lastSeen, &createdAt); err != nil {
			return nil, err
		}
		sess.LastSeen, _ = time.ParseInLocation("2006-01-02 15:04:05", lastSeen, time.UTC)
		sess.CreatedAt, _ = time.ParseInLocation("2006-01-02 15:04:05", createdAt, time.UTC)
		out = append(out, sess)
	}
	return out, rows.Err()
}

// DeleteCLISession removes a session by ID.
func (s *Store) DeleteCLISession(id string) error {
	_, err := s.db.Exec(`DELETE FROM cli_sessions WHERE id = ?`, id)
	return err
}

// PruneStaleCLISessions removes sessions not seen for more than 30 days.
func (s *Store) PruneStaleCLISessions() error {
	cutoff := time.Now().UTC().Add(-30 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	_, err := s.db.Exec(`DELETE FROM cli_sessions WHERE last_seen < ?`, cutoff)
	return err
}

// GetCLISessionUser returns the user_id for a session; sql.ErrNoRows if not found.
func (s *Store) GetCLISessionUser(sessionID string) (int64, error) {
	var uid int64
	err := s.db.QueryRow(`SELECT user_id FROM cli_sessions WHERE id = ?`, sessionID).Scan(&uid)
	if err == sql.ErrNoRows {
		return 0, sql.ErrNoRows
	}
	return uid, err
}
