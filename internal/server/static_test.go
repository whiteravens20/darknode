package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The client's files are served from one directory and nothing outside it,
// however the request path is written.
func TestStaticStaysInsideItsDirectory(t *testing.T) {
	dir := t.TempDir()
	web := filepath.Join(dir, "web")
	if err := os.MkdirAll(filepath.Join(web, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, body string) {
		if err := os.WriteFile(filepath.Join(web, path), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.html", "the page")
	write("assets/app.js", "the script")
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("not for anyone"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "secret.txt"), filepath.Join(web, "link.txt")); err != nil {
		t.Fatal(err)
	}

	h := spa(web)
	get := func(target string) (int, string) {
		r := httptest.NewRequest(http.MethodGet, target, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}

	if code, body := get("/assets/app.js"); code != 200 || body != "the script" {
		t.Errorf("a real file: %d %q", code, body)
	}
	if _, body := get("/"); body != "the page" {
		t.Errorf("the root: %q", body)
	}
	// A route of the client's own is answered with the page, not a 404.
	if code, body := get("/some/client/route"); code != 200 || body != "the page" {
		t.Errorf("a client route: %d %q", code, body)
	}
	// Nothing outside the directory, by any spelling or through a link.
	for _, target := range []string{
		"/../secret.txt",
		"/..%2fsecret.txt",
		"/assets/../../secret.txt",
		"/link.txt",
	} {
		code, body := get(target)
		if body == "not for anyone" {
			t.Errorf("%s reached a file outside the directory (%d)", target, code)
		}
	}
}

func TestStaticCaching(t *testing.T) {
	web := t.TempDir()
	if err := os.MkdirAll(filepath.Join(web, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{"index.html": "page", "assets/app.js": "script"} {
		if err := os.WriteFile(filepath.Join(web, path), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h := spa(web)
	header := func(target string) string {
		r := httptest.NewRequest(http.MethodGet, target, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Header().Get("Cache-Control")
	}
	if got := header("/assets/app.js"); got != "public, max-age=31536000, immutable" {
		t.Errorf("a built asset: %q", got)
	}
	if got := header("/"); got != "no-cache" {
		t.Errorf("the page: %q", got)
	}
}

func TestForLogIsSafe(t *testing.T) {
	cases := map[string]string{
		"/api/cmd":                "/api/cmd",
		"/a\nFAKE level=ERROR":    "/a.FAKE level=ERROR",
		"/b\r\ninjected":          "/b..injected",
		"/c\x00\x7f":              "/c..",
		strings.Repeat("/x", 200): strings.Repeat("/x", 60) + "…",
	}
	for in, want := range cases {
		if got := forLog(in); got != want {
			t.Errorf("forLog(%q) = %q, want %q", in, got, want)
		}
	}
}
