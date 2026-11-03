// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/whiteravens20/darknode/internal/store"
)

// The shop sells the programs of the catalogue, at the prices the catalogue
// names. Money earned through the mission board is clean, and clean money is
// what the shop takes: what a player takes from a bank is not spent here
// until it has been laundered, which is a system of its own.
func (e *Env) shop(ctx context.Context, c cmd) (Result, error) {
	if len(c.args) > 0 {
		return e.shopBuy(ctx, c)
	}

	owned := map[string]int{}
	for _, i := range c.run.Software {
		owned[i.Software] = max(owned[i.Software], i.Version)
	}
	out := Result{Lines: []Line{{
		Text: fmt.Sprintf("  %-16s %-6s %-28s %s", "PROGRAM", "YOURS", "FOR SALE", "WHAT IT DOES"),
		Kind: KindHead,
	}}}
	for _, sw := range e.World.Software {
		var sale []string
		for _, v := range sw.Versions {
			if v.Version > owned[sw.ID] {
				sale = append(sale, fmt.Sprintf("v%d $%s", v.Version, money(int64(v.Price))))
			}
		}
		yours := "—"
		if owned[sw.ID] > 0 {
			yours = fmt.Sprintf("v%d", owned[sw.ID])
		}
		does := sw.Description
		if layer := toolLayer[sw.ID]; layer != "" {
			does = fmt.Sprintf("opens the %s layer", layer)
		}
		if len(sale) == 0 {
			sale = []string{"nothing left to buy"}
		}
		out.Lines = append(out.Lines, Line{
			Text: fmt.Sprintf("  %-16s %-6s %-28s %s", sw.ID, yours, strings.Join(sale, " · "), does),
		})
	}
	out.Lines = append(out.Lines,
		Line{Text: fmt.Sprintf("  You have $%s clean. `shop buy <program> [version]`.", money(balance(c.run))), Kind: KindDim},
		Line{Text: "  Hardware is not sold yet.", Kind: KindDim},
	)
	return out, nil
}

func (e *Env) shopBuy(ctx context.Context, c cmd) (Result, error) {
	if len(c.args) < 2 || strings.ToLower(c.args[0]) != "buy" || len(c.args) > 3 {
		return errorResult("Usage: shop, or shop buy <program> [version]"), nil
	}
	id := strings.ToLower(c.args[1])
	sw, ok := e.World.SoftwareByID(id)
	if !ok {
		return errorResult(fmt.Sprintf("The shop has nothing called %q. `shop` lists what it sells.", c.args[1])), nil
	}
	owned := 0
	for _, i := range c.run.Software {
		if i.Software == sw.ID {
			owned = max(owned, i.Version)
		}
	}

	// Without a version, the next one up from what the player has.
	version := owned + 1
	if len(c.args) == 3 {
		n, err := strconv.Atoi(strings.TrimPrefix(strings.ToLower(c.args[2]), "v"))
		if err != nil {
			return errorResult("Usage: shop buy <program> [version]"), nil
		}
		version = n
	}
	v, ok := sw.Version(version)
	if !ok {
		return errorResult(fmt.Sprintf("%s has no version %d.", sw.Name, version)), nil
	}
	for _, i := range c.run.Software {
		if i.Software == sw.ID && i.Version == version {
			return errorResult(fmt.Sprintf("You already have %s v%d.", sw.Name, version)), nil
		}
	}

	paid, err := e.Store.Spend(ctx, c.run.ID, int64(v.Price))
	if err != nil {
		return Result{}, err
	}
	if !paid {
		return errorResult(fmt.Sprintf("%s v%d costs $%s and you have $%s.",
			sw.Name, version, money(int64(v.Price)), money(balance(c.run)))), nil
	}
	if err := e.Store.InstallSoftware(ctx, c.run.ID, sw.ID, version); err != nil {
		return Result{}, err
	}
	return Result{Changed: true, Lines: []Line{
		{Text: fmt.Sprintf("%s v%d installed. $%s left.", sw.Name, version,
			money(balance(c.run)-int64(v.Price))), Kind: KindOK},
	}}, nil
}

func (e *Env) bank(_ context.Context, c cmd) (Result, error) {
	out := Result{Lines: []Line{{
		Text: fmt.Sprintf("  %-28s %12s %12s", "BANK", "CLEAN", "DIRTY"),
		Kind: KindHead,
	}}}
	for _, a := range c.run.Accounts {
		name := a.Server
		if s, ok := e.World.ServerByID(a.Server); ok {
			name = s.Name
		}
		if a.Primary {
			name += " (yours)"
		}
		out.Lines = append(out.Lines, Line{
			Text: fmt.Sprintf("  %-28s %12s %12s", name, "$"+money(a.Clean), "$"+money(a.Dirty)),
		})
	}
	if balance(c.run) < 0 {
		out.Lines = append(out.Lines, Line{
			Text: "  You owe money. Interest runs while you play.",
			Kind: KindError,
		})
	}
	return out, nil
}

// balance is the clean money in the run's primary account.
func balance(run *store.Run) int64 {
	for _, a := range run.Accounts {
		if a.Primary {
			return a.Clean
		}
	}
	return 0
}

// money writes an amount the way the terminal shows it, in thousands.
func money(amount int64) string {
	sign := ""
	if amount < 0 {
		sign, amount = "-", -amount
	}
	text := strconv.FormatInt(amount, 10)
	for i := len(text) - 3; i > 0; i -= 3 {
		text = text[:i] + "," + text[i:]
	}
	return sign + text
}
