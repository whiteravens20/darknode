// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/whiteravens20/darknode/internal/auth"
	"github.com/whiteravens20/darknode/internal/game"
	"github.com/whiteravens20/darknode/internal/server"
	"github.com/whiteravens20/darknode/internal/world"
)

// worldDir is where the catalogue's data files are read from.
const worldDirEnv = "DARKNODE_WORLD_DIR"

func serve(ctx context.Context, args []string, getenv func(string) string) error {
	fs := flagSet("serve")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, db, logger, err := setup(ctx, getenv)
	if err != nil {
		return err
	}
	defer db.Close()

	dir := getenv(worldDirEnv)
	if dir == "" {
		dir = "data/world"
	}
	w, err := world.Load(os.DirFS(dir))
	if err != nil {
		return fmt.Errorf("load the world from %s: %w", dir, err)
	}
	logger.Info("world loaded", "dir", dir, "organisations", len(w.Orgs), "servers", len(w.Servers))

	launched, err := db.LaunchedAt(ctx)
	if err != nil {
		return fmt.Errorf("read the instance clock: %w", err)
	}

	env := &game.Env{
		Store: db,
		World: w,
		Clock: game.Clock{Epoch: w.Epoch, Launched: launched},
		Now:   time.Now,
	}

	// One password hash holds its memory cost until it finishes, so the
	// number that may run at once is tied to the machine, not to requests.
	concurrent := max(runtime.GOMAXPROCS(0)/2, 1)
	srv := server.New(server.Options{
		Config:  cfg,
		Store:   db,
		Game:    env,
		Hasher:  auth.NewHasher(auth.DefaultArgon2, concurrent),
		Puzzle:  auth.NewPuzzle(auth.DefaultPuzzle(cfg.PowBits)),
		Logger:  logger,
		Version: version,
		Now:     time.Now,
	})

	ctx, stop := notifyContext(ctx)
	defer stop()

	background, cancelBackground := context.WithCancel(ctx)
	defer cancelBackground()
	go srv.Background(background)

	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// No WriteTimeout: the state stream is a long-lived response. Read
		// and idle timeouts still bound a connection that sends nothing.
		ReadTimeout: 30 * time.Second,
		IdleTimeout: 2 * time.Minute,
		ErrorLog:    nil,
	}

	errs := make(chan error, 1)
	go func() {
		logger.Info("listening", "address", cfg.Listen, "registration", string(cfg.Registration), "version", version)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	srv.CloseStreams()
	shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdown); err != nil {
		return fmt.Errorf("shut down: %w", err)
	}
	return nil
}
