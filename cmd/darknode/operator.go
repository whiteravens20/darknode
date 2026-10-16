// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/whiteravens20/darknode/internal/auth"
	"github.com/whiteravens20/darknode/internal/store"
)

// invite makes invite codes and prints them. Only the codes' HMACs are
// stored, so this is the one time they can be read.
func invite(ctx context.Context, args []string, getenv func(string) string) error {
	fs := flagSet("invite")
	count := fs.Int("n", 1, "how many codes to make")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *count < 1 || *count > 1000 {
		return errors.New("-n must be from 1 to 1000")
	}

	cfg, db, _, err := setup(ctx, getenv)
	if err != nil {
		return err
	}
	defer db.Close()

	codes := make([]string, 0, *count)
	macs := make([][]byte, 0, *count)
	for range *count {
		code := auth.NewCode()
		normalized, err := auth.NormalizeCode(code)
		if err != nil {
			return err
		}
		codes = append(codes, code)
		macs = append(macs, auth.CodeMAC(cfg.Secret, auth.InviteCode, normalized))
	}
	if err := db.CreateInvites(ctx, macs); err != nil {
		return fmt.Errorf("store the invite codes: %w", err)
	}
	for _, c := range codes {
		fmt.Println(c)
	}
	return nil
}

// recoveryReset gives an account a fresh set of recovery codes and ends its
// sessions. It is the last resort when a player has lost every code.
func recoveryReset(ctx context.Context, args []string, getenv func(string) string) error {
	fs := flagSet("recovery-reset")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: darknode recovery-reset NAME")
	}
	name := fs.Arg(0)

	cfg, db, _, err := setup(ctx, getenv)
	if err != nil {
		return err
	}
	defer db.Close()

	acct, err := db.AccountByUsername(ctx, name)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: %s", errNoAccount, name)
	}
	if err != nil {
		return err
	}

	codes := make([]string, 0, auth.RecoveryCodeCount)
	macs := make([][]byte, 0, auth.RecoveryCodeCount)
	for range auth.RecoveryCodeCount {
		code := auth.NewCode()
		normalized, err := auth.NormalizeCode(code)
		if err != nil {
			return err
		}
		codes = append(codes, code)
		macs = append(macs, auth.CodeMAC(cfg.Secret, auth.RecoveryCode, normalized))
	}
	if err := db.ReplaceRecoveryCodes(ctx, acct.ID, macs); err != nil {
		return fmt.Errorf("store the recovery codes: %w", err)
	}

	fmt.Printf("New recovery codes for %s. Its sessions were ended.\n", acct.Username)
	for _, c := range codes {
		fmt.Println(c)
	}
	return nil
}
