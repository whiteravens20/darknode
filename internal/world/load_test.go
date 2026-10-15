// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package world

import (
	"strings"
	"testing"
	"testing/fstest"
)

// A small, valid catalogue used as the base; tests mutate a copy of it.
func base() fstest.MapFS {
	return fstest.MapFS{
		"world.yaml": &fstest.MapFile{Data: []byte(`
epoch: 2049-01-01T00:00:00Z
start:
  links: [test-1, central-bank]
  bank: central-bank
  software:
    - { id: pass_breaker, version: 1 }
`)},
		"software.yaml": &fstest.MapFile{Data: []byte(`
software:
  - id: pass_breaker
    name: PassBreaker
    description: Opens the password layer.
    versions:
      - { version: 1, price: 1000, size_mb: 2, cpu_mhz: 0, ram_mb: 0 }
      - { version: 2, price: 3000, size_mb: 3, cpu_mhz: 0, ram_mb: 0 }
`)},
		"orgs/academy.yaml": &fstest.MapFile{Data: []byte(`
id: academy
name: Riverside Academy
type: DARKNODE_TEST
faction: ""
location: { x: 42, y: 38 }
servers:
  - id: test-1
    name: Practice Machine
    ip: 10.0.0.1
    security: 1
    files:
      - { path: /readme.txt, size_mb: 1, content: "A practice machine." }
`)},
		"orgs/bank.yaml": &fstest.MapFile{Data: []byte(`
id: bank
name: Central Bank
type: BANK
faction: CORPORATE
location: { x: 60, y: 30 }
servers:
  - id: central-bank
    name: Central Bank
    ip: 172.16.1.1
    security: 6
    proxy: true
    firewall: true
    monitor: true
`)},
	}
}

func TestLoadValid(t *testing.T) {
	w, err := Load(base())
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Orgs) != 2 || len(w.Servers) != 2 || len(w.Software) != 1 {
		t.Fatalf("counts: orgs=%d servers=%d software=%d", len(w.Orgs), len(w.Servers), len(w.Software))
	}
	if w.Orgs[0].ID != "academy" || w.Orgs[1].ID != "bank" {
		t.Errorf("orgs not sorted by id: %s, %s", w.Orgs[0].ID, w.Orgs[1].ID)
	}
	s, ok := w.ServerByID("test-1")
	if !ok || !s.Type().Public() == false {
		// DARKNODE_TEST is not public; Public() must be false.
		if !ok {
			t.Fatal("test-1 not found")
		}
	}
	if s.Type().Public() {
		t.Error("DARKNODE_TEST should not be public")
	}
	if _, ok := w.ServerByIP(s.IP); !ok {
		t.Error("server not indexed by IP")
	}
	if w.Start.Bank == nil || w.Start.Bank.ID != "central-bank" {
		t.Error("start bank not resolved")
	}
	if len(w.Start.Software) != 1 || w.Start.Software[0] != (SoftwareRef{ID: "pass_breaker", Version: 1}) {
		t.Errorf("start software: %+v", w.Start.Software)
	}
	if !w.Epoch.Equal(w.Epoch.UTC()) {
		t.Error("epoch not in UTC")
	}
}

func TestLoadReportsManyProblems(t *testing.T) {
	cases := map[string]struct {
		file, body, want string
	}{
		"public ip": {"orgs/academy.yaml",
			strings.Replace(string(base()["orgs/academy.yaml"].Data), "10.0.0.1", "8.8.8.8", 1),
			"public internet address"},
		"bad security": {"orgs/academy.yaml",
			strings.Replace(string(base()["orgs/academy.yaml"].Data), "security: 1", "security: 11", 1),
			"must be from 1 to 10"},
		"unknown type": {"orgs/academy.yaml",
			strings.Replace(string(base()["orgs/academy.yaml"].Data), "DARKNODE_TEST", "WORKSHOP", 1),
			"unknown type"},
		"duplicate ip": {"orgs/bank.yaml",
			strings.Replace(string(base()["orgs/bank.yaml"].Data), "172.16.1.1", "10.0.0.1", 1),
			"also the address of"},
		"start bank not a bank": {"world.yaml",
			strings.Replace(string(base()["world.yaml"].Data), "bank: central-bank", "bank: test-1", 1),
			"is not a bank"},
		"unknown start software": {"world.yaml",
			strings.Replace(string(base()["world.yaml"].Data), "id: pass_breaker, version: 1", "id: ghost, version: 1", 1),
			"unknown program"},
		"filename mismatch": {"orgs/academy.yaml",
			strings.Replace(string(base()["orgs/academy.yaml"].Data), "id: academy", "id: school", 1),
			"must be named school.yaml"},
		"unknown yaml key": {"orgs/academy.yaml",
			string(base()["orgs/academy.yaml"].Data) + "colour: green\n",
			"field colour not found"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fsys := base()
			fsys[tc.file] = &fstest.MapFile{Data: []byte(tc.body)}
			_, err := Load(fsys)
			if err == nil {
				t.Fatalf("the bad catalogue was accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error does not mention %q:\n%v", tc.want, err)
			}
		})
	}
}
