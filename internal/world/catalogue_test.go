// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package world_test

import (
	"os"
	"testing"

	"github.com/whiteravens20/darknode/internal/world"
)

// catalogueDir is the catalogue that ships with the server, relative to this
// package.
const catalogueDir = "../../data/world"

// TestShippedCatalogueLoads keeps the data files that ship valid: a typo in
// one of them fails here, not at a player's first start.
func TestShippedCatalogueLoads(t *testing.T) {
	w, err := world.Load(os.DirFS(catalogueDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Orgs) == 0 || len(w.Servers) == 0 || len(w.Software) == 0 {
		t.Fatalf("the catalogue is empty: orgs=%d servers=%d software=%d",
			len(w.Orgs), len(w.Servers), len(w.Software))
	}
	if w.Start.Bank == nil {
		t.Fatal("no starting bank")
	}
	if len(w.Start.Links) == 0 {
		t.Fatal("a new agent would know no addresses")
	}
	if len(w.Start.Software) == 0 {
		t.Fatal("a new gateway would have no programs")
	}
	if w.Epoch.IsZero() {
		t.Fatal("no epoch")
	}
}

// TestEveryStartingLinkIsReachable checks that what a new agent is given is
// consistent: the bank is among the links, and the starting programs exist.
func TestEveryStartingLinkIsReachable(t *testing.T) {
	w, err := world.Load(os.DirFS(catalogueDir))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range w.Start.Links {
		if _, ok := w.ServerByIP(s.IP); !ok {
			t.Errorf("starting link %s has no address in the catalogue", s.ID)
		}
		if s == w.Start.Bank {
			found = true
		}
	}
	if !found {
		t.Error("the starting bank is not among the starting links")
	}
	for _, ref := range w.Start.Software {
		sw, ok := w.SoftwareByID(ref.ID)
		if !ok {
			t.Errorf("starting program %s is not in the shop", ref.ID)
			continue
		}
		if _, ok := sw.Version(ref.Version); !ok {
			t.Errorf("starting program %s has no version %d", ref.ID, ref.Version)
		}
	}
}

// TestPracticeServerIsGentle keeps the machine a new player meets first easy:
// no defences, lowest security.
func TestPracticeServerIsGentle(t *testing.T) {
	w, err := world.Load(os.DirFS(catalogueDir))
	if err != nil {
		t.Fatal(err)
	}
	var practice *world.Server
	for _, s := range w.Start.Links {
		if s.Type() == world.TypeDarkNodeTest {
			practice = s
			break
		}
	}
	if practice == nil {
		t.Fatal("a new agent has no practice machine among its links")
	}
	if practice.Security != 1 || practice.Proxy || practice.Firewall || practice.Monitor {
		t.Errorf("the practice machine is not gentle: security=%d proxy=%v firewall=%v monitor=%v",
			practice.Security, practice.Proxy, practice.Firewall, practice.Monitor)
	}
	if len(practice.Files) == 0 {
		t.Error("the practice machine has nothing to read")
	}
}
