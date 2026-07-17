package api

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// newSyncTestAuthenticator builds an Authenticator wired for the sync path: the
// admin allowlist and session secret used by adminCookie, plus the injected
// sync function. No OAuth config is needed — sync no longer runs an OAuth
// round-trip.
func newSyncTestAuthenticator(syncFn VolunteerSyncFunc) *Authenticator {
	return &Authenticator{
		secret:         testSecret,
		adminEmails:    map[string]struct{}{testAdminEmail: {}},
		logger:         zap.NewNop(),
		syncVolunteers: syncFn,
	}
}

func syncTestHandler(a *Authenticator) http.Handler {
	mux := http.NewServeMux()
	a.registerRoutes(mux)
	return mux
}

func TestSync_RequiresAdmin(t *testing.T) {
	called := false
	a := newSyncTestAuthenticator(func(context.Context) error {
		called = true
		return nil
	})

	rec := doRequest(t, syncTestHandler(a), http.MethodPost, "/auth/sync", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "syncing without an admin session must be rejected")
	assert.False(t, called, "sync must not run without a verified admin session")
}

func TestSync_Success(t *testing.T) {
	called := false
	a := newSyncTestAuthenticator(func(context.Context) error {
		called = true
		return nil
	})

	rec := doRequest(t, syncTestHandler(a), http.MethodPost, "/auth/sync", "", adminCookie())
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.True(t, called, "an admin sync must run the sync function")
}

func TestSync_Failure(t *testing.T) {
	a := newSyncTestAuthenticator(func(context.Context) error {
		return errors.New("sheets access denied")
	})

	rec := doRequest(t, syncTestHandler(a), http.MethodPost, "/auth/sync", "", adminCookie())
	assert.Equal(t, http.StatusBadGateway, rec.Code, "a failed sheet fetch must surface as an upstream error")
}

func TestSync_NotConfigured(t *testing.T) {
	a := newSyncTestAuthenticator(nil)

	rec := doRequest(t, syncTestHandler(a), http.MethodPost, "/auth/sync", "", adminCookie())
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code, "with no sync function wired the endpoint is unavailable")
}

func TestSync_RejectsGet(t *testing.T) {
	a := newSyncTestAuthenticator(func(context.Context) error { return nil })

	rec := doRequest(t, syncTestHandler(a), http.MethodGet, "/auth/sync", "", adminCookie())
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code, "sync mutates state, so only POST is allowed")
}
