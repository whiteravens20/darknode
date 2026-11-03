// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"context"
	"testing"
)

// debtState reads where a run's debt stands.
func debtState(t *testing.T, env *Env, runID int64) (balance int64, open bool, charged, missed int) {
	t.Helper()
	debtors, err := env.Store.Debtors(context.Background(), DebtThreshold)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range debtors {
		if d.RunID == runID {
			return d.Balance, d.Since != nil, d.Charged, d.Missed
		}
	}
	return 0, false, 0, 0
}

// TestOwingMoneyGetsWorseWhileYouPlay walks the whole of a debt: the window
// opens when the balance falls far enough, interest is added for each game
// day of play, the first window to run out costs the player their gear, and
// the second ends the run.
func TestOwingMoneyGetsWorseWhileYouPlay(t *testing.T) {
	env, _, accounts := engineEnv(t, "agent")
	e := NewEngine(env, &recorder{}, quietLogger())
	ctx := context.Background()
	account := accounts[0]
	run, err := env.Store.ActiveRun(ctx, account)
	if err != nil {
		t.Fatal(err)
	}

	// A fine takes the balance well below the threshold.
	if _, err := env.Store.Fine(ctx, run.ID, 700); err != nil {
		t.Fatal(err)
	}
	e.advanceDebts(ctx)
	balance, open, charged, missed := debtState(t, env, run.ID)
	if balance != StartBalance-700 {
		t.Fatalf("the balance is $%d, want $%d", balance, StartBalance-700)
	}
	if !open || charged != 0 || missed != 0 {
		t.Fatalf("after falling into debt: open=%v charged=%d missed=%d", open, charged, missed)
	}

	// A game day of play adds interest, once.
	if err := env.Store.AddExposure(ctx, []int64{account}, GameDay); err != nil {
		t.Fatal(err)
	}
	e.advanceDebts(ctx)
	e.advanceDebts(ctx)
	owed := int64(600)
	withInterest, _, charged, _ := debtState(t, env, run.ID)
	if want := -(owed + int64(float64(owed)*DebtInterest)); withInterest != want {
		t.Errorf("after one day of play the balance is $%d, want $%d", withInterest, want)
	}
	if charged != 1 {
		t.Errorf("interest was charged %d time(s) for one day of play", charged)
	}

	// The window runs out: they come for the gear and give one more window.
	before, err := env.Store.ActiveRun(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Store.AddExposure(ctx, []int64{account}, DebtWindow); err != nil {
		t.Fatal(err)
	}
	e.advanceDebts(ctx)
	_, open, charged, missed = debtState(t, env, run.ID)
	if !open || missed != 1 || charged != 0 {
		t.Errorf("after the first window ran out: open=%v charged=%d missed=%d", open, charged, missed)
	}
	after, err := env.Store.ActiveRun(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Software) != len(before.Software)-1 {
		t.Errorf("the gateway holds %d program(s), want %d: one was owed",
			len(after.Software), len(before.Software)-1)
	}

	// The second window runs out and the run is over.
	if err := env.Store.AddExposure(ctx, []int64{account}, DebtWindow); err != nil {
		t.Fatal(err)
	}
	e.advanceDebts(ctx)
	if r, err := env.Store.ActiveRun(ctx, account); err != nil {
		t.Fatal(err)
	} else if r != nil {
		t.Error("a second window ran out and the run is still active")
	}
}

// TestPayingTheDebtOffClosesTheWindow is the way out: money in the account
// and nobody is chasing you.
func TestPayingTheDebtOffClosesTheWindow(t *testing.T) {
	env, _, accounts := engineEnv(t, "agent")
	e := NewEngine(env, &recorder{}, quietLogger())
	ctx := context.Background()
	account := accounts[0]
	run, _ := env.Store.ActiveRun(ctx, account)

	if _, err := env.Store.Fine(ctx, run.ID, 1000); err != nil {
		t.Fatal(err)
	}
	e.advanceDebts(ctx)
	if _, open, _, _ := debtState(t, env, run.ID); !open {
		t.Fatal("no window opened on a debt of $900")
	}

	if err := env.Store.Credit(ctx, run.ID, 2000, 0); err != nil {
		t.Fatal(err)
	}
	e.advanceDebts(ctx)
	balance, open, _, _ := debtState(t, env, run.ID)
	if open {
		t.Errorf("the window is still open with $%d in the account", balance)
	}
}
