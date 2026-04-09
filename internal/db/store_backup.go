package db

import (
	"context"
	"database/sql"
)

func (s *Store) CreateBackupDestination(ctx context.Context, name, provider, config string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO backup_destinations (name, provider, config, created_at, updated_at) VALUES (?, ?, ?, datetime('now'), datetime('now'))`,
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

func (s *Store) UpdateBackupDestinationConfig(ctx context.Context, id int64, config string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE backup_destinations SET config = ?, updated_at = datetime('now') WHERE id = ?`,
		config, id)
	return err
}

func (s *Store) CreateBackupSchedule(ctx context.Context, appID string, destID int64, backupType, volumeNames, cronExpr string, retention int) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO backup_schedules (app_id, destination_id, backup_type, volume_names, cron_schedule, retention_count, enabled, created_at, updated_at) 
		VALUES (?, ?, ?, ?, ?, ?, 1, datetime('now'), datetime('now'))`,
		appID, destID, backupType, volumeNames, cronExpr, retention)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListBackupSchedules(ctx context.Context, appID string) ([]BackupSchedule, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, app_id, destination_id, backup_type, volume_names, cron_schedule, retention_count, enabled, last_run_at, created_at 
		FROM backup_schedules WHERE app_id = ? ORDER BY created_at DESC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schedules []BackupSchedule
	for rows.Next() {
		var s BackupSchedule
		var lastRun sql.NullString
		if err := rows.Scan(&s.ID, &s.AppID, &s.DestinationID, &s.BackupType, &s.VolumeNames, &s.CronExpression, &s.RetentionCount, &s.Enabled, &lastRun, &s.CreatedAt); err != nil {
			return nil, err
		}
		s.LastRun = lastRun.String
		schedules = append(schedules, s)
	}
	return schedules, rows.Err()
}

func (s *Store) UpdateBackupSchedule(ctx context.Context, scheduleID int64, appID string, destID int64, backupType, volumeNames, cronExpr string, retention int) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE backup_schedules SET destination_id = ?, backup_type = ?, volume_names = ?, cron_schedule = ?, retention_count = ?, updated_at = datetime('now') WHERE id = ? AND app_id = ?`,
		destID, backupType, volumeNames, cronExpr, retention, scheduleID, appID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) UpdateBackupScheduleEnabled(ctx context.Context, id int64, enabled bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE backup_schedules SET enabled = ? WHERE id = ?`, enabled, id)
	return err
}

func (s *Store) UpdateBackupScheduleEnabledForApp(ctx context.Context, id int64, appID string, enabled bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE backup_schedules SET enabled = ? WHERE id = ? AND app_id = ?`, enabled, id, appID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) UpdateBackupScheduleLastRun(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE backup_schedules SET last_run_at = datetime('now') WHERE id = ?`, id)
	return err
}

func (s *Store) DeleteBackupSchedule(ctx context.Context, appID string, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM backup_schedules WHERE id = ? AND app_id = ?`, id, appID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) CreateBackupHistory(ctx context.Context, appID string, destID int64, backupType, remotePath, status, errorMsg string, size int64) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO backup_history (app_id, destination_id, backup_type, volume_name, remote_path, status, error_message, size_bytes, created_at) 
		VALUES (?, ?, ?, '', ?, ?, ?, ?, datetime('now'))`,
		appID, destID, backupType, remotePath, status, errorMsg, size)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) CountBackupHistory(ctx context.Context, appID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM backup_history WHERE app_id = ?`, appID).Scan(&n)
	return n, err
}

// ListBackupHistory returns the newest rows up to limit (no offset). Prefer ListBackupHistoryPage for paged APIs.
func (s *Store) ListBackupHistory(ctx context.Context, appID string, limit int) ([]BackupHistory, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.ListBackupHistoryPage(ctx, appID, limit, 0)
}

