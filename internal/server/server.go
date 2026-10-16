// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

// Package server is the HTTP side of the game: the JSON API, the state stream
// and the files of the web client.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/whiteravens20/darknode/internal/auth"
	"github.com/whiteravens20/darknode/internal/config"
	"github.com/whiteravens20/darknode/internal/game"
	"github.com/whiteravens20/darknode/internal/store"
)

// Session lifetimes: a session ends after two weeks without use, and after
// sixty days in any case.
const (
	sessionIdle     = 14 * 24 * time.Hour
	sessionLifetime = 60 * 24 * time.Hour
	sessionTouch    = time.Hour // how often a used session's expiry is moved
)

// Options are what a Server is built from.
type Options struct {
	Config  config.Config
	Store   *store.Store
	Game    *game.Env
	Hasher  *auth.Hasher
	Puzzle  *auth.Puzzle
	Logger  *slog.Logger
	Version string
	// Now returns the real time; tests replace it.
	Now func() time.Time
}

// Server serves the game.
type Server struct {
	Options

	// Registration limits, per network and in total.
	regHour, regDay, regGlobal *auth.Window
	// Sign-in and recovery slow-downs, per account and network, and per network.
	loginBackoff, loginNetBackoff, recoverBackoff *auth.Backoff

	hub *Hub
}

// New returns a Server.
func New(o Options) *Server {
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Server{
		Options:         o,
		regHour:         auth.NewWindow(3, time.Hour),
		regDay:          auth.NewWindow(10, 24*time.Hour),
		regGlobal:       auth.NewWindow(30, time.Minute),
		loginBackoff:    auth.NewBackoff(3, 30*time.Second, 15*time.Minute),
		loginNetBackoff: auth.NewBackoff(10, 30*time.Second, 15*time.Minute),
		recoverBackoff:  auth.NewBackoff(3, 30*time.Second, 15*time.Minute),
		hub:             NewHub(5, 5000),
	}
}

// Handler returns the server's routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("POST /api/auth/challenge", s.challenge)
	mux.HandleFunc("POST /api/auth/register", s.register)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("POST /api/auth/recover", s.recoverAccount)
	mux.HandleFunc("GET /api/session", s.withSession(s.sessionInfo))
	mux.HandleFunc("POST /api/cmd", s.withSession(s.command))
	mux.HandleFunc("GET /api/stream", s.withSession(s.stream))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "No such endpoint.")
	})
	if s.Config.WebDir != "" {
		mux.Handle("/", spa(s.Config.WebDir))
	}
	return s.recoverPanics(s.securityHeaders(s.sameOrigin(mux)))
}

// Background runs the server's periodic work until ctx ends: it forgets old
// rate-limit entries and expired sessions, and counts online time.
func (s *Server) Background(ctx context.Context) {
	exposure := time.NewTicker(exposureTick)
	prune := time.NewTicker(time.Minute)
	sessions := time.NewTicker(time.Hour)
	defer exposure.Stop()
	defer prune.Stop()
	defer sessions.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-exposure.C:
			if err := s.Store.AddExposure(ctx, s.hub.Online(), int64(exposureTick/time.Second)); err != nil && ctx.Err() == nil {
				s.Logger.Error("count online time", "err", err)
			}
		case <-prune.C:
			now := s.Now()
			for _, w := range []*auth.Window{s.regHour, s.regDay, s.regGlobal} {
				w.Prune(now)
			}
			for _, b := range []*auth.Backoff{s.loginBackoff, s.loginNetBackoff, s.recoverBackoff} {
				b.Prune(now)
			}
			s.Puzzle.Prune(now)
		case <-sessions.C:
			if n, err := s.Store.DeleteExpiredSessions(ctx); err != nil && ctx.Err() == nil {
				s.Logger.Error("delete expired sessions", "err", err)
			} else if n > 0 {
				s.Logger.Info("deleted expired sessions", "count", n)
			}
		}
	}
}

// CloseStreams ends every open state stream, for a shutdown.
func (s *Server) CloseStreams() { s.hub.CloseAll() }

// exposureTick is how often online time is added to the runs of players
// whose stream is open.
const exposureTick = 5 * time.Second

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.Store.Ping(ctx); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}
