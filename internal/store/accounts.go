// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Errors of account operations.
var (
	ErrUsernameTaken = errors.New("username taken")
	ErrInviteInvalid = errors.New("invite code invalid or used")
	ErrCodeInvalid   = errors.New("recovery code invalid or used")
)

// Account is a login.
type Account struct {
	ID           int64
	Username     string
	PasswordHash string
	CreatedAt    time.Time
}

// NewAccount is everything a registration writes, in one transaction.
type NewAccount struct {
	Username     string
	PasswordHash string
	// RecoveryMACs are the HMACs of the account's recovery codes.
	RecoveryMACs [][]byte
	// InviteMAC is the HMAC of the invite code, or nil when none is needed.
	InviteMAC []byte
	// SessionHash and SessionExpires start the first session.
	SessionHash    []byte
	SessionExpires time.Time
}

// CreateAccount writes a new account with its recovery codes and first
// session, and uses up the invite when one is given.
func (s *Store) CreateAccount(ctx context.Context, n NewAccount) (Account, error) {
	var a Account
	err := s.tx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `INSERT INTO account (username, username_key, password_hash)
			VALUES ($1, lower($1), $2) RETURNING id, username, password_hash, created_at`,
			n.Username, n.PasswordHash).Scan(&a.ID, &a.Username, &a.PasswordHash, &a.CreatedAt)
		if isUniqueViolation(err, "account_username_key_key") {
			return ErrUsernameTaken
		}
		if err != nil {
			return err
		}
		if n.InviteMAC != nil {
			tag, err := tx.Exec(ctx, `UPDATE invite SET used_by = $1, used_at = now()
				WHERE code_mac = $2 AND used_at IS NULL`, a.ID, n.InviteMAC)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return ErrInviteInvalid
			}
		}
		if err := insertRecoveryCodes(ctx, tx, a.ID, n.RecoveryMACs); err != nil {
			return err
		}
		return insertSession(ctx, tx, a.ID, n.SessionHash, n.SessionExpires)
	})
	return a, err
}

// AccountByUsername finds an account by its name, ignoring letter case.
func (s *Store) AccountByUsername(ctx context.Context, username string) (Account, error) {
	var a Account
	err := s.pool.QueryRow(ctx, `SELECT id, username, password_hash, created_at
		FROM account WHERE username_key = lower($1)`, username).
		Scan(&a.ID, &a.Username, &a.PasswordHash, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	return a, err
}

// SetPasswordHash replaces an account's password hash.
func (s *Store) SetPasswordHash(ctx context.Context, accountID int64, hash string) error {
	_, err := s.pool.Exec(ctx, "UPDATE account SET password_hash = $2 WHERE id = $1", accountID, hash)
	return err
}

// Recover uses up one recovery code, sets a new password hash, ends every
// session of the account and starts a new one, all in one transaction.
func (s *Store) Recover(ctx context.Context, accountID int64, codeMAC []byte, hash string, sessionHash []byte, expires time.Time) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE recovery_code SET used_at = now()
			WHERE account_id = $1 AND code_mac = $2 AND used_at IS NULL`, accountID, codeMAC)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrCodeInvalid
		}
		if _, err := tx.Exec(ctx, "UPDATE account SET password_hash = $2 WHERE id = $1", accountID, hash); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM session WHERE account_id = $1", accountID); err != nil {
			return err
		}
		return insertSession(ctx, tx, accountID, sessionHash, expires)
	})
}

// RecoveryCodesLeft counts the account's unused recovery codes.
func (s *Store) RecoveryCodesLeft(ctx context.Context, accountID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, "SELECT count(*) FROM recovery_code WHERE account_id = $1 AND used_at IS NULL", accountID).Scan(&n)
	return n, err
}

// ReplaceRecoveryCodes gives an account a fresh set of recovery codes and
// ends its sessions. The operator uses it when a player has lost every code.
func (s *Store) ReplaceRecoveryCodes(ctx context.Context, accountID int64, macs [][]byte) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "DELETE FROM recovery_code WHERE account_id = $1", accountID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM session WHERE account_id = $1", accountID); err != nil {
			return err
		}
		return insertRecoveryCodes(ctx, tx, accountID, macs)
	})
}

func insertRecoveryCodes(ctx context.Context, tx pgx.Tx, accountID int64, macs [][]byte) error {
	for _, mac := range macs {
		if _, err := tx.Exec(ctx, "INSERT INTO recovery_code (account_id, code_mac) VALUES ($1, $2)", accountID, mac); err != nil {
			return err
		}
	}
	return nil
}
