package api

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// syncTestHandler is an Organisation's routes with syncFn as its roster sync.
// No OAuth config is needed — sync runs on the server's own service account.
func syncTestHandler(syncFn VolunteerSyncFunc) http.Handler {
	return NewHandler(&mockStore{}, testVolunteers(), apiTestCfg, newTestAuthenticator(), nil, nil, nil, syncFn, zap.NewNop()).Routes()
}

func TestSync_RequiresAnOrganiser(t *testing.T) {
	called := false
	syncFn := VolunteerSyncFunc(func(context.Context) error {
		called = true
		return nil
	})

	rec := doRequest(t, syncTestHandler(syncFn), http.MethodPost, "/api/sync", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "syncing without an Organiser session must be rejected")
	assert.False(t, called, "sync must not run without a verified Organiser session")
}

func TestSync_Success(t *testing.T) {
	called := false
	syncFn := VolunteerSyncFunc(func(context.Context) error {
		called = true
		return nil
	})

	rec := doRequest(t, syncTestHandler(syncFn), http.MethodPost, "/api/sync", "", organiserCookie())
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.True(t, called, "an Organiser sync must run the sync function")
}

func TestSync_Failure(t *testing.T) {
	syncFn := VolunteerSyncFunc(func(context.Context) error {
		return errors.New("sheets access denied")
	})

	rec := doRequest(t, syncTestHandler(syncFn), http.MethodPost, "/api/sync", "", organiserCookie())
	assert.Equal(t, http.StatusBadGateway, rec.Code, "a failed sheet fetch must surface as an upstream error")
}

func TestSync_NotConfigured(t *testing.T) {
	var syncFn VolunteerSyncFunc

	rec := doRequest(t, syncTestHandler(syncFn), http.MethodPost, "/api/sync", "", organiserCookie())
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code, "with no sync function wired the endpoint is unavailable")
}

func TestSync_RejectsGet(t *testing.T) {
	syncFn := VolunteerSyncFunc(func(context.Context) error { return nil })

	rec := doRequest(t, syncTestHandler(syncFn), http.MethodGet, "/api/sync", "", organiserCookie())
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code, "sync mutates state, so only POST is allowed")
}
