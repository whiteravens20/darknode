// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"context"
	"fmt"

	"github.com/whiteravens20/darknode/internal/store"
)

// Owing money is the one penalty that gets worse while nothing happens. A
// balance far enough below nothing starts a window; interest is added for
// every game day of play inside it; and the end of the window costs the
// player their gear, or the run.
//
// Like every other pressure in the game, it runs on online time: a player who
// stops playing stops the clock, and stops earning as well.
const (
	// DebtThreshold is the balance below which a window starts.
	DebtThreshold = -500
	// DebtWindow is how long the player has, in seconds of play. A game day
	// is 1440 of them, and the window is three.
	DebtWindow = 3 * GameDay
	// DebtInterest is added to what is owed for every game day of play.
	DebtInterest = 0.05
	// GameDay is a day of the game's clock, in seconds of online time.
	GameDay = 24 * 60
)

// advanceDebts charges interest and closes windows that have run out. It runs
// with the online time the loop has just written down.
func (e *Engine) advanceDebts(ctx context.Context) {
	debtors, err := e.env.Store.Debtors(ctx, DebtThreshold)
	if err != nil {
		if ctx.Err() == nil {
			e.log.Error("read the runs that owe money", "err", err)
		}
		return
	}
	for _, d := range debtors {
		e.advanceDebt(ctx, d)
	}
}

func (e *Engine) advanceDebt(ctx context.Context, d store.Debtor) {
	// Out of the hole: the window closes and nothing is owed.
	if d.Balance >= DebtThreshold && d.Since != nil {
		if err := e.env.Store.SaveDebt(ctx, d.RunID, nil, 0, d.Missed); err != nil {
			e.log.Error("clear a debt", "run", d.RunID, "err", err)
			return
		}
		e.out.Publish(d.AccountID, DeltaAlert, AlertDelta{
			Text: "Your balance is out of the red. Nobody is chasing you for it now.",
			Kind: KindOK,
		})
		return
	}
	if d.Balance >= DebtThreshold {
		return
	}

	// Into the hole: the window starts and the player is told, because this
	// is a debt somebody is collecting, not a secret.
	if d.Since == nil {
		since := d.Exposure
		if err := e.env.Store.SaveDebt(ctx, d.RunID, &since, 0, d.Missed); err != nil {
			e.log.Error("open a debt", "run", d.RunID, "err", err)
			return
		}
		e.out.Publish(d.AccountID, DeltaAlert, AlertDelta{
			Text: fmt.Sprintf("You owe $%s. You have three days of play to clear it, and it grows while you play.",
				money(-d.Balance)),
			Kind: KindError,
		})
		return
	}

	played := d.Exposure - *d.Since
	// Interest, once for each game day of play inside the window.
	if days := int(played / GameDay); days > d.Charged {
		interest := int64(float64(-d.Balance) * DebtInterest * float64(days-d.Charged))
		if _, err := e.env.Store.Fine(ctx, d.RunID, interest); err != nil {
			e.log.Error("charge interest", "run", d.RunID, "err", err)
			return
		}
		if err := e.env.Store.SaveDebt(ctx, d.RunID, d.Since, days, d.Missed); err != nil {
			e.log.Error("write down the interest charged", "run", d.RunID, "err", err)
			return
		}
		e.out.Publish(d.AccountID, DeltaAlert, AlertDelta{
			Text: fmt.Sprintf("Interest of $%s on what you owe.", money(interest)),
			Kind: KindError,
		})
		e.publishRun(ctx, d.AccountID)
		d.Charged = days
	}

	if played < DebtWindow {
		return
	}

	// The window has run out. The first time they come for the gear and give
	// the player another window; the second time the run is over.
	if d.Missed == 0 {
		name, err := e.seizeDearest(ctx, e.runOf(ctx, d.AccountID))
		if err != nil {
			e.log.Error("seize for a debt", "run", d.RunID, "err", err)
		}
		since := d.Exposure
		if err := e.env.Store.SaveDebt(ctx, d.RunID, &since, 0, 1); err != nil {
			e.log.Error("write down a missed window", "run", d.RunID, "err", err)
			return
		}
		text := "Your window ran out. They came for what you owe."
		if name != "" {
			text = fmt.Sprintf("Your window ran out. They took %s against what you owe.", name)
		}
		e.out.Publish(d.AccountID, DeltaAlert, AlertDelta{Text: text, Kind: KindError})
		e.publishRun(ctx, d.AccountID)
		return
	}

	if err := e.env.Store.EndRun(ctx, d.RunID, e.env.Now()); err != nil {
		e.log.Error("end a run for debt", "run", d.RunID, "err", err)
		return
	}
	e.log.Info("run ended: a second window on a debt ran out", "run", d.RunID)
	e.out.Publish(d.AccountID, DeltaAlert, AlertDelta{
		Text: "Your second window ran out. This run is over.", Kind: KindError,
	})
	e.out.Publish(d.AccountID, DeltaResync, ResyncDelta{})
}

// runOf reads the account's active run, or nil.
func (e *Engine) runOf(ctx context.Context, account int64) *store.Run {
	run, err := e.env.Store.ActiveRun(ctx, account)
	if err != nil {
		e.log.Error("read a run", "account", account, "err", err)
		return nil
	}
	return run
}

// publishRun sends the run's state to the player's browsers.
func (e *Engine) publishRun(ctx context.Context, account int64) {
	view, err := e.env.RunState(ctx, Player{AccountID: account})
	if err != nil {
		e.log.Error("build the run state", "account", account, "err", err)
		return
	}
	e.out.Publish(account, DeltaRun, RunDelta{Run: view})
}
