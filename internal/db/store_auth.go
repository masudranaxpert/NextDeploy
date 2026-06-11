package db

import (
	"context"
	"time"
)

func (s *Store) UserCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(ctx context.Context, username, passwordHash, role string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users(username,password_hash,role,created_at) VALUES(?,?,?,?)`,
		username, passwordHash, role, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (User, error) {
	var u User
	var created string
	err := s.db.QueryRowContext(ctx,
		`SELECT id,username,password_hash,role,created_at,max_apps,max_memory_mb,max_cpus,status FROM users WHERE username=? COLLATE NOCASE`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &created, &u.MaxApps, &u.MaxMemoryMB, &u.MaxCPUs, &u.Status)
	if err != nil {
		return User{}, err
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return u, nil
}

func (s *Store) GetUserByID(ctx context.Context, id int64) (User, error) {
	var u User
	var created string
	err := s.db.QueryRowContext(ctx,
		`SELECT id,username,password_hash,role,created_at,max_apps,max_memory_mb,max_cpus,status FROM users WHERE id=?`, id).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &created, &u.MaxApps, &u.MaxMemoryMB, &u.MaxCPUs, &u.Status)
	if err != nil {
		return User{}, err
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return u, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,username,password_hash,role,created_at,max_apps,max_memory_mb,max_cpus,status FROM users ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		var created string
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &created, &u.MaxApps, &u.MaxMemoryMB, &u.MaxCPUs, &u.Status); err != nil {
			return nil, err
		}
		u.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) UpdateUserPassword(ctx context.Context, id int64, passwordHash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=?`, passwordHash, id)
	return err
}

func (s *Store) UpdateUserRole(ctx context.Context, id int64, role string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET role=? WHERE id=?`, role, id)
	return err
}

func (s *Store) UpdateUserStatus(ctx context.Context, id int64, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET status=? WHERE id=?`, status, id)
	return err
}

func (s *Store) UpdateUserLimits(ctx context.Context, id int64, maxApps, maxMemoryMB int, maxCPUs float64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET max_apps=?, max_memory_mb=?, max_cpus=? WHERE id=?`, maxApps, maxMemoryMB, maxCPUs, id)
	return err
}

func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id=?`, id)
	return err
}

func (s *Store) CreateSession(ctx context.Context, token string, userID int64, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions(token,user_id,expires_at) VALUES(?,?,?)`,
		token, userID, expiresAt.UTC().Format(time.RFC3339))
	return err
}

func (s *Store) GetSession(ctx context.Context, token string) (userID int64, expiresAt time.Time, err error) {
	var expiresStr string
	err = s.db.QueryRowContext(ctx,
		`SELECT user_id,expires_at FROM sessions WHERE token=?`, token).
		Scan(&userID, &expiresStr)
	if err != nil {
		return 0, time.Time{}, err
	}
	expiresAt, _ = time.Parse(time.RFC3339, expiresStr)
	return userID, expiresAt, nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token=?`, token)
	return err
}

func (s *Store) PruneExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at < ?`, time.Now().UTC().Format(time.RFC3339))
	return err
}
