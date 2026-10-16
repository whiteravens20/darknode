// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package server

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// spa serves the built web client from dir. A request for a file that exists
// is served from disk; anything else gets index.html, so the client's own
// routes work on a reload.
func spa(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	index := filepath.Join(dir, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := path.Clean("/" + r.URL.Path)
		name := filepath.Join(dir, filepath.FromSlash(clean))

		// The hashed files of a build never change under their name, so they
		// may be cached for a long time; index.html must not be.
		if strings.HasPrefix(clean, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}

		if info, err := os.Stat(name); err == nil && !info.IsDir() {
			files.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, index)
	})
}
