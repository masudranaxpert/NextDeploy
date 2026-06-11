package db

import (
	"context"
	"database/sql"
	"time"
)

func (s *Store) AddCollaborator(ctx context.Context, appID string, userID int64, role string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO app_collaborators (app_id, user_id, role, created_at) VALUES (?, ?, ?, ?)`,
		appID, userID, role, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *Store) RemoveCollaborator(ctx context.Context, appID string, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM app_collaborators WHERE app_id = ? AND user_id = ?`,
		appID, userID)
	return err
}

func (s *Store) GetCollaborator(ctx context.Context, appID string, userID int64) (AppCollaborator, error) {
	var c AppCollaborator
	var created string
	err := s.db.QueryRowContext(ctx,
		`SELECT app_id, user_id, role, created_at FROM app_collaborators WHERE app_id = ? AND user_id = ?`,
		appID, userID).Scan(&c.AppID, &c.UserID, &c.Role, &created)
	if err != nil {
		return AppCollaborator{}, err
	}
	c.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return c, nil
}

func (s *Store) ListCollaborators(ctx context.Context, appID string) ([]AppCollaborator, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT app_id, user_id, role, created_at FROM app_collaborators WHERE app_id = ?`,
		appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AppCollaborator
	for rows.Next() {
		var c AppCollaborator
		var created string
		if err := rows.Scan(&c.AppID, &c.UserID, &c.Role, &created); err != nil {
			return nil, err
		}
		c.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) AddPrivateRegistry(ctx context.Context, reg PrivateRegistry) (int64, error) {
	var uid interface{}
	if reg.UserID != nil {
		uid = *reg.UserID
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO private_registries (user_id, name, server_address, username, password_encrypted, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		uid, reg.Name, reg.ServerAddress, reg.Username, reg.PasswordEncrypted, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdatePrivateRegistry(ctx context.Context, reg PrivateRegistry) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE private_registries SET name = ?, server_address = ?, username = ?, password_encrypted = ? WHERE id = ?`,
		reg.Name, reg.ServerAddress, reg.Username, reg.PasswordEncrypted, reg.ID)
	return err
}

func (s *Store) DeletePrivateRegistry(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM private_registries WHERE id = ?`, id)
	return err
}

func (s *Store) GetPrivateRegistry(ctx context.Context, id int64) (PrivateRegistry, error) {
	var r PrivateRegistry
	var created string
	var uid sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, name, server_address, username, password_encrypted, created_at FROM private_registries WHERE id = ?`,
		id).Scan(&r.ID, &uid, &r.Name, &r.ServerAddress, &r.Username, &r.PasswordEncrypted, &created)
	if err != nil {
		return PrivateRegistry{}, err
	}
	if uid.Valid {
		val := uid.Int64
		r.UserID = &val
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return r, nil
}

func (s *Store) ListPrivateRegistries(ctx context.Context, userID *int64) ([]PrivateRegistry, error) {
	var query string
	var args []interface{}
	if userID != nil {
		query = `SELECT id, user_id, name, server_address, username, password_encrypted, created_at 
		         FROM private_registries WHERE user_id IS NULL OR user_id = ? ORDER BY id ASC`
		args = append(args, *userID)
	} else {
		query = `SELECT id, user_id, name, server_address, username, password_encrypted, created_at 
		         FROM private_registries ORDER BY id ASC`
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PrivateRegistry
	for rows.Next() {
		var r PrivateRegistry
		var created string
		var uid sql.NullInt64
		if err := rows.Scan(&r.ID, &uid, &r.Name, &r.ServerAddress, &r.Username, &r.PasswordEncrypted, &created); err != nil {
			return nil, err
		}
		if uid.Valid {
			val := uid.Int64
			r.UserID = &val
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetGlobalAllocatedResources(ctx context.Context) (totalMemoryMB int64, totalCPUs float64, err error) {
	err = s.db.QueryRowContext(ctx, 
		`SELECT COALESCE(SUM(max_memory_mb), 0), COALESCE(SUM(max_cpus), 0) FROM users WHERE status = 'active'`).Scan(&totalMemoryMB, &totalCPUs)
	return
}

func (s *Store) TransferAppOwnership(ctx context.Context, appID string, newOwnerID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE apps SET owner_id = ? WHERE id = ?`, newOwnerID, appID)
	return err
}
