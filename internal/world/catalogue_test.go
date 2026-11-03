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

// load reads the catalogue that ships, failing the test if it does not.
func load(t *testing.T) *world.World {
	t.Helper()
	w, err := world.Load(os.DirFS(catalogueDir))
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// TestTheServersTheBalanceWasWorkedOutAgainstExist keeps the catalogue and the
// game's numbers together. Break-in times, trace rates and the deadline an
// administrator sets were all computed for three configurations; if none of
// them is in the world any more, every number the game is tuned by describes a
// server nobody can reach.
func TestTheServersTheBalanceWasWorkedOutAgainstExist(t *testing.T) {
	w := load(t)
	for _, want := range []struct {
		security                 int
		proxy, firewall, monitor bool
	}{
		{security: 4, firewall: true, monitor: true},
		{security: 6, proxy: true, firewall: true, monitor: true},
		{security: 8, proxy: true, firewall: true, monitor: true},
	} {
		var found *world.Server
		for _, s := range w.Servers {
			if s.Security == want.security && s.Proxy == want.proxy &&
				s.Firewall == want.firewall && s.Monitor == want.monitor {
				found = s
				break
			}
		}
		if found == nil {
			t.Errorf("no server with security %d, proxy=%v firewall=%v monitor=%v: the balance was worked out against one",
				want.security, want.proxy, want.firewall, want.monitor)
			continue
		}
		if found.Type().Public() {
			t.Errorf("%s stands in for a target of the balance but its type %s starts no trace",
				found.ID, found.Type())
		}
		sensitive := false
		for _, f := range found.Files {
			sensitive = sensitive || f.Sensitive
		}
		if !sensitive {
			t.Errorf("%s has nothing worth stealing or destroying, so no mission can point at it", found.ID)
		}
	}
}

// minRelays is how many servers a player must be able to choose a route from.
// A connection may be bounced through as many relays as the gateway has
// network cards, and the upgraded gateway the balance assumes has four; with
// exactly four in the world there would be one possible route and no choice.
const minRelays = 6

// TestThereAreEnoughRelaysToChooseARouteFrom guards the one thing that makes a
// hard target possible at all: ground for the trace to cover.
func TestThereAreEnoughRelaysToChooseARouteFrom(t *testing.T) {
	w := load(t)
	orgs := map[string]bool{}
	relays := 0
	for _, s := range w.Servers {
		switch s.Type() {
		case world.TypeISP, world.TypeUniversity:
			relays++
			orgs[s.Org.ID] = true
		}
	}
	if relays < minRelays {
		t.Errorf("the catalogue has %d relays (ISP or university servers), want at least %d", relays, minRelays)
	}
	if len(orgs) < 2 {
		t.Errorf("every relay belongs to %d organisation(s); a route through one operator is no route", len(orgs))
	}
}

// TestInterNICIsPublicAndKnownFromTheStart is what finding anything depends
// on: the register is readable without breaking in, and a new agent has its
// address.
func TestInterNICIsPublicAndKnownFromTheStart(t *testing.T) {
	w := load(t)
	var registry *world.Server
	for _, s := range w.Start.Links {
		if s.Type() == world.TypeInterNIC {
			registry = s
			break
		}
	}
	if registry == nil {
		t.Fatal("a new agent does not know the address of the register")
	}
	if !registry.Type().Public() {
		t.Errorf("%s is the register and must be readable without a break-in", registry.ID)
	}
	if len(registry.Files) == 0 {
		t.Errorf("%s has nothing to read", registry.ID)
	}
}
