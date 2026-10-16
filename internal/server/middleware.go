// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package server

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

// The page may load scripts, styles, workers and data only from its own
// origin. Nothing is fetched from a third party, fonts included.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; " +
	"img-src 'self' data:; font-src 'self'; connect-src 'self'; worker-src 'self'; " +
	"object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// sameOrigin rejects an unsafe-method API request whose browser-set Origin
// names a different site. The session cookie is SameSite=Lax, and this is the
// second layer: a request a foreign page makes to the API is turned away.
// Requests with no Origin header (same-origin GETs, curl, tests) pass; only a
// present, mismatched Origin is rejected.
func (s *Server) sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		origin := r.Header.Get("Origin")
		if origin != "" && !sameHost(origin, r) {
			writeError(w, http.StatusForbidden, "bad_origin", "Request origin not allowed.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sameHost reports whether origin names the host the request was sent to.
func sameHost(origin string, r *http.Request) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v) // the server's own signal to drop the connection
				}
				s.Logger.Error("handler panic",
					"method", forLog(r.Method), "path", forLog(r.URL.Path), "panic", v)
				writeError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// maxLoggedLength is how much of a value taken from a request is written to
// the log.
const maxLoggedLength = 120

// forLog makes a value from a request safe to write to the log: control
// characters become dots, so nothing can forge a line however the log is
// formatted, and the length is capped, so a long request cannot flood it.
func forLog(s string) string {
	if len(s) > maxLoggedLength {
		s = s[:maxLoggedLength] + "…"
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '.'
		}
		return r
	}, s)
}

// maxBodyBytes is the largest request body the API reads.
const maxBodyBytes = 64 * 1024

// readJSON decodes a JSON request body into dst. It requires the JSON content
// type, limits the size, and refuses unknown fields and trailing data.
func readJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return errBadRequest("The request body must be JSON.")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return errBadRequest("The request body is too large.")
		}
		return errBadRequest("The request body is not valid JSON.")
	}
	if dec.More() {
		return errBadRequest("The request body has trailing data.")
	}
	return nil
}

type badRequest struct{ msg string }

func (e badRequest) Error() string { return e.msg }

func errBadRequest(msg string) error { return badRequest{msg: msg} }

// apiError is the JSON body of every failed API response.
type apiError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body) // a client that left mid-write needs no answer
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, apiError{Error: code, Message: message})
}
