// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"strings"
	"testing"
)

// TestTheShopSellsWhatTheCatalogueNames keeps the one list of prices: the
// shop shows what a run does not have yet, at the catalogue's prices, and
// says how much the player can spend.
func TestTheShopSellsWhatTheCatalogueNames(t *testing.T) {
	env, p, ctx := start(t)

	out := say(t, env, p, ctx, "shop")
	for _, want := range []string{
		"pass_breaker", "v2 $3,000", // the run has v1, so v2 is what is left
		"decrypter", "v1 $3,000", // nothing of it owned yet
		"log_wiper", "v2 $2,000 · v3 $5,000",
		"opens the password layer",
		"nothing left to buy", // one of everything else is owned already
		"You have $100 clean",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the shop lacks %q:\n%s", want, out)
		}
	}

	if out := say(t, env, p, ctx, "bank"); !strings.Contains(out, "$100") ||
		!strings.Contains(out, "(yours)") {
		t.Errorf("the bank says:\n%s", out)
	}
}

// TestAProgramIsBoughtWithCleanMoneyOrNotAtAll is the whole of the economy a
// player meets first: what is in the account is what can be spent, and a
// version is owned once.
func TestAProgramIsBoughtWithCleanMoneyOrNotAtAll(t *testing.T) {
	env, p, ctx := start(t)

	// A hundred credits buys nothing: the cheapest thing on the list is
	// five hundred.
	if out := say(t, env, p, ctx, "shop buy log_wiper 2"); !strings.Contains(out, "costs $2,000 and you have $100") {
		t.Errorf("buying beyond the account:\n%s", out)
	}
	run, err := env.Store.ActiveRun(ctx, p.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if balance(run) != StartBalance {
		t.Errorf("a refused purchase moved the balance to $%d", balance(run))
	}

	// With money in the account it goes through, once.
	if err := env.Store.Credit(ctx, run.ID, 10000, 0); err != nil {
		t.Fatal(err)
	}
	if out := say(t, env, p, ctx, "shop buy decrypter"); !strings.Contains(out, "Decrypter v1 installed") ||
		!strings.Contains(out, "$7,100 left") {
		t.Errorf("buying a program:\n%s", out)
	}
	if out := say(t, env, p, ctx, "shop buy decrypter 1"); !strings.Contains(out, "already have") {
		t.Errorf("buying the same version twice:\n%s", out)
	}
	run, err = env.Store.ActiveRun(ctx, p.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(StartBalance + 10000 - 3000); balance(run) != want {
		t.Errorf("after one purchase the account holds $%d, want $%d", balance(run), want)
	}
	if !hasTool(run, "decrypter") {
		t.Error("the program was paid for and not installed")
	}

	// Without a version it is the next one up from what the run has.
	if out := say(t, env, p, ctx, "shop buy pass_breaker"); !strings.Contains(out, "PassBreaker v2 installed") {
		t.Errorf("buying the next version up:\n%s", out)
	}
	// And what nobody sells is refused clearly.
	if out := say(t, env, p, ctx, "shop buy address_probe"); !strings.Contains(out, "no version 2") {
		t.Errorf("buying past the last version:\n%s", out)
	}
	if out := say(t, env, p, ctx, "shop buy nonsense"); !strings.Contains(out, "nothing called") {
		t.Errorf("buying what nobody wrote:\n%s", out)
	}
	if out := say(t, env, p, ctx, "shop sideways"); !strings.Contains(out, "Usage") {
		t.Errorf("an invented subcommand:\n%s", out)
	}
}

// TestMoneyIsWrittenTheWayPeopleReadIt is small but it is on every screen the
// player sees.
func TestMoneyIsWrittenTheWayPeopleReadIt(t *testing.T) {
	for amount, want := range map[int64]string{
		0: "0", 100: "100", 1000: "1,000", 12345: "12,345",
		1234567: "1,234,567", -500: "-500", -12345: "-12,345",
	} {
		if got := money(amount); got != want {
			t.Errorf("money(%d) = %q, want %q", amount, got, want)
		}
	}
}
