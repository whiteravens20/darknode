// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package server

import (
	"io/fs"
	"net/http"
	"os"
	"strings"
	"sync"
)

// spa serves the built web client from dir. A request for a file that is
// there is answered with it; anything else gets index.html, so the client's
// own routes survive a reload.
//
// The directory is opened as an os.Root, which refuses to open anything
// outside itself, symbolic links included. The request path therefore never
// becomes a path on the filesystem by hand.
func spa(dir string) http.Handler {
	open := sync.OnceValues(func() (*os.Root, error) { return os.OpenRoot(dir) })

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		root, err := open()
		if err != nil {
			http.Error(w, "the client is not available", http.StatusInternalServerError)
			return
		}

		name := strings.TrimPrefix(r.URL.Path, "/")
		// The hashed files of a build never change under their name, so they
		// may be cached for a long time; index.html must not be.
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}

		if serveFile(w, r, root, name) {
			return
		}
		if !serveFile(w, r, root, "index.html") {
			http.Error(w, "the client is not available", http.StatusInternalServerError)
		}
	})
}

// serveFile answers with the named file of root and reports whether it did.
func serveFile(w http.ResponseWriter, r *http.Request, root *os.Root, name string) bool {
	if !fs.ValidPath(name) {
		return false
	}
	info, err := root.Stat(name)
	if err != nil || info.IsDir() {
		return false
	}
	file, err := root.Open(name)
	if err != nil {
		return false
	}
	defer file.Close()

	// ServeContent sets the type from the name and answers a range request.
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
	return true
}
