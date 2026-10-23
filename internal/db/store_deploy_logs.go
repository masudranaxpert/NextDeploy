package db

import (
	"context"
	"errors"
	"time"
)

func (s *Store) InsertDeployLog(ctx context.Context, appID, action string, ok bool, output string) error {
	return s.InsertDeployLogWithSnapshot(ctx, appID, action, ok, output, "", "", "", "")
}

// InsertDeployLogWithSnapshot records a deploy run along with slot, commit, image digest, and env snapshot for instant rollback.
func (s *Store) InsertDeployLogWithSnapshot(ctx context.Context, appID, action string, ok bool, output, slot, commitSHA, imageDigest, envSnapshot string) error {
	if appID == "" || action == "" {
		return errors.New("invalid deploy log")
	}
	const maxOut = 65536
	if len(output) > maxOut {
		output = output[:maxOut] + "\n… (truncated)"
	}
	okInt := 0
	if ok {
		okInt = 1
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO deploy_logs (app_id, action, ok, output, created_at, slot, commit_sha, image_digest, env_snapshot) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		appID, action, okInt, output, time.Now().UTC().Format(time.RFC3339), slot, commitSHA, imageDigest, envSnapshot)
	if err != nil {
		return err
	}
	return s.trimDeployLogs(ctx, appID, 10)
}

func (s *Store) trimDeployLogs(ctx context.Context, appID string, keep int) error {
	_, err := s.db.ExecContext(ctx, `
DELETE FROM deploy_logs WHERE app_id = ? AND id NOT IN (
  SELECT id FROM (
    SELECT id FROM deploy_logs WHERE app_id = ? ORDER BY created_at DESC, id DESC LIMIT ?
  )
)`, appID, appID, keep)
	return err
}

func (s *Store) ListDeployLogs(ctx context.Context, appID string, limit int) ([]DeployLog, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, action, ok, output, created_at, COALESCE(slot,''), COALESCE(commit_sha,''), COALESCE(image_digest,''), COALESCE(env_snapshot,'') FROM deploy_logs WHERE app_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`,
		appID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeployLog
	for rows.Next() {
		var d DeployLog
		var ok int
		var created string
		if err := rows.Scan(&d.ID, &d.Action, &ok, &d.Output, &created, &d.Slot, &d.CommitSHA, &d.ImageDigest, &d.EnvSnapshot); err != nil {
			return nil, err
		}
		d.OK = ok != 0
		t, _ := time.Parse(time.RFC3339, created)
		d.CreatedAt = t
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetDeployLog returns one deploy log row for an app, or sql.ErrNoRows.
func (s *Store) GetDeployLog(ctx context.Context, appID string, logID int64) (DeployLog, error) {
	var d DeployLog
	var ok int
	var created string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, action, ok, output, created_at, COALESCE(slot,''), COALESCE(commit_sha,''), COALESCE(image_digest,''), COALESCE(env_snapshot,'') FROM deploy_logs WHERE id = ? AND app_id = ?`,
		logID, appID).Scan(&d.ID, &d.Action, &ok, &d.Output, &created, &d.Slot, &d.CommitSHA, &d.ImageDigest, &d.EnvSnapshot)
	if err != nil {
		return DeployLog{}, err
	}
	d.OK = ok != 0
	t, _ := time.Parse(time.RFC3339, created)
	d.CreatedAt = t
	return d, nil
}

// DeleteDeployLog removes one deploy log row if it belongs to the app. Returns whether a row was deleted.
func (s *Store) DeleteDeployLog(ctx context.Context, appID string, logID int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM deploy_logs WHERE id = ? AND app_id = ?`, logID, appID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (s *Store) ClearDeployLogs(ctx context.Context, appID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM deploy_logs WHERE app_id = ?`, appID)
	return err
}
