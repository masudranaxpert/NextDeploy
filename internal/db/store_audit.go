package db

import (
	"context"
	"strings"
	"time"
)

func (s *Store) CreateAuditLog(ctx context.Context, log AuditLog) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_logs (user_id, username, action, target_type, target_id, ip_address, user_agent, details, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.UserID, log.Username, log.Action, log.TargetType, log.TargetID, log.IPAddress, log.UserAgent, log.Details, log.CreatedAt.UTC().Format(time.RFC3339),
	)
	return err
}

const auditSelect = `SELECT a.id, a.user_id, a.username, COALESCE(u.role,''), a.action, a.target_type, a.target_id, a.ip_address, a.user_agent, a.details, a.created_at,
		COALESCE(o.username,''), COALESCE(o.role,'')
	FROM audit_logs a
	LEFT JOIN users u ON u.id = a.user_id
	LEFT JOIN apps ap ON a.target_type = 'app' AND ap.id = a.target_id
	LEFT JOIN users o ON o.id = ap.owner_id`

func scanAuditRows(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]AuditLog, error) {
	var logs []AuditLog
	for rows.Next() {
		var l AuditLog
		var created string
		if err := rows.Scan(&l.ID, &l.UserID, &l.Username, &l.ActorRole, &l.Action, &l.TargetType, &l.TargetID, &l.IPAddress, &l.UserAgent, &l.Details, &created, &l.TargetOwner, &l.TargetOwnerRole); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339, created); err == nil {
			l.CreatedAt = t.Local()
		}
		if l.ActorRole == "" && (l.UserID == 0 || l.Username == "system") {
			l.ActorRole = "system"
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

func (s *Store) ListAuditLogs(ctx context.Context, limit, offset int) ([]AuditLog, error) {
	rows, err := s.db.QueryContext(ctx, auditSelect+` ORDER BY a.created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAuditRows(rows)
}

func (s *Store) CountAuditLogs(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_logs").Scan(&count)
	return count, err
}

func (s *Store) PruneAuditLogs(ctx context.Context, keepDays int) error {
	cutoff := time.Now().UTC().AddDate(0, 0, -keepDays).Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, "DELETE FROM audit_logs WHERE created_at < ?", cutoff)
	return err
}

func inPlaceholders(n int) string {
	if n == 0 {
		return "''"
	}
	return strings.TrimRight(strings.Repeat("?,", n), ",")
}

func auditScope(userID int64, appIDs []string) (string, []interface{}) {
	args := []interface{}{userID}
	for _, id := range appIDs {
		args = append(args, id)
	}
	return "(a.user_id = ? OR (a.target_type = 'app' AND a.target_id IN (" + inPlaceholders(len(appIDs)) + ")))", args
}

// ListAuditLogsForUser returns what a non-admin may see: their own actions plus anything done
// to their apps (for example by an administrator).
func (s *Store) ListAuditLogsForUser(ctx context.Context, userID int64, appIDs []string, limit, offset int) ([]AuditLog, error) {
	where, args := auditScope(userID, appIDs)
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, auditSelect+` WHERE `+where+` ORDER BY a.created_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAuditRows(rows)
}

func (s *Store) CountAuditLogsForUser(ctx context.Context, userID int64, appIDs []string) (int, error) {
	where, args := auditScope(userID, appIDs)
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_logs a WHERE "+where, args...).Scan(&n)
	return n, err
}
