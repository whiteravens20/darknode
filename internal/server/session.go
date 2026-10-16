// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/whiteravens20/darknode/internal/auth"
	"github.com/whiteravens20/darknode/internal/game"
	"github.com/whiteravens20/darknode/internal/store"
)

// cookieName is the name of the session cookie. The __Host- prefix means a
// browser keeps it only when it was set Secure, from the exact host, with
// path "/", so it cannot be fixed by a subdomain or over plain HTTP.
const (
	cookieName      = "__Host-darknode"
	cookieNamePlain = "darknode_session"
)

// cookieName returns the cookie name used for the current transport.
func (s *Server) cookieName() string {
	if s.Config.CookieSecure {
		return cookieName
	}
	return cookieNamePlain
}

// setSessionCookie sends the session token to the browser.
func (s *Server) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName(),
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.Config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionLifetime / time.Second),
	})
}

// clearSessionCookie tells the browser to drop the session cookie.
func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName(),
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.Config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// withSession wraps a handler that needs a signed-in player. It reads the
// cookie, loads the session, hands the player to the handler, and moves the
// session's expiry forward at most once an hour.
func (s *Server) withSession(next func(http.ResponseWriter, *http.Request, sessionContext)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(s.cookieName())
		if err != nil || c.Value == "" {
			writeError(w, http.StatusUnauthorized, "no_session", "Not signed in.")
			return
		}
		tokenHash := auth.TokenHash(c.Value)
		sess, err := s.Store.SessionByToken(r.Context(), tokenHash)
		if errors.Is(err, store.ErrNotFound) {
			s.clearSessionCookie(w)
			writeError(w, http.StatusUnauthorized, "no_session", "Your session has ended. Sign in again.")
			return
		}
		if err != nil {
			s.Logger.Error("load session", "err", err)
			writeError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
			return
		}

		// A session ends after sessionIdle without use, and after
		// sessionLifetime from its start whatever happens.
		now := s.Now()
		expires := now.Add(sessionIdle)
		if limit := sess.CreatedAt.Add(sessionLifetime); expires.After(limit) {
			expires = limit
		}
		if now.Sub(sess.LastSeenAt) >= sessionTouch {
			if err := s.Store.TouchSession(r.Context(), tokenHash, expires); err != nil {
				s.Logger.Error("touch session", "err", err)
			} else {
				s.setSessionCookie(w, c.Value) // move the browser's expiry forward too
			}
		}

		next(w, r, sessionContext{
			player:    game.Player{AccountID: sess.AccountID, Username: sess.Username},
			tokenHash: tokenHash,
		})
	}
}

// sessionContext is what a signed-in handler is given.
type sessionContext struct {
	player game.Player
	// tokenHash identifies this session, for closing its own stream.
	tokenHash []byte
}
