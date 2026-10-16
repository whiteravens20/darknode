// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package server

import (
	"errors"
	"net/http"
	"strings"
	"unicode"

	"github.com/whiteravens20/darknode/internal/auth"
	"github.com/whiteravens20/darknode/internal/config"
	"github.com/whiteravens20/darknode/internal/store"
)

// Account name and password limits.
const (
	minUsername = 3
	maxUsername = 24
	minPassword = 8
	maxPassword = 128
)

// challengeResponse is the puzzle a browser solves before it may register.
type challengeResponse struct {
	ID   string `json:"id"`
	Bits int    `json:"bits"`
	// MinAgeMS is how long after the puzzle was handed out a solution is
	// accepted. The page waits it out; a script that does not is turned away.
	MinAgeMS int64 `json:"min_age_ms"`
}

func (s *Server) challenge(w http.ResponseWriter, r *http.Request) {
	if s.Config.Registration != config.RegistrationOpen {
		writeError(w, http.StatusForbidden, "registration_closed", s.registrationClosedMessage())
		return
	}
	key := s.limitKey(r)
	c, err := s.Puzzle.Issue(key, s.Now())
	if errors.Is(err, auth.ErrPuzzleBusy) {
		writeError(w, http.StatusTooManyRequests, "busy", "The server is busy. Try again shortly.")
		return
	}
	if err != nil {
		s.Logger.Error("issue challenge", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	writeJSON(w, http.StatusOK, challengeResponse{
		ID:       c.ID,
		Bits:     c.Bits,
		MinAgeMS: s.Puzzle.MinAge().Milliseconds(),
	})
}

type registerRequest struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	Challenge string `json:"challenge"`
	Solution  string `json:"solution"`
	Invite    string `json:"invite"`
	// Trap is a field the real form keeps empty and hidden; a filled one is a bot.
	Trap string `json:"website"`
}

// registerResponse carries the one-time recovery codes. They are shown once,
// at registration, and never again.
type registerResponse struct {
	Username      string   `json:"username"`
	RecoveryCodes []string `json:"recovery_codes"`
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := readJSON(w, r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	if req.Trap != "" {
		// A bot filled the hidden field. Answer as success without making one.
		writeError(w, http.StatusBadRequest, "invalid", "Registration failed.")
		return
	}

	switch s.Config.Registration {
	case config.RegistrationClosed:
		writeError(w, http.StatusForbidden, "registration_closed", s.registrationClosedMessage())
		return
	case config.RegistrationOpen:
		if msg := s.checkPuzzle(req); msg != "" {
			writeError(w, http.StatusForbidden, "pow", msg)
			return
		}
	}

	username, ok := normalizeUsername(req.Username)
	if !ok {
		writeError(w, http.StatusBadRequest, "username", "A name is 3 to 24 letters, digits, _ or -.")
		return
	}
	if n := len([]rune(req.Password)); n < minPassword || n > maxPassword {
		writeError(w, http.StatusBadRequest, "password", "A password is 8 to 128 characters.")
		return
	}

	if !s.allowRegistration(r) {
		writeError(w, http.StatusTooManyRequests, "rate", "Too many accounts from here lately. Try again later.")
		return
	}

	var inviteMAC []byte
	if s.Config.Registration == config.RegistrationInvite {
		mac, err := s.inviteMAC(req.Invite)
		if err != nil {
			writeError(w, http.StatusForbidden, "invite", "This instance needs a valid invite code.")
			return
		}
		inviteMAC = mac
	}

	hash, err := s.Hasher.Hash(r.Context(), req.Password)
	if err != nil {
		s.Logger.Error("hash password", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}

	codes, macs := s.newRecoveryCodes()
	token := auth.NewSessionToken()
	acct, err := s.Store.CreateAccount(r.Context(), store.NewAccount{
		Username:       username,
		PasswordHash:   hash,
		RecoveryMACs:   macs,
		InviteMAC:      inviteMAC,
		SessionHash:    auth.TokenHash(token),
		SessionExpires: s.Now().Add(sessionIdle),
	})
	switch {
	case errors.Is(err, store.ErrUsernameTaken):
		writeError(w, http.StatusConflict, "username_taken", "That name is taken.")
		return
	case errors.Is(err, store.ErrInviteInvalid):
		writeError(w, http.StatusForbidden, "invite", "This invite code is not valid.")
		return
	case err != nil:
		s.Logger.Error("create account", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}

	s.recordRegistration(r)
	s.setSessionCookie(w, token)
	s.Logger.Info("account created", "account", acct.ID)
	writeJSON(w, http.StatusOK, registerResponse{Username: acct.Username, RecoveryCodes: codes})
}

// checkPuzzle returns a message for the player, or "" when the solution was
// accepted.
func (s *Server) checkPuzzle(req registerRequest) string {
	err := s.Puzzle.Check(req.Challenge, req.Solution, s.Now())
	switch {
	case err == nil:
		return ""
	case errors.Is(err, auth.ErrSolvedTooFast):
		return "The form was sent too quickly. Please try again."
	case errors.Is(err, auth.ErrChallengeAbsent), errors.Is(err, auth.ErrChallengeOld):
		return "Your challenge expired. Please try again."
	default:
		return "The automated-client check failed. Please try again."
	}
}

func (s *Server) registrationClosedMessage() string {
	if s.Config.Registration == config.RegistrationInvite {
		return "This instance is invite-only."
	}
	return "Registration is closed on this instance."
}

// normalizeUsername checks a name and returns it trimmed. Names are compared
// without case by the store, so the stored spelling is the one the player chose.
func normalizeUsername(name string) (string, bool) {
	name = strings.TrimSpace(name)
	n := len([]rune(name))
	if n < minUsername || n > maxUsername {
		return "", false
	}
	for _, r := range name {
		if !(unicode.IsLetter(r) && r < unicode.MaxASCII) && !unicode.IsDigit(r) && r != '_' && r != '-' {
			return "", false
		}
	}
	return name, true
}

func (s *Server) newRecoveryCodes() (codes []string, macs [][]byte) {
	for i := 0; i < auth.RecoveryCodeCount; i++ {
		code := auth.NewCode()
		normalized, _ := auth.NormalizeCode(code)
		codes = append(codes, code)
		macs = append(macs, auth.CodeMAC(s.Config.Secret, auth.RecoveryCode, normalized))
	}
	return codes, macs
}

func (s *Server) inviteMAC(code string) ([]byte, error) {
	normalized, err := auth.NormalizeCode(code)
	if err != nil {
		return nil, err
	}
	return auth.CodeMAC(s.Config.Secret, auth.InviteCode, normalized), nil
}

func (s *Server) limitKey(r *http.Request) string {
	return auth.LimitKey(auth.ClientIP(r, s.Config.TrustedProxies))
}

func (s *Server) allowRegistration(r *http.Request) bool {
	key := s.limitKey(r)
	now := s.Now()
	return s.regHour.Allowed(key, now) && s.regDay.Allowed(key, now) && s.regGlobal.Allowed("", now)
}

func (s *Server) recordRegistration(r *http.Request) {
	key := s.limitKey(r)
	now := s.Now()
	s.regHour.Add(key, now)
	s.regDay.Add(key, now)
	s.regGlobal.Add("", now)
}

func writeBadRequest(w http.ResponseWriter, err error) {
	var bad badRequest
	if errors.As(err, &bad) {
		writeError(w, http.StatusBadRequest, "bad_request", bad.msg)
		return
	}
	writeError(w, http.StatusBadRequest, "bad_request", "The request was not understood.")
}
