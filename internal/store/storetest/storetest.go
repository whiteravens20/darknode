// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

// Package storetest gives each test a fresh, migrated database of its own.
//
// It needs DARKNODE_TEST_DATABASE_URL, a connection to a PostgreSQL server
// as a user that may create databases. Without it the database tests are
// skipped, unless DARKNODE_TEST_REQUIRE_DB is set, as it is in CI: there a
// missing database fails the run instead of passing it untested.
package storetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/whiteravens20/darknode/internal/store"
)

// URL creates an empty database and returns its connection string. The
// database is dropped when the test ends.
func URL(t testing.TB) string {
	t.Helper()
	admin := os.Getenv("DARKNODE_TEST_DATABASE_URL")
	if admin == "" {
		if os.Getenv("DARKNODE_TEST_REQUIRE_DB") != "" {
			t.Fatal("DARKNODE_TEST_DATABASE_URL is not set, and DARKNODE_TEST_REQUIRE_DB requires the database tests")
		}
		t.Skip("set DARKNODE_TEST_DATABASE_URL to run the database tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg, err := pgx.ParseConfig(admin)
	if err != nil {
		t.Fatalf("DARKNODE_TEST_DATABASE_URL: %v", err)
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect to the test server: %v", err)
	}
	defer conn.Close(ctx)

	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	name := "darknode_test_" + hex.EncodeToString(suffix)
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create the test database: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		conn, err := pgx.ConnectConfig(ctx, cfg)
		if err != nil {
			t.Errorf("drop the test database: %v", err)
			return
		}
		defer conn.Close(ctx)
		if _, err := conn.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)"); err != nil {
			t.Errorf("drop the test database: %v", err)
		}
	})

	u, err := url.Parse(admin)
	if err != nil || u.Scheme == "" {
		t.Fatalf("DARKNODE_TEST_DATABASE_URL must be a postgres:// URL: %v", err)
	}
	u.Path = "/" + name
	return u.String()
}

// Open returns a store on a fresh, migrated database.
func Open(t testing.TB) *store.Store {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(ctx, URL(t), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return s
}
