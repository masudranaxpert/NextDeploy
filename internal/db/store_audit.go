package db

import (
	"context"
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

func (s *Store) ListAuditLogs(ctx context.Context) ([]AuditLog, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, username, action, target_type, target_id, ip_address, user_agent, details, created_at
		 FROM audit_logs
		 ORDER BY created_at DESC LIMIT 500`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []AuditLog
	for rows.Next() {
		var log AuditLog
		var createdAtStr string
		err := rows.Scan(
			&log.ID, &log.UserID, &log.Username, &log.Action, &log.TargetType, &log.TargetID,
			&log.IPAddress, &log.UserAgent, &log.Details, &createdAtStr,
		)
		if err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			log.CreatedAt = t.Local()
		}
		logs = append(logs, log)
	}
	return logs, nil
}

func (s *Store) PruneAuditLogs(ctx context.Context, days int) error {
	cutoff := time.Now().AddDate(0, 0, -days).UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, "DELETE FROM audit_logs WHERE created_at < ?", cutoff)
	return err
}
