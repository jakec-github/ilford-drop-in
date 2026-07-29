package api

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A stack runner needs one URL that answers "the app is up" and nothing else.
// The SPA fallback makes every other path a 200, so this endpoint is the only
// honest readiness signal.
func TestHealthEndpoint_Ready(t *testing.T) {
	rec := doRequest(t, newTestHandler(&mockStore{}, testVolunteers()), http.MethodGet, "/health", "")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}

// Readiness means the app can serve, not that the process is alive: a server
// that cannot reach its database has nothing to show, so it must not report
// ready and let a runner proceed into failures.
func TestHealthEndpoint_DatabaseUnreachable(t *testing.T) {
	store := &mockStore{pingErr: errors.New("connection refused")}
	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/health", "")

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.JSONEq(t, `{"status":"unavailable"}`, rec.Body.String())
}

// Health is polled by scripts and, in prod, by whatever watches the container.
// Neither carries a session.
func TestHealthEndpoint_NeedsNoSession(t *testing.T) {
	rec := doRequest(t, newTestHandler(&mockStore{}, testVolunteers()), http.MethodGet, "/health", "")

	assert.NotEqual(t, http.StatusUnauthorized, rec.Code)
}
