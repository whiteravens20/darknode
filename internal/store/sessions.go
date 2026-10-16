// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Session is a signed-in browser, found by the hash of its cookie token.
type Session struct {
	AccountID  int64
	Username   string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
}

// CreateSession starts a session for an account.
func (s *Store) CreateSession(ctx context.Context, accountID int64, tokenHash []byte, expires time.Time) error {
	return s.tx(ctx, func(tx pgx.Tx) error { return insertSession(ctx, tx, accountID, tokenHash, expires) })
}

func insertSession(ctx context.Context, tx pgx.Tx, accountID int64, tokenHash []byte, expires time.Time) error {
	_, err := tx.Exec(ctx, "INSERT INTO session (token_hash, account_id, expires_at) VALUES ($1, $2, $3)",
		tokenHash, accountID, expires)
	return err
}

// SessionByToken returns the unexpired session with the given token hash.
func (s *Store) SessionByToken(ctx context.Context, tokenHash []byte) (Session, error) {
	var x Session
	err := s.pool.QueryRow(ctx, `SELECT s.account_id, a.username, s.created_at, s.last_seen_at, s.expires_at
		FROM session s JOIN account a ON a.id = s.account_id
		WHERE s.token_hash = $1 AND s.expires_at > now()`, tokenHash).
		Scan(&x.AccountID, &x.Username, &x.CreatedAt, &x.LastSeenAt, &x.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	return x, err
}

// TouchSession records that a session was used and moves its expiry.
func (s *Store) TouchSession(ctx context.Context, tokenHash []byte, expires time.Time) error {
	_, err := s.pool.Exec(ctx, "UPDATE session SET last_seen_at = now(), expires_at = $2 WHERE token_hash = $1",
		tokenHash, expires)
	return err
}

// DeleteSession ends one session.
func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM session WHERE token_hash = $1", tokenHash)
	return err
}

// DeleteExpiredSessions removes sessions past their expiry and reports how many.
func (s *Store) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, "DELETE FROM session WHERE expires_at <= now()")
	return tag.RowsAffected(), err
}
