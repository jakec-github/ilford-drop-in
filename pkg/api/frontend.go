package api

import (
	"io/fs"
	"net/http"
	"strings"
)

// frontendHandler serves the embedded frontend build with an SPA fallback:
// paths that match a file in the build are served as-is; anything else gets
// index.html so client-side routes resolve on hard navigation.
//
// It is registered as the mux's catch-all without a method pattern (see
// Routes), so it enforces the method itself: a page is only ever fetched.
func frontendHandler(frontend fs.FS) http.Handler {
	fileServer := http.FileServerFS(frontend)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if f, err := frontend.Open(path); err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		http.ServeFileFS(w, r, frontend, "index.html")
	})
}

// hasFrontend reports whether the filesystem holds a usable frontend build.
// Dev builds embed an empty dist/ placeholder, in which case the server stays
// API-only.
func hasFrontend(frontend fs.FS) bool {
	if frontend == nil {
		return false
	}
	_, err := fs.Stat(frontend, "index.html")
	return err == nil
}
