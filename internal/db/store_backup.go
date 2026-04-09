package db

import (
	"context"
	"database/sql"
)

func (s *Store) CreateBackupDestination(ctx context.Context, name, provider, config string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO backup_destinations (name, provider, config, created_at) VALUES (?, ?, ?, datetime('now'))`,
		name, provider, config)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListBackupDestinations(ctx context.Context) ([]BackupDestination, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, provider, config, created_at FROM backup_destinations ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dests []BackupDestination
	for rows.Next() {
		var d BackupDestination
		if err := rows.Scan(&d.ID, &d.Name, &d.Provider, &d.Config, &d.CreatedAt); err != nil {
			return nil, err
		}
		dests = append(dests, d)
	}
	return dests, rows.Err()
}

func (s *Store) GetBackupDestination(ctx context.Context, id int64) (BackupDestination, error) {
	var d BackupDestination
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, provider, config, created_at FROM backup_destinations WHERE id = ?`, id).
		Scan(&d.ID, &d.Name, &d.Provider, &d.Config, &d.CreatedAt)
	if err != nil {
		return BackupDestination{}, err
	}
	return d, nil
}

func (s *Store) DeleteBackupDestination(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM backup_destinations WHERE id = ?`, id)
	return err
}

func (s *Store) CreateBackupSchedule(ctx context.Context, appID string, destID int64, backupType, cronExpr string, retention int) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO backup_schedules (app_id, destination_id, backup_type, cron_expression, retention_count, enabled, created_at) 
		VALUES (?, ?, ?, ?, ?, 1, datetime('now'))`,
		appID, destID, backupType, cronExpr, retention)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListBackupSchedules(ctx context.Context, appID string) ([]BackupSchedule, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, app_id, destination_id, backup_type, cron_expression, retention_count, enabled, last_run, created_at 
		FROM backup_schedules WHERE app_id = ? ORDER BY created_at DESC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schedules []BackupSchedule
	for rows.Next() {
		var s BackupSchedule
		var lastRun sql.NullString
		if err := rows.Scan(&s.ID, &s.AppID, &s.DestinationID, &s.BackupType, &s.CronExpression, &s.RetentionCount, &s.Enabled, &lastRun, &s.CreatedAt); err != nil {
			return nil, err
		}
		s.LastRun = lastRun.String
		schedules = append(schedules, s)
	}
	return schedules, rows.Err()
}

func (s *Store) UpdateBackupScheduleEnabled(ctx context.Context, id int64, enabled bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE backup_schedules SET enabled = ? WHERE id = ?`, enabled, id)
	return err
}

func (s *Store) UpdateBackupScheduleLastRun(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE backup_schedules SET last_run = datetime('now') WHERE id = ?`, id)
	return err
}

func (s *Store) DeleteBackupSchedule(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM backup_schedules WHERE id = ?`, id)
	return err
}

func (s *Store) CreateBackupHistory(ctx context.Context, appID string, destID int64, backupType, remotePath, status, errorMsg string, size int64) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO backup_history (app_id, destination_id, backup_type, remote_path, status, error_message, size_bytes, created_at) 
		VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		appID, destID, backupType, remotePath, status, errorMsg, size)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListBackupHistory(ctx context.Context, appID string, limit int) ([]BackupHistory, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, app_id, destination_id, backup_type, remote_path, status, error_message, size_bytes, created_at 
		FROM backup_history WHERE app_id = ? ORDER BY created_at DESC LIMIT ?`, appID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []BackupHistory
	for rows.Next() {
		var h BackupHistory
		var errMsg sql.NullString
		if err := rows.Scan(&h.ID, &h.AppID, &h.DestinationID, &h.BackupType, &h.RemotePath, &h.Status, &errMsg, &h.SizeBytes, &h.CreatedAt); err != nil {
			return nil, err
		}
		h.ErrorMessage = errMsg.String
		history = append(history, h)
	}
	return history, rows.Err()
}

func (s *Store) UpdateBackupHistoryStatus(ctx context.Context, id int64, status, errorMsg string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE backup_history SET status = ?, error_message = ? WHERE id = ?`, status, errorMsg, id)
	return err
}

func (s *Store) DeleteOldBackups(ctx context.Context, appID string, destID int64, backupType string, keepCount int) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM backup_history 
		WHERE id IN (
			SELECT id FROM backup_history 
			WHERE app_id = ? AND destination_id = ? AND backup_type = ? AND status = 'completed'
			ORDER BY created_at DESC 
			LIMIT -1 OFFSET ?
		)`, appID, destID, backupType, keepCount)
	return err
}

func (s *Store) GetBackupHistory(ctx context.Context, id int64) (BackupHistory, error) {
	var h BackupHistory
	var errMsg sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, app_id, destination_id, backup_type, remote_path, status, error_message, size_bytes, created_at 
		FROM backup_history WHERE id = ?`, id).
		Scan(&h.ID, &h.AppID, &h.DestinationID, &h.BackupType, &h.RemotePath, &h.Status, &errMsg, &h.SizeBytes, &h.CreatedAt)
	if err != nil {
		return BackupHistory{}, err
	}
	h.ErrorMessage = errMsg.String
	return h, nil
}
