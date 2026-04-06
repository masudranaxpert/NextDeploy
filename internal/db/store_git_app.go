package db

import (
	"context"
	"errors"
	"strings"
	"time"
)

func (s *Store) GetAppGitConfig(ctx context.Context, appID string) (AppGitConfig, error) {
	var g AppGitConfig
	var auto int
	var created, updated string
	err := s.db.QueryRowContext(ctx, `
SELECT app_id, COALESCE(git_provider_id,0), provider, repo_url, repo_full_name, branch, auth_mode, token,
       app_git_id, installation_id, private_key_pem, webhook_secret, auto_deploy,
       last_deploy_ref, created_at, updated_at
FROM app_git_configs WHERE app_id = ?`, appID).Scan(
		&g.AppID, &g.GitProviderID, &g.Provider, &g.RepoURL, &g.RepoFullName, &g.Branch, &g.AuthMode, &g.Token,
		&g.AppGitID, &g.InstallationID, &g.PrivateKeyPEM, &g.WebhookSecret, &auto,
		&g.LastDeployRef, &created, &updated,
	)
	if err != nil {
		return AppGitConfig{}, err
	}
	g.AutoDeploy = auto != 0
	g.CreatedAt, _ = time.Parse(time.RFC3339, created)
	g.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return g, nil
}

func (s *Store) UpsertAppGitConfig(ctx context.Context, g AppGitConfig) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(g.Provider) == "" {
		g.Provider = "github"
	}
	if strings.TrimSpace(g.Branch) == "" {
		g.Branch = "main"
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO app_git_configs (
  app_id, git_provider_id, provider, repo_url, repo_full_name, branch, auth_mode, token,
  app_git_id, installation_id, private_key_pem, webhook_secret, auto_deploy,
  last_deploy_ref, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(app_id) DO UPDATE SET
  git_provider_id=excluded.git_provider_id,
  provider=excluded.provider,
  repo_url=excluded.repo_url,
  repo_full_name=excluded.repo_full_name,
  branch=excluded.branch,
  auth_mode=excluded.auth_mode,
  token=excluded.token,
  app_git_id=excluded.app_git_id,
  installation_id=excluded.installation_id,
  private_key_pem=excluded.private_key_pem,
  webhook_secret=excluded.webhook_secret,
  auto_deploy=excluded.auto_deploy,
  last_deploy_ref=excluded.last_deploy_ref,
  updated_at=excluded.updated_at`,
		g.AppID, g.GitProviderID, g.Provider, g.RepoURL, g.RepoFullName, g.Branch, g.AuthMode, g.Token,
		g.AppGitID, g.InstallationID, g.PrivateKeyPEM, g.WebhookSecret, boolInt(g.AutoDeploy),
		g.LastDeployRef, now, now,
	)
	return err
}

func (s *Store) DeleteAppGitConfig(ctx context.Context, appID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM app_git_configs WHERE app_id = ?`, appID)
	return err
}

func (s *Store) ListGitApps(ctx context.Context) ([]AppGitConfig, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT app_id, COALESCE(git_provider_id,0), provider, repo_url, repo_full_name, branch, auth_mode, token,
       app_git_id, installation_id, private_key_pem, webhook_secret, auto_deploy,
       last_deploy_ref, created_at, updated_at
FROM app_git_configs ORDER BY datetime(updated_at) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AppGitConfig
	for rows.Next() {
		var g AppGitConfig
		var auto int
		var created, updated string
		if err := rows.Scan(
			&g.AppID, &g.GitProviderID, &g.Provider, &g.RepoURL, &g.RepoFullName, &g.Branch, &g.AuthMode, &g.Token,
			&g.AppGitID, &g.InstallationID, &g.PrivateKeyPEM, &g.WebhookSecret, &auto,
			&g.LastDeployRef, &created, &updated,
		); err != nil {
			return nil, err
		}
		g.AutoDeploy = auto != 0
		g.CreatedAt, _ = time.Parse(time.RFC3339, created)
		g.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) MarkWebhookDelivery(ctx context.Context, appID, deliveryID string) (bool, error) {
	if strings.TrimSpace(appID) == "" || strings.TrimSpace(deliveryID) == "" {
		return false, errors.New("invalid webhook delivery")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO webhook_deliveries(app_id, delivery_id, created_at) VALUES (?, ?, ?)`,
		appID, deliveryID, time.Now().UTC().Format(time.RFC3339))
	if err == nil {
		return true, nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "unique") {
		return false, nil
	}
	return false, err
}
