package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

var testFrontend = fstest.MapFS{
	// The base tag is the one token frontendHandler rewrites, so the stand-in
	// build carries it exactly as web/index.html does.
	"index.html":   {Data: []byte(`<html><head>` + baseTag + `</head>app</html>`)},
	"chunk-abc.js": {Data: []byte("console.log(1)")},
}

// testIndex is what the stand-in build's index.html serves as at the root.
const testIndex = `<html><head>` + baseTag + `</head>app</html>`

func TestFrontendHandlerServesFiles(t *testing.T) {
	handler := frontendHandler(testFrontend, "")

	tests := []struct {
		name     string
		path     string
		wantBody string
	}{
		{"existing file", "/chunk-abc.js", "console.log(1)"},
		{"root serves index", "/", testIndex},
		{"unknown path falls back to index", "/shifts/2026-02-02", testIndex},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			body, _ := io.ReadAll(rec.Body)
			if string(body) != tt.wantBody {
				t.Errorf("body = %q, want %q", body, tt.wantBody)
			}
		})
	}
}

func TestHasFrontend(t *testing.T) {
	if hasFrontend(nil) {
		t.Error("hasFrontend(nil) = true, want false")
	}
	if hasFrontend(fstest.MapFS{".gitkeep": {}}) {
		t.Error("hasFrontend(placeholder-only) = true, want false")
	}
	if !hasFrontend(testFrontend) {
		t.Error("hasFrontend(build) = false, want true")
	}
}
