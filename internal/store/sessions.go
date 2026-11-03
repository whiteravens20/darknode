// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Session is a signed-in browser, found by the hash of its cookie token.
//
// Every judgement about a session's age is made against the time its caller
// passes in, never against the database's own now(): the server has one clock,
// and two clocks deciding one thing can disagree.
type Session struct {
	AccountID  int64
	Username   string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
}

// CreateSession starts a session for an account at started, ending at expires.
func (s *Store) CreateSession(ctx context.Context, accountID int64, tokenHash []byte, started, expires time.Time) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		return insertSession(ctx, tx, accountID, tokenHash, started, expires)
	})
}

func insertSession(ctx context.Context, tx pgx.Tx, accountID int64, tokenHash []byte, started, expires time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO session (token_hash, account_id, created_at, last_seen_at, expires_at)
		VALUES ($1, $2, $3, $3, $4)`, tokenHash, accountID, started, expires)
	return err
}

// SessionByToken returns the session with the given token hash, as long as it
// has not expired by now.
func (s *Store) SessionByToken(ctx context.Context, tokenHash []byte, now time.Time) (Session, error) {
	var x Session
	err := s.pool.QueryRow(ctx, `SELECT s.account_id, a.username, s.created_at, s.last_seen_at, s.expires_at
		FROM session s JOIN account a ON a.id = s.account_id
		WHERE s.token_hash = $1 AND s.expires_at > $2`, tokenHash, now).
		Scan(&x.AccountID, &x.Username, &x.CreatedAt, &x.LastSeenAt, &x.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	return x, err
}

// TouchSession records that a session was used at now and moves its expiry.
func (s *Store) TouchSession(ctx context.Context, tokenHash []byte, now, expires time.Time) error {
	_, err := s.pool.Exec(ctx, "UPDATE session SET last_seen_at = $2, expires_at = $3 WHERE token_hash = $1",
		tokenHash, now, expires)
	return err
}

// DeleteSession ends one session.
func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM session WHERE token_hash = $1", tokenHash)
	return err
}

// DeleteExpiredSessions removes sessions that have expired by now and reports
// how many.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, "DELETE FROM session WHERE expires_at <= $1", now)
	return tag.RowsAffected(), err
}
