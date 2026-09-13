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
	ID          int64      `json:"id"`
	UserID      int64      `json:"user_id"`
	Name        string     `json:"name"`
	TokenHash   string     `json:"-"`
	TokenPrefix string     `json:"token_prefix"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
}

// HashToken calculates the SHA-256 hex digest of an API token.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}

// CreateAPIToken generates a new secure token, stores its hash, and returns the raw secret.
func (s *Store) CreateAPIToken(ctx context.Context, userID int64, name string, expiresAt *time.Time) (string, APIToken, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Default API Token"
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

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO api_tokens (user_id, name, token_hash, token_prefix, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		userID, name, tokenHash, prefix, now.Format(time.RFC3339), expStr)
	if err != nil {
		return "", APIToken{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return "", APIToken{}, err
	}

	return rawToken, APIToken{
		ID:          id,
		UserID:      userID,
		Name:        name,
		TokenHash:   tokenHash,
		TokenPrefix: prefix,
		CreatedAt:   now,
		ExpiresAt:   expiresAt,
	}, nil
}

// ValidateAPIToken checks a raw token and returns the associated User if valid and unexpired.
func (s *Store) ValidateAPIToken(ctx context.Context, rawToken string) (User, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return User{}, errors.New("empty token")
	}
	tokenHash := HashToken(rawToken)
	nowStr := time.Now().UTC().Format(time.RFC3339)

	var userID int64
	var tokenID int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id FROM api_tokens 
		 WHERE token_hash = ? AND (expires_at IS NULL OR expires_at = '' OR expires_at > ?)`,
		tokenHash, nowStr).Scan(&tokenID, &userID)
	if err != nil {
		return User{}, err
	}

	_, _ = s.db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`,
		nowStr, tokenID)

	return s.GetUserByID(ctx, userID)
}

// ListAPITokensForUser returns metadata for all tokens belonging to the specified user.
func (s *Store) ListAPITokensForUser(ctx context.Context, userID int64) ([]APIToken, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, name, token_prefix, created_at, expires_at, last_used_at 
		 FROM api_tokens WHERE user_id = ? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []APIToken
	for rows.Next() {
		var t APIToken
		var created string
		var expires, lastUsed sql.NullString
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.TokenPrefix, &created, &expires, &lastUsed); err != nil {
			return nil, err
		}
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

// DeleteAPIToken removes a token owned by the user.
func (s *Store) DeleteAPIToken(ctx context.Context, id int64, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID)
	return err
}
