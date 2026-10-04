package store

import (
	"context"
	"time"
)

// CreateSession stores the hash of a new session token.
func (s *Store) CreateSession(ctx context.Context, tokenHash string, expires time.Time) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO sessions (token_hash, expires_at) VALUES (?, ?)",
		tokenHash, expires.UTC().Format(timeFormat))
	return err
}

// SessionValid reports whether the session exists and has not expired.
func (s *Store) SessionValid(ctx context.Context, tokenHash string, now time.Time) (bool, error) {
	var ok bool
	err := s.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM sessions WHERE token_hash = ? AND expires_at > ?)",
		tokenHash, now.UTC().Format(timeFormat)).Scan(&ok)
	return ok, err
}

// DeleteSession removes one session.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", tokenHash)
	return err
}

// DeleteOtherSessions removes every session except the one with keepHash.
func (s *Store) DeleteOtherSessions(ctx context.Context, keepHash string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash <> ?", keepHash)
	return err
}

// DeleteExpiredSessions removes sessions that expired before now.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ?", now.UTC().Format(timeFormat))
	return err
}
