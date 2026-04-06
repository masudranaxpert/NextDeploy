package db

import (
	"context"
	"time"
)

func (s *Store) ListGitProviders(ctx context.Context) ([]GitProvider, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,provider,token,notes,created_at,updated_at FROM git_providers ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GitProvider
	for rows.Next() {
		var p GitProvider
		var ca, ua string
		if err := rows.Scan(&p.ID, &p.Name, &p.Provider, &p.Token, &p.Notes, &ca, &ua); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339, ca)
		p.UpdatedAt, _ = time.Parse(time.RFC3339, ua)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetGitProvider(ctx context.Context, id int64) (GitProvider, error) {
	var p GitProvider
	var ca, ua string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,provider,token,notes,created_at,updated_at FROM git_providers WHERE id=?`, id).
		Scan(&p.ID, &p.Name, &p.Provider, &p.Token, &p.Notes, &ca, &ua)
	if err != nil {
		return GitProvider{}, err
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, ca)
	p.UpdatedAt, _ = time.Parse(time.RFC3339, ua)
	return p, nil
}

func (s *Store) CreateGitProvider(ctx context.Context, name, provider, token, notes string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO git_providers(name,provider,token,notes,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
		name, provider, token, notes, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateGitProvider(ctx context.Context, id int64, name, provider, token, notes string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE git_providers SET name=?,provider=?,token=?,notes=?,updated_at=? WHERE id=?`,
		name, provider, token, notes, now, id)
	return err
}

func (s *Store) DeleteGitProvider(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM git_providers WHERE id=?`, id)
	return err
}

func (s *Store) GetGitHubProviderDetail(ctx context.Context, providerID int64) (GitHubProviderDetail, error) {
	var d GitHubProviderDetail
	var created, updated string
	var createdVia int
	err := s.db.QueryRowContext(ctx, `
SELECT provider_id, github_app_id, client_id, client_secret, private_key_pem, webhook_secret,
       installation_id, account_login, app_slug, manifest_state, created_via_manifest, created_at, updated_at
FROM github_provider_details WHERE provider_id=?`, providerID).Scan(
		&d.ProviderID, &d.GitHubAppID, &d.ClientID, &d.ClientSecret, &d.PrivateKeyPEM, &d.WebhookSecret,
		&d.InstallationID, &d.AccountLogin, &d.AppSlug, &d.ManifestState, &createdVia, &created, &updated,
	)
	if err != nil {
		return GitHubProviderDetail{}, err
	}
	d.CreatedViaManifest = createdVia != 0
	d.CreatedAt, _ = time.Parse(time.RFC3339, created)
	d.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return d, nil
}

func (s *Store) GetGitHubProviderDetailByManifestState(ctx context.Context, state string) (GitHubProviderDetail, error) {
	var d GitHubProviderDetail
	var created, updated string
	var createdVia int
	err := s.db.QueryRowContext(ctx, `
SELECT provider_id, github_app_id, client_id, client_secret, private_key_pem, webhook_secret,
       installation_id, account_login, app_slug, manifest_state, created_via_manifest, created_at, updated_at
FROM github_provider_details WHERE manifest_state=?`, state).Scan(
		&d.ProviderID, &d.GitHubAppID, &d.ClientID, &d.ClientSecret, &d.PrivateKeyPEM, &d.WebhookSecret,
		&d.InstallationID, &d.AccountLogin, &d.AppSlug, &d.ManifestState, &createdVia, &created, &updated,
	)
	if err != nil {
		return GitHubProviderDetail{}, err
	}
	d.CreatedViaManifest = createdVia != 0
	d.CreatedAt, _ = time.Parse(time.RFC3339, created)
	d.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return d, nil
}

func (s *Store) UpsertGitHubProviderDetail(ctx context.Context, d GitHubProviderDetail) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO github_provider_details (
  provider_id, github_app_id, client_id, client_secret, private_key_pem, webhook_secret,
  installation_id, account_login, app_slug, manifest_state, created_via_manifest, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(provider_id) DO UPDATE SET
  github_app_id=excluded.github_app_id,
  client_id=excluded.client_id,
  client_secret=excluded.client_secret,
  private_key_pem=excluded.private_key_pem,
  webhook_secret=excluded.webhook_secret,
  installation_id=excluded.installation_id,
  account_login=excluded.account_login,
  app_slug=excluded.app_slug,
  manifest_state=excluded.manifest_state,
  created_via_manifest=excluded.created_via_manifest,
  updated_at=excluded.updated_at`,
		d.ProviderID, d.GitHubAppID, d.ClientID, d.ClientSecret, d.PrivateKeyPEM, d.WebhookSecret,
		d.InstallationID, d.AccountLogin, d.AppSlug, d.ManifestState, boolInt(d.CreatedViaManifest), now, now,
	)
	return err
}

func (s *Store) ListGitHubProviderDetails(ctx context.Context) ([]GitHubProviderDetail, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT provider_id, github_app_id, client_id, client_secret, private_key_pem, webhook_secret,
       installation_id, account_login, app_slug, manifest_state, created_via_manifest, created_at, updated_at
FROM github_provider_details ORDER BY provider_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GitHubProviderDetail
	for rows.Next() {
		var d GitHubProviderDetail
		var created, updated string
		var createdVia int
		if err := rows.Scan(
			&d.ProviderID, &d.GitHubAppID, &d.ClientID, &d.ClientSecret, &d.PrivateKeyPEM, &d.WebhookSecret,
			&d.InstallationID, &d.AccountLogin, &d.AppSlug, &d.ManifestState, &createdVia, &created, &updated,
		); err != nil {
			return nil, err
		}
		d.CreatedViaManifest = createdVia != 0
		d.CreatedAt, _ = time.Parse(time.RFC3339, created)
		d.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetAppSourceType returns the source type for an app ("files" or "git").
func (s *Store) GetAppSourceType(ctx context.Context, appID string) string {
	var v string
	_ = s.db.QueryRowContext(ctx, `SELECT source_type FROM apps WHERE id=?`, appID).Scan(&v)
	if v == "" {
		return "files"
	}
	return v
}

// SetAppSourceType updates the source_type for an app.
func (s *Store) SetAppSourceType(ctx context.Context, appID, sourceType string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE apps SET source_type=? WHERE id=?`, sourceType, appID)
	return err
}