func (s *Store) ListBackupHistoryPage(ctx context.Context, appID string, limit, offset int) ([]BackupHistory, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, app_id, destination_id, backup_type, volume_name, remote_path, status, error_message, size_bytes, log, created_at, remote_missing, remote_checked_at 
		FROM backup_history WHERE app_id = ? ORDER BY created_at DESC LIMIT ? OFFSET ?`, appID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []BackupHistory
	for rows.Next() {
		var h BackupHistory
		var errMsg, log sql.NullString
		if err := rows.Scan(&h.ID, &h.AppID, &h.DestinationID, &h.BackupType, &h.VolumeName, &h.RemotePath, &h.Status, &errMsg, &h.SizeBytes, &log, &h.CreatedAt, &h.RemoteMissingCode, &h.RemoteCheckedAt); err != nil {
			return nil, err
		}
		h.ErrorMessage = errMsg.String
		h.Log = log.String
		history = append(history, h)
	}
	return history, rows.Err()
}

func (s *Store) UpdateBackupHistoryStatus(ctx context.Context, id int64, status, errorMsg string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE backup_history SET status = ?, error_message = ? WHERE id = ?`, status, errorMsg, id)
	return err
}

func (s *Store) UpdateBackupHistoryStatusWithLog(ctx context.Context, id int64, status, errorMsg, log string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE backup_history SET status = ?, error_message = ?, log = ? WHERE id = ?`, status, errorMsg, log, id)
	return err
}

func (s *Store) UpdateBackupHistoryCompleted(ctx context.Context, id int64, volumeName, remotePath string, size int64, log string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE backup_history SET status = 'completed', error_message = '', log = ?, volume_name = ?, remote_path = ?, size_bytes = ?, remote_missing = 0, remote_checked_at = datetime('now') WHERE id = ?`,
		log, volumeName, remotePath, size, id)
	return err
}

// UpdateBackupHistoryRemoteCheck stores rclone remote existence result (paired with remote_checked_at).
func (s *Store) UpdateBackupHistoryRemoteCheck(ctx context.Context, id int64, missing bool) error {
	v := 0
	if missing {
		v = 1
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE backup_history SET remote_missing = ?, remote_checked_at = datetime('now') WHERE id = ?`,
		v, id)
	return err
}

func (s *Store) DeleteOldBackups(ctx context.Context, appID string, destID int64, backupType string, keepCount int) error {
	if keepCount < 1 {
		return nil
	}
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

func (s *Store) ListOldBackupsForPrune(ctx context.Context, appID string, destID int64, backupType string, keepCount int) ([]BackupHistory, error) {
	if keepCount < 1 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, app_id, destination_id, backup_type, volume_name, remote_path, status, error_message, size_bytes, log, created_at, remote_missing, remote_checked_at
		FROM backup_history
		WHERE id IN (
			SELECT id FROM backup_history
			WHERE app_id = ? AND destination_id = ? AND backup_type = ? AND status = 'completed'
			ORDER BY created_at DESC
			LIMIT -1 OFFSET ?
		)
		ORDER BY created_at ASC`, appID, destID, backupType, keepCount)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BackupHistory
	for rows.Next() {
		var h BackupHistory
		var errMsg, log sql.NullString
		if err := rows.Scan(&h.ID, &h.AppID, &h.DestinationID, &h.BackupType, &h.VolumeName, &h.RemotePath, &h.Status, &errMsg, &h.SizeBytes, &log, &h.CreatedAt, &h.RemoteMissingCode, &h.RemoteCheckedAt); err != nil {
			return nil, err
		}
		h.ErrorMessage = errMsg.String
		h.Log = log.String
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *Store) DeleteBackupHistoryByID(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM backup_history WHERE id = ?`, id)
	return err
}

func (s *Store) GetBackupHistory(ctx context.Context, id int64) (BackupHistory, error) {
	var h BackupHistory
	var errMsg, log sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, app_id, destination_id, backup_type, volume_name, remote_path, status, error_message, size_bytes, log, created_at, remote_missing, remote_checked_at 
		FROM backup_history WHERE id = ?`, id).
		Scan(&h.ID, &h.AppID, &h.DestinationID, &h.BackupType, &h.VolumeName, &h.RemotePath, &h.Status, &errMsg, &h.SizeBytes, &log, &h.CreatedAt, &h.RemoteMissingCode, &h.RemoteCheckedAt)
	if err != nil {
		return BackupHistory{}, err
	}
	h.ErrorMessage = errMsg.String
	h.Log = log.String
	return h, nil
}
