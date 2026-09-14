package db

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// APIToken represents a persistent access token for API and MCP clients.
type APIToken struct {
	ID                 int64      `json:"id"`
	UserID             int64      `json:"user_id"`
	Name               string     `json:"name"`
	TokenHash          string     `json:"-"`
	TokenPrefix        string     `json:"token_prefix"`
	Kind               string     `json:"kind"` // "cli" | "mcp"
	AllowEnvReveal     bool       `json:"allow_env_reveal"`
	AllowServerExec    bool       `json:"allow_server_exec"`
	AllowContainerExec bool       `json:"allow_container_exec"`
	CreatedAt          time.Time  `json:"created_at"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	LastUsedAt         *time.Time `json:"last_used_at,omitempty"`
}

// HashToken calculates the SHA-256 hex digest of an API token.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}

// CreateAPIToken generates a new secure token, stores its hash, and returns the raw secret.
func (s *Store) CreateAPIToken(ctx context.Context, userID int64, name, kind string, expiresAt *time.Time, allowEnvReveal, allowServerExec, allowContainerExec bool) (string, APIToken, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Default API Token"
	}
	kind = strings.TrimSpace(kind)
	if kind != "mcp" && kind != "cli" {
		kind = "cli"
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", APIToken{}, fmt.Errorf("crypto rand failed: %w", err)
	}
	rawToken := "nd_" + hex.EncodeToString(b)
	tokenHash := HashToken(rawToken)
	prefix := rawToken[:8] + "..."
	now := time.Now().UTC()

	var expStr sql.NullString
	if expiresAt != nil && !expiresAt.IsZero() {
		expStr = sql.NullString{String: expiresAt.UTC().Format(time.RFC3339), Valid: true}
	}

	revealVal := 0
	if allowEnvReveal {
		revealVal = 1
	}
	execVal := 0
	if allowServerExec {
		execVal = 1
	}
	containerExecVal := 0
	if allowContainerExec {
		containerExecVal = 1
	}

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO api_tokens (user_id, name, token_hash, token_prefix, kind, allow_env_reveal, allow_server_exec, allow_container_exec, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, name, tokenHash, prefix, kind, revealVal, execVal, containerExecVal, now.Format(time.RFC3339), expStr)
	if err != nil {
		return "", APIToken{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return "", APIToken{}, err
	}

	return rawToken, APIToken{
		ID:                 id,
		UserID:             userID,
		Name:               name,
		TokenHash:          tokenHash,
		TokenPrefix:        prefix,
		Kind:               kind,
		AllowEnvReveal:     allowEnvReveal,
		AllowServerExec:    allowServerExec,
		AllowContainerExec: allowContainerExec,
		CreatedAt:          now,
		ExpiresAt:          expiresAt,
	}, nil
}

// ValidateAPIToken checks a raw token and returns the associated User and APIToken if valid and unexpired.
func (s *Store) ValidateAPIToken(ctx context.Context, rawToken string) (User, APIToken, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return User{}, APIToken{}, errors.New("empty token")
	}
	tokenHash := HashToken(rawToken)
	nowStr := time.Now().UTC().Format(time.RFC3339)

	var t APIToken
	var expStr sql.NullString
	var lastUsed sql.NullString
	var createdStr string
	var kindStr sql.NullString
	var allowRevealInt, allowServerExecInt, allowContainerExecInt int

	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, name, token_prefix, COALESCE(kind, 'cli'), allow_env_reveal, allow_server_exec, allow_container_exec, created_at, expires_at, last_used_at 
		 FROM api_tokens 
		 WHERE token_hash = ? AND (expires_at IS NULL OR expires_at = '' OR expires_at > ?)`,
		tokenHash, nowStr).Scan(&t.ID, &t.UserID, &t.Name, &t.TokenPrefix, &kindStr, &allowRevealInt, &allowServerExecInt, &allowContainerExecInt, &createdStr, &expStr, &lastUsed)
	if err != nil {
		return User{}, APIToken{}, err
	}

	t.TokenHash = tokenHash
	t.Kind = kindStr.String
	if t.Kind == "" {
		t.Kind = "cli"
	}
	t.AllowEnvReveal = allowRevealInt == 1
	t.AllowServerExec = allowServerExecInt == 1
	t.AllowContainerExec = allowContainerExecInt == 1
	t.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	if expStr.Valid && expStr.String != "" {
		if parsed, err := time.Parse(time.RFC3339, expStr.String); err == nil {
			t.ExpiresAt = &parsed
		}
	}
	if lastUsed.Valid && lastUsed.String != "" {
		if parsed, err := time.Parse(time.RFC3339, lastUsed.String); err == nil {
			t.LastUsedAt = &parsed
		}
	}

	_, _ = s.db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`,
		nowStr, t.ID)

	u, err := s.GetUserByID(ctx, t.UserID)
	return u, t, err
}

// ListAPITokensForUser returns metadata for all tokens belonging to the specified user.
func (s *Store) ListAPITokensForUser(ctx context.Context, userID int64) ([]APIToken, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, name, token_prefix, COALESCE(kind, 'cli'), allow_env_reveal, allow_server_exec, allow_container_exec, created_at, expires_at, last_used_at 
		 FROM api_tokens WHERE user_id = ? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []APIToken
	for rows.Next() {
		var t APIToken
		var created string
		var kindStr sql.NullString
		var expires, lastUsed sql.NullString
		var allowRevealInt, allowServerExecInt, allowContainerExecInt int
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.TokenPrefix, &kindStr, &allowRevealInt, &allowServerExecInt, &allowContainerExecInt, &created, &expires, &lastUsed); err != nil {
			return nil, err
		}
		t.Kind = kindStr.String
		if t.Kind == "" {
			t.Kind = "cli"
		}
		t.AllowEnvReveal = allowRevealInt == 1
		t.AllowServerExec = allowServerExecInt == 1
		t.AllowContainerExec = allowContainerExecInt == 1
		t.CreatedAt, _ = time.Parse(time.RFC3339, created)
		if expires.Valid && expires.String != "" {
			if parsed, err := time.Parse(time.RFC3339, expires.String); err == nil {
				t.ExpiresAt = &parsed
			}
		}
		if lastUsed.Valid && lastUsed.String != "" {
			if parsed, err := time.Parse(time.RFC3339, lastUsed.String); err == nil {
				t.LastUsedAt = &parsed
			}
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

// ToggleAPITokenEnvReveal toggles whether the token is allowed to call env_reveal.
func (s *Store) ToggleAPITokenEnvReveal(ctx context.Context, id int64, userID int64) (bool, error) {
	var cur int
	if err := s.db.QueryRowContext(ctx, `SELECT allow_env_reveal FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID).Scan(&cur); err != nil {
		return false, err
	}
	newVal := 1
	if cur == 1 {
		newVal = 0
	}
	_, err := s.db.ExecContext(ctx, `UPDATE api_tokens SET allow_env_reveal = ? WHERE id = ? AND user_id = ?`, newVal, id, userID)
	return newVal == 1, err
}

