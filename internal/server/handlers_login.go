// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/whiteravens20/darknode/internal/auth"
	"github.com/whiteravens20/darknode/internal/store"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Username string `json:"username"`
}

// The same answer for a name that does not exist and a wrong password, so
// the reply does not say which accounts exist.
const signInFailed = "Wrong name or password."

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := readJSON(w, r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	name := strings.TrimSpace(req.Username)
	netKey := s.limitKey(r)
	acctKey := strings.ToLower(name)
	now := s.Now()

	if wait := maxWait(s.loginBackoff.Wait(acctKey, now), s.loginNetBackoff.Wait(netKey, now)); wait > 0 {
		writeRetryAfter(w, wait)
		return
	}

	acct, err := s.Store.AccountByUsername(r.Context(), name)
	if errors.Is(err, store.ErrNotFound) {
		// Spend the time of a real check, so an unknown name does not
		// answer faster than a wrong password.
		s.Hasher.VerifyNothing(r.Context(), req.Password)
		s.failLogin(acctKey, netKey, now)
		writeError(w, http.StatusUnauthorized, "sign_in_failed", signInFailed)
		return
	}
	if err != nil {
		s.Logger.Error("load account", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}

	ok, rehash, err := s.Hasher.Verify(r.Context(), acct.PasswordHash, req.Password)
	if err != nil {
		s.Logger.Error("verify password", "account", acct.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	if !ok {
		s.failLogin(acctKey, netKey, now)
		writeError(w, http.StatusUnauthorized, "sign_in_failed", signInFailed)
		return
	}

	if rehash {
		if hash, err := s.Hasher.Hash(r.Context(), req.Password); err == nil {
			if err := s.Store.SetPasswordHash(r.Context(), acct.ID, hash); err != nil {
				s.Logger.Error("store rehashed password", "account", acct.ID, "err", err)
			}
		}
	}

	token := auth.NewSessionToken()
	if err := s.Store.CreateSession(r.Context(), acct.ID, auth.TokenHash(token), now.Add(sessionIdle)); err != nil {
		s.Logger.Error("create session", "account", acct.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	s.loginBackoff.Reset(acctKey)
	s.loginNetBackoff.Reset(netKey)
	s.setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, loginResponse{Username: acct.Username})
}

func (s *Server) failLogin(acctKey, netKey string, now time.Time) {
	s.loginBackoff.Fail(acctKey, now)
	s.loginNetBackoff.Fail(netKey, now)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(s.cookieName()); err == nil && c.Value != "" {
		hash := auth.TokenHash(c.Value)
		if err := s.Store.DeleteSession(r.Context(), hash); err != nil {
			s.Logger.Error("delete session", "err", err)
		}
		s.hub.CloseSession(hash)
	}
	s.clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type recoverRequest struct {
	Username string `json:"username"`
	Code     string `json:"code"`
	Password string `json:"password"`
}

// recoverAccount takes one recovery code and a new password. It ends every
// other session of the account and signs this browser in.
func (s *Server) recoverAccount(w http.ResponseWriter, r *http.Request) {
	var req recoverRequest
	if err := readJSON(w, r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	name := strings.TrimSpace(req.Username)
	netKey := s.limitKey(r)
	now := s.Now()
	if wait := s.recoverBackoff.Wait(netKey, now); wait > 0 {
		writeRetryAfter(w, wait)
		return
	}
	if n := len([]rune(req.Password)); n < minPassword || n > maxPassword {
		writeError(w, http.StatusBadRequest, "password", "A password is 8 to 128 characters.")
		return
	}

	const failed = "That name and recovery code do not match."
	normalized, err := auth.NormalizeCode(req.Code)
	if err != nil {
		s.recoverBackoff.Fail(netKey, now)
		writeError(w, http.StatusUnauthorized, "recovery_failed", failed)
		return
	}
	acct, err := s.Store.AccountByUsername(r.Context(), name)
	if errors.Is(err, store.ErrNotFound) {
		s.recoverBackoff.Fail(netKey, now)
		writeError(w, http.StatusUnauthorized, "recovery_failed", failed)
		return
	}
	if err != nil {
		s.Logger.Error("load account", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}

	hash, err := s.Hasher.Hash(r.Context(), req.Password)
	if err != nil {
		s.Logger.Error("hash password", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	token := auth.NewSessionToken()
	err = s.Store.Recover(r.Context(), acct.ID,
		auth.CodeMAC(s.Config.Secret, auth.RecoveryCode, normalized),
		hash, auth.TokenHash(token), now.Add(sessionIdle))
	if errors.Is(err, store.ErrCodeInvalid) {
		s.recoverBackoff.Fail(netKey, now)
		writeError(w, http.StatusUnauthorized, "recovery_failed", failed)
		return
	}
	if err != nil {
		s.Logger.Error("recover account", "account", acct.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}

	s.hub.CloseAccount(acct.ID)
	s.recoverBackoff.Reset(netKey)
	s.loginBackoff.Reset(strings.ToLower(name))
	s.setSessionCookie(w, token)
	left, err := s.Store.RecoveryCodesLeft(r.Context(), acct.ID)
	if err != nil {
		s.Logger.Error("count recovery codes", "account", acct.ID, "err", err)
	}
	s.Logger.Info("account recovered", "account", acct.ID, "codes_left", left)
	writeJSON(w, http.StatusOK, map[string]any{"username": acct.Username, "codes_left": left})
}

// sessionInfo says who is signed in; the client calls it when the page loads.
func (s *Server) sessionInfo(w http.ResponseWriter, r *http.Request, sc sessionContext) {
	writeJSON(w, http.StatusOK, map[string]string{"username": sc.player.Username})
}

// handleConfig tells the client what this instance allows, before sign-in.
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"registration": string(s.Config.Registration),
		"version":      s.Version,
	})
}

func writeRetryAfter(w http.ResponseWriter, wait time.Duration) {
	seconds := int(wait.Round(time.Second)/time.Second) + 1
	w.Header().Set("Retry-After", fmt.Sprint(seconds))
	writeError(w, http.StatusTooManyRequests, "slow_down",
		fmt.Sprintf("Too many attempts. Wait %d seconds and try again.", seconds))
}

func maxWait(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
