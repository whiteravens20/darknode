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
// changed the player's state, every stream of that account is sent the run's
// new state; the answer carries the words, the stream carries the state.
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
		s.publishRun(r, sc.player)
	}

	lines := result.Lines
	if lines == nil {
		lines = []game.Line{}
	}
	writeJSON(w, http.StatusOK, commandResponse{Lines: lines})
}

// publishRun sends the run's state to every stream of the player. When the
// state cannot be read, the streams are told to open again instead: a browser
// showing a state the server has moved past is worse than a reconnect.
func (s *Server) publishRun(r *http.Request, p game.Player) {
	run, err := s.Game.RunState(r.Context(), p)
	if err != nil {
		s.Logger.Error("build run state", "account", p.AccountID, "err", err)
		s.hub.Publish(p.AccountID, game.DeltaResync, game.ResyncDelta{})
		return
	}
	s.hub.Publish(p.AccountID, game.DeltaRun, game.RunDelta{Run: run})
}

// streamPing is how often a comment is sent to keep the connection open
// through proxies that close an idle one.
const streamPing = 25 * time.Second

// stream sends the player's state as server-sent events: a snapshot when the
// stream opens, and a delta for everything that happens afterwards. Each
// event is numbered, so a browser that misses one can tell.
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

	// An open stream is what being online means, and online time is what the
	// game judges a run by, so the simulation is told either way.
	s.engine.Join(sc.player.AccountID)
	defer s.engine.Leave(sc.player.AccountID)

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("Connection", "keep-alive")
	// Tell nginx not to buffer this response, which would hold back events.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	write := func(kind string, payload any) bool {
		body, err := json.Marshal(payload)
		if err != nil {
			s.Logger.Error("encode delta", "kind", kind, "err", err)
			return false
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, body); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	// The snapshot is number one of this stream's sequence; everything the
	// hub queued while it was being built comes after it.
	snap, err := s.Game.Snapshot(r.Context(), sc.player)
	if err != nil {
		s.Logger.Error("build snapshot", "account", sc.player.AccountID, "err", err)
		return
	}
	snap.Seq = 1
	if !write(game.DeltaSnapshot, snap) {
		return
	}

	ping := time.NewTicker(streamPing)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-sub.closed:
			// The session ended elsewhere: a sign-out, or a recovery.
			write(game.DeltaEnded, struct{}{})
			return
		case ev := <-sub.events:
			if !write(ev.kind, ev.delta) {
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
