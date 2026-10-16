// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

// Command darknode runs the game server.
//
// Without arguments it serves the game. The other subcommands are for the
// operator: `migrate` brings the database up to date, `invite` makes invite
// codes, and `recovery-reset` gives an account a fresh set of recovery codes.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/whiteravens20/darknode/internal/config"
	"github.com/whiteravens20/darknode/internal/store"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(context.Background(), os.Args[1:], os.Getenv); err != nil {
		fmt.Fprintln(os.Stderr, "darknode: "+err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string) error {
	command := "serve"
	if len(args) > 0 && !isFlag(args[0]) {
		command, args = args[0], args[1:]
	}

	switch command {
	case "serve":
		return serve(ctx, args, getenv)
	case "migrate":
		return migrate(ctx, getenv)
	case "invite":
		return invite(ctx, args, getenv)
	case "recovery-reset":
		return recoveryReset(ctx, args, getenv)
	case "version":
		fmt.Println("darknode " + version)
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", command)
	}
}

func isFlag(s string) bool { return len(s) > 1 && s[0] == '-' }

func usage() {
	fmt.Fprint(os.Stderr, `darknode - the game server

Usage:
  darknode [serve]              serve the game (the default)
  darknode migrate              bring the database up to date and exit
  darknode invite [-n COUNT]    make invite codes and print them
  darknode recovery-reset NAME  give an account new recovery codes
  darknode version              print the version

Settings come from the environment; see the README.
`)
}

// setup reads the configuration, opens the database and migrates it.
func setup(ctx context.Context, getenv func(string) string) (config.Config, *store.Store, *slog.Logger, error) {
	cfg, err := config.FromEnv(getenv)
	if err != nil {
		return cfg, nil, nil, err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	db, err := store.Open(ctx, cfg.DatabaseURL, cfg.DatabasePassword)
	if err != nil {
		return cfg, nil, logger, err
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		return cfg, nil, logger, fmt.Errorf("migrate the database: %w", err)
	}
	return cfg, db, logger, nil
}

func migrate(ctx context.Context, getenv func(string) string) error {
	_, db, logger, err := setup(ctx, getenv)
	if err != nil {
		return err
	}
	defer db.Close()
	logger.Info("database is up to date")
	return nil
}

func notifyContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
}

var errNoAccount = errors.New("no such account")

// flagSet returns a flag set that prints its errors to stderr.
func flagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}
