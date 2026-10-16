// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// LaunchedAt returns the moment the instance was first started, recording it
// on the first call. The shared world clock counts from it.
func (s *Store) LaunchedAt(ctx context.Context) (time.Time, error) {
	if _, err := s.pool.Exec(ctx, "INSERT INTO instance DEFAULT VALUES ON CONFLICT (id) DO NOTHING"); err != nil {
		return time.Time{}, err
	}
	var t time.Time
	err := s.pool.QueryRow(ctx, "SELECT launched_at FROM instance WHERE id = 1").Scan(&t)
	return t, err
}

// CreateInvites stores the HMACs of new invite codes, all or none.
func (s *Store) CreateInvites(ctx context.Context, macs [][]byte) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		for _, mac := range macs {
			if _, err := tx.Exec(ctx, "INSERT INTO invite (code_mac) VALUES ($1)", mac); err != nil {
				return err
			}
		}
		return nil
	})
}
