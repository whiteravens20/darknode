// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrRunActive is returned when an account that already has an active run
// asks for a new one.
var ErrRunActive = errors.New("an active run exists")

// Gateway is the player's computer.
type Gateway struct {
	CPUMHz    int
	RAMMB     int
	ModemMbps int
	StorageMB int
	NICCount  int
	Security  int
}

// Installed names one installed version of a program.
type Installed struct {
	Software string
	Version  int
}

// BankAccount is one of the player's bank accounts.
type BankAccount struct {
	Server  string
	Clean   int64
	Dirty   int64
	Primary bool
}

// NewRun is what a new run starts with.
type NewRun struct {
	AccountID int64
	Gateway   Gateway
	Bank      string
	Balance   int64
	Software  []Installed
	Known     []string
}

// Run is the state of an active run that the client is shown.
type Run struct {
	ID              int64
	Reputation      int
	ExposureSeconds int64
	CreatedAt       time.Time
	Gateway         Gateway
	Accounts        []BankAccount
	Software        []Installed
	Known           []string
}

// CreateRun starts a run. It fails with ErrRunActive when the account has one.
func (s *Store) CreateRun(ctx context.Context, n NewRun) (int64, error) {
	var id int64
	err := s.tx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, "INSERT INTO run (account_id) VALUES ($1) RETURNING id", n.AccountID).Scan(&id)
		if isUniqueViolation(err, "run_one_active_idx") {
			return ErrRunActive
		}
		if err != nil {
			return err
		}
		g := n.Gateway
		if _, err := tx.Exec(ctx, `INSERT INTO gateway (run_id, cpu_mhz, ram_mb, modem_mbps, storage_mb, nic_count, security)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, id, g.CPUMHz, g.RAMMB, g.ModemMbps, g.StorageMB, g.NICCount, g.Security); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO bank_account (run_id, bank_server, balance_clean, is_primary)
			VALUES ($1, $2, $3, true)`, id, n.Bank, n.Balance); err != nil {
			return err
		}
		for _, sw := range n.Software {
			if _, err := tx.Exec(ctx, "INSERT INTO run_software (run_id, software, version) VALUES ($1, $2, $3)",
				id, sw.Software, sw.Version); err != nil {
				return err
			}
		}
		for _, server := range n.Known {
			if _, err := tx.Exec(ctx, "INSERT INTO known_server (run_id, server) VALUES ($1, $2)", id, server); err != nil {
				return err
			}
		}
		return nil
	})
	return id, err
}

// ActiveRun returns the account's active run, or nil when it has none.
func (s *Store) ActiveRun(ctx context.Context, accountID int64) (*Run, error) {
	r := &Run{}
	err := s.pool.QueryRow(ctx, `SELECT r.id, r.reputation, r.exposure_seconds, r.created_at,
			g.cpu_mhz, g.ram_mb, g.modem_mbps, g.storage_mb, g.nic_count, g.security
		FROM run r JOIN gateway g ON g.run_id = r.id
		WHERE r.account_id = $1 AND r.active`, accountID).
		Scan(&r.ID, &r.Reputation, &r.ExposureSeconds, &r.CreatedAt,
			&r.Gateway.CPUMHz, &r.Gateway.RAMMB, &r.Gateway.ModemMbps, &r.Gateway.StorageMB,
			&r.Gateway.NICCount, &r.Gateway.Security)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	r.Accounts, err = collect(ctx, s, `SELECT bank_server, balance_clean, balance_dirty, is_primary
		FROM bank_account WHERE run_id = $1 ORDER BY is_primary DESC, id`, r.ID,
		func(row pgx.CollectableRow) (BankAccount, error) {
			var b BankAccount
			err := row.Scan(&b.Server, &b.Clean, &b.Dirty, &b.Primary)
			return b, err
		})
	if err != nil {
		return nil, err
	}
	r.Software, err = collect(ctx, s, `SELECT software, version FROM run_software
		WHERE run_id = $1 ORDER BY software, version`, r.ID,
		func(row pgx.CollectableRow) (Installed, error) {
			var i Installed
			err := row.Scan(&i.Software, &i.Version)
			return i, err
		})
	if err != nil {
		return nil, err
	}
	r.Known, err = collect(ctx, s, "SELECT server FROM known_server WHERE run_id = $1 ORDER BY server", r.ID,
		func(row pgx.CollectableRow) (string, error) {
			var srv string
			err := row.Scan(&srv)
			return srv, err
		})
	if err != nil {
		return nil, err
	}
	return r, nil
}

// AddExposure adds seconds of online time to the active runs of the given accounts.
func (s *Store) AddExposure(ctx context.Context, accountIDs []int64, seconds int64) error {
	if len(accountIDs) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx, `UPDATE run SET exposure_seconds = exposure_seconds + $2
		WHERE active AND account_id = ANY($1)`, accountIDs, seconds)
	return err
}

func collect[T any](ctx context.Context, s *Store, sql string, arg any, scan pgx.RowToFunc[T]) ([]T, error) {
	rows, err := s.pool.Query(ctx, sql, arg)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scan)
}
