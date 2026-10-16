// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package store

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestEmbeddedMigrationsLoad(t *testing.T) {
	ms, err := loadMigrations(migrationFiles)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) == 0 || ms[0].version != 1 {
		t.Fatalf("embedded migrations: %+v", ms)
	}
}

func TestMigrationNumberingIsChecked(t *testing.T) {
	cases := map[string]fstest.MapFS{
		"gap": {
			"migrations/0001_a.sql": {Data: []byte("SELECT 1")},
			"migrations/0003_c.sql": {Data: []byte("SELECT 1")},
		},
		"bad name": {
			"migrations/1_a.sql": {Data: []byte("SELECT 1")},
		},
	}
	for name, fsys := range cases {
		if _, err := loadMigrations(fsys); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestAppliedMigrationsAreChecked(t *testing.T) {
	ms, err := loadMigrations(fstest.MapFS{
		"migrations/0001_a.sql": {Data: []byte("SELECT 1")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkApplied(map[int][]byte{1: ms[0].checksum}, ms); err != nil {
		t.Fatalf("matching history refused: %v", err)
	}
	if err := checkApplied(map[int][]byte{1: []byte("other")}, ms); err == nil || !strings.Contains(err.Error(), "changed after it was applied") {
		t.Fatalf("an edited migration was not caught: %v", err)
	}
	if err := checkApplied(map[int][]byte{1: ms[0].checksum, 2: []byte("x")}, ms); err == nil || !strings.Contains(err.Error(), "does not have") {
		t.Fatalf("a newer database was not caught: %v", err)
	}
}
