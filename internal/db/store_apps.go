package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

func (s *Store) CreateApp(ctx context.Context, id, name string, ownerID int64) error {
	if id == "" || name == "" {
		return errors.New("invalid app")
	}
	exists, err := s.AppNameExistsForUser(ctx, name, ownerID)
	if err != nil {
		return err
	}
	if exists {
		return errors.New("app name already exists")
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO apps (id, name, created_at, compose_file, owner_id, status) VALUES (?, ?, ?, ?, ?, ?)`,
		id, name, time.Now().UTC().Format(time.RFC3339), "docker-compose.yml", ownerID, "active")
	return err
}

func (s *Store) AppNameExistsForUser(ctx context.Context, name string, ownerID int64) (bool, error) {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return false, nil
	}
	var found string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM apps WHERE lower(name) = ? AND owner_id = ? LIMIT 1`, name, ownerID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return found != "", nil
}

func (s *Store) AppNameExists(ctx context.Context, name string) (bool, error) {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return false, nil
	}
	var found string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM apps WHERE lower(name) = ? LIMIT 1`, name).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return found != "", nil
}

func (s *Store) ListApps(ctx context.Context) ([]App, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, created_at, COALESCE(compose_file,''), COALESCE(owner_id, 0), COALESCE(status, 'active') FROM apps ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []App
	for rows.Next() {
		var a App
		var created string
		if err := rows.Scan(&a.ID, &a.Name, &created, &a.ComposeFile, &a.OwnerID, &a.Status); err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339, created)
		if err != nil {
			t = time.Time{}
		}
		a.CreatedAt = t
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) ListAppsForUser(ctx context.Context, userID int64) ([]App, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, created_at, COALESCE(compose_file,''), COALESCE(owner_id, 0), COALESCE(status, 'active')
		FROM apps
		WHERE owner_id = ? OR id IN (SELECT app_id FROM app_collaborators WHERE user_id = ?)
		ORDER BY created_at DESC`, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []App
	for rows.Next() {
		var a App
		var created string
		if err := rows.Scan(&a.ID, &a.Name, &created, &a.ComposeFile, &a.OwnerID, &a.Status); err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339, created)
		if err != nil {
			t = time.Time{}
		}
		a.CreatedAt = t
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) GetApp(ctx context.Context, id string) (App, error) {
	var a App
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT id, name, created_at, COALESCE(compose_file,''), COALESCE(owner_id, 0), COALESCE(status, 'active') FROM apps WHERE id = ?`, id).Scan(&a.ID, &a.Name, &created, &a.ComposeFile, &a.OwnerID, &a.Status)
	if err != nil {
		return App{}, err
	}
	t, _ := time.Parse(time.RFC3339, created)
	a.CreatedAt = t
	if strings.TrimSpace(a.ComposeFile) == "" {
		a.ComposeFile = "docker-compose.yml"
	}
	return a, nil
}

func (s *Store) UpdateAppStatus(ctx context.Context, id string, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE apps SET status = ? WHERE id = ?`, status, id)
	return err
}

func (s *Store) UpdateAppOwner(ctx context.Context, id string, ownerID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE apps SET owner_id = ? WHERE id = ?`, ownerID, id)
	return err
}


func (s *Store) UpdateComposeFile(ctx context.Context, id, composeFile string) error {
	composeFile = strings.TrimSpace(composeFile)
	if composeFile == "" {
		composeFile = "docker-compose.yml"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE apps SET compose_file = ? WHERE id = ?`, composeFile, id)
	return err
}

// GetPanelEnv returns environment variables managed in the panel UI (dotenv text), not the workspace .env file.
func (s *Store) GetPanelEnv(ctx context.Context, appID string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(panel_env,'') FROM apps WHERE id = ?`, appID).Scan(&v)
	if err != nil {
		return "", err
	}
	return v, nil
}

// UpdatePanelEnv persists panel-managed compose environment (dotenv format).
func (s *Store) UpdatePanelEnv(ctx context.Context, appID, content string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE apps SET panel_env = ? WHERE id = ?`, content, appID)
	return err
}

// AppComposeHint carries batched DB fields for docker compose paths and env (apps list optimization).
type AppComposeHint struct {
	SourceType string
	RepoURL    string
	PanelEnv   string
}

// BatchAppComposeHints loads source_type, panel_env, and git repo_url for many apps in one query.
func (s *Store) BatchAppComposeHints(ctx context.Context, ids []string) (map[string]AppComposeHint, error) {
	out := make(map[string]AppComposeHint, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	ph := strings.Repeat("?,", len(ids))
	ph = strings.TrimSuffix(ph, ",")
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	q := `SELECT a.id, COALESCE(a.source_type,''), COALESCE(a.panel_env,''), COALESCE(g.repo_url,'') ` +
		`FROM apps a LEFT JOIN app_git_configs g ON g.app_id = a.id WHERE a.id IN (` + ph + `)`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, st, env, repo string
		if err := rows.Scan(&id, &st, &env, &repo); err != nil {
			return nil, err
		}
		if st == "" {
			st = "files"
		}
		out[id] = AppComposeHint{SourceType: st, RepoURL: repo, PanelEnv: env}
	}
	return out, rows.Err()
}

func (s *Store) DeleteApp(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("invalid id")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// deploy_logs has no FK to apps; other app_id tables use ON DELETE CASCADE.
	if _, err := tx.ExecContext(ctx, `DELETE FROM deploy_logs WHERE app_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM apps WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}
