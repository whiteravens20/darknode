// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// migrationLock is the advisory lock that keeps two starting servers from
// migrating the same database at once ("darknode" as eight bytes).
const migrationLock int64 = 0x6461726b6e6f6465

var migrationName = regexp.MustCompile(`^(\d{4})_[a-z0-9_]+\.sql$`)

type migration struct {
	version  int
	name     string
	sql      string
	checksum []byte
}

func loadMigrations(fsys fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, "migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		m := migrationName.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, fmt.Errorf("migration file %q is not named NNNN_name.sql", e.Name())
		}
		body, err := fs.ReadFile(fsys, "migrations/"+e.Name())
		if err != nil {
			return nil, err
		}
		version, _ := strconv.Atoi(m[1])
		sum := sha256.Sum256(body)
		out = append(out, migration{version: version, name: e.Name(), sql: string(body), checksum: sum[:]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i, m := range out {
		if m.version != i+1 {
			return nil, fmt.Errorf("migrations must be numbered 1, 2, 3 without gaps; %s is out of place", m.name)
		}
	}
	return out, nil
}

// Migrate brings the schema up to date. It refuses to run against a database
// that has a migration this build does not know, or one whose file changed
// after it was applied: both mean the binary and the database disagree.
func (s *Store) Migrate(ctx context.Context) error {
	return s.migrate(ctx, migrationFiles)
}

func (s *Store) migrate(ctx context.Context, fsys fs.FS) error {
	migrations, err := loadMigrations(fsys)
	if err != nil {
		return err
	}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLock); err != nil {
		return fmt.Errorf("take the migration lock: %w", err)
	}
	// The lock is also released when the connection closes, so an error here is harmless.
	defer conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", migrationLock)

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migration (
		version    integer PRIMARY KEY,
		name       text NOT NULL,
		checksum   bytea NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		return err
	}

	applied := map[int][]byte{}
	rows, err := conn.Query(ctx, "SELECT version, checksum FROM schema_migration")
	if err != nil {
		return err
	}
	for rows.Next() {
		var v int
		var sum []byte
		if err := rows.Scan(&v, &sum); err != nil {
			rows.Close()
			return err
		}
		applied[v] = sum
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	if err := checkApplied(applied, migrations); err != nil {
		return err
	}

	for _, m := range migrations {
		if _, done := applied[m.version]; done {
			continue
		}
		err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, m.sql); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "INSERT INTO schema_migration (version, name, checksum) VALUES ($1, $2, $3)",
				m.version, m.name, m.checksum)
			return err
		})
		if err != nil {
			return fmt.Errorf("apply %s: %w", m.name, err)
		}
	}
	return nil
}

// checkApplied compares the migrations recorded in the database with the
// ones in this build.
func checkApplied(applied map[int][]byte, migrations []migration) error {
	known := map[int]migration{}
	for _, m := range migrations {
		known[m.version] = m
	}
	for v, sum := range applied {
		m, ok := known[v]
		if !ok {
			return fmt.Errorf("the database has migration %d, which this build does not have; run a newer build", v)
		}
		if !bytes.Equal(m.checksum, sum) {
			return fmt.Errorf("migration %s changed after it was applied; add a new migration instead", m.name)
		}
	}
	return nil
}