// ToggleAPITokenServerExec toggles whether the token is allowed to call server_exec.
func (s *Store) ToggleAPITokenServerExec(ctx context.Context, id int64, userID int64) (bool, error) {
	var cur int
	if err := s.db.QueryRowContext(ctx, `SELECT allow_server_exec FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID).Scan(&cur); err != nil {
		return false, err
	}
	newVal := 1
	if cur == 1 {
		newVal = 0
	}
	_, err := s.db.ExecContext(ctx, `UPDATE api_tokens SET allow_server_exec = ? WHERE id = ? AND user_id = ?`, newVal, id, userID)
	return newVal == 1, err
}

// ToggleAPITokenContainerExec toggles whether the token is allowed to call container_exec.
func (s *Store) ToggleAPITokenContainerExec(ctx context.Context, id int64, userID int64) (bool, error) {
	var cur int
	if err := s.db.QueryRowContext(ctx, `SELECT allow_container_exec FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID).Scan(&cur); err != nil {
		return false, err
	}
	newVal := 1
	if cur == 1 {
		newVal = 0
	}
	_, err := s.db.ExecContext(ctx, `UPDATE api_tokens SET allow_container_exec = ? WHERE id = ? AND user_id = ?`, newVal, id, userID)
	return newVal == 1, err
}

// DeleteAPIToken removes a token owned by the user.
func (s *Store) DeleteAPIToken(ctx context.Context, id int64, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID)
	return err
}
