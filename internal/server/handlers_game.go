// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/whiteravens20/darknode/internal/game"
)

type commandRequest struct {
	Line string `json:"line"`
}

type commandResponse struct {
	Lines []game.Line `json:"lines"`
}

// command runs one terminal line and answers with its output. When the line
// changed the player's state, every stream of that account is told to send a
// fresh snapshot.
func (s *Server) command(w http.ResponseWriter, r *http.Request, sc sessionContext) {
	var req commandRequest
	if err := readJSON(w, r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	result, err := s.Game.Execute(r.Context(), sc.player, req.Line)
	if err != nil {
		s.Logger.Error("run command", "account", sc.player.AccountID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	if result.Changed {
		s.hub.Notify(sc.player.AccountID)
	}
	lines := result.Lines
	if lines == nil {
		lines = []game.Line{}
	}
	writeJSON(w, http.StatusOK, commandResponse{Lines: lines})
}

// streamPing is how often a comment is sent to keep the connection open
// through proxies that close an idle one.
const streamPing = 25 * time.Second

// stream sends the player's state as server-sent events: a snapshot when the
// stream opens, and another whenever the state changes.
func (s *Server) stream(w http.ResponseWriter, r *http.Request, sc sessionContext) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "Streaming is not available.")
		return
	}
	sub, err := s.hub.Subscribe(sc.player.AccountID, sc.tokenHash)
	if errors.Is(err, ErrTooManyStreams) {
		writeError(w, http.StatusTooManyRequests, "too_many_streams", "Too many open windows. Close one and reload.")
		return
	}
	if err != nil {
		s.Logger.Error("subscribe", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	defer s.hub.Unsubscribe(sub)

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("Connection", "keep-alive")
	// Tell nginx not to buffer this response, which would hold back events.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	send := func() bool {
		snap, err := s.Game.Snapshot(r.Context(), sc.player)
		if err != nil {
			s.Logger.Error("build snapshot", "account", sc.player.AccountID, "err", err)
			return false
		}
		body, err := json.Marshal(snap)
		if err != nil {
			s.Logger.Error("encode snapshot", "err", err)
			return false
		}
		if _, err := fmt.Fprintf(w, "event: state\ndata: %s\n\n", body); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	if !send() {
		return
	}
	ping := time.NewTicker(streamPing)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-sub.closed:
			// The session ended elsewhere: sign-out, or recovery.
			fmt.Fprint(w, "event: ended\ndata: {}\n\n")
			flusher.Flush()
			return
		case <-sub.changed:
			if !send() {
				return
			}
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
