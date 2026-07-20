package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

var testFrontend = fstest.MapFS{
	"index.html":   {Data: []byte("<html>app</html>")},
	"chunk-abc.js": {Data: []byte("console.log(1)")},
}

func TestFrontendHandlerServesFiles(t *testing.T) {
	handler := frontendHandler(testFrontend)

	tests := []struct {
		name     string
		path     string
		wantBody string
	}{
		{"existing file", "/chunk-abc.js", "console.log(1)"},
		{"root serves index", "/", "<html>app</html>"},
		{"unknown path falls back to index", "/rota/2026-02-02", "<html>app</html>"},
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
