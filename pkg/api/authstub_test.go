package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
)

func testServerConfig() *config.ServerConfig {
	return &config.ServerConfig{
		Port:          8080,
		SessionSecret: string(testSecret),
		AdminEmails:   []string{"admin@example.com"},
	}
}

func testStubAuth(t *testing.T) *Authenticator {
	t.Helper()
	a, err := NewStubAuthenticator(&config.DevModeConfig{
		AdminEmail:    "admin@example.com",
		VolunteersCSV: "test_data/volunteers.csv",
	}, testServerConfig(), zap.NewNop(), nil)
	require.NoError(t, err)
	return a
}

// The stub's whole purpose: a session with no Google round-trip. Login must be
// a single request that comes back with a cookie the rest of the app accepts.
func TestStubAuthenticator_LoginMintsAdminSession(t *testing.T) {
	a := testStubAuth(t)
	mux := http.NewServeMux()
	a.registerRoutes(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/login", nil))

	require.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "/", rec.Header().Get("Location"), "stub login must not leave the app")

	var session *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			session = c
		}
	}
	require.NotNil(t, session, "login should set a session cookie")
	assert.True(t, session.HttpOnly)
	assert.False(t, session.Secure, "dev runs over plain HTTP; a Secure cookie would never come back")

	// The minted cookie is a real session: it satisfies the same admin gate
	// every protected route uses.
	me := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	me.AddCookie(session)
	meRec := httptest.NewRecorder()
	mux.ServeHTTP(meRec, me)

	require.Equal(t, http.StatusOK, meRec.Code)
	assert.JSONEq(t, `{"email":"admin@example.com"}`, meRec.Body.String())
}

// Nothing about the stub weakens the gate itself: an unauthenticated request is
// still refused, so a snapshot of the logged-out UI is honest.
func TestStubAuthenticator_StillRejectsSessionlessRequests(t *testing.T) {
	a := testStubAuth(t)
	rec := httptest.NewRecorder()
	a.requireAdmin(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("wrapped handler should not run")
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/protected", nil))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// The OAuth callback has no exchange to perform in stub mode. It must say so
// rather than dereference the OIDC verifier it was never given.
func TestStubAuthenticator_CallbackUnavailable(t *testing.T) {
	a := testStubAuth(t)
	mux := http.NewServeMux()
	a.registerRoutes(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/callback?code=x&state=y", nil))

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// A session for an address the allowlist does not hold carries no authority, so
// the server would boot into a login that silently does nothing. Fail loudly at
// construction instead.
func TestNewStubAuthenticator_RequiresAnAllowlistedEmail(t *testing.T) {
	_, err := NewStubAuthenticator(&config.DevModeConfig{
		AdminEmail:    "stranger@example.com",
		VolunteersCSV: "test_data/volunteers.csv",
	}, testServerConfig(), zap.NewNop(), nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "stranger@example.com")
	assert.Contains(t, err.Error(), "adminEmails")
}

// The allowlist folds Gmail-equivalent addresses; the stub email is checked
// against the same folding, so the two configs cannot disagree over a dot.
func TestNewStubAuthenticator_FoldsGmailVariants(t *testing.T) {
	srv := testServerConfig()
	srv.AdminEmails = []string{"jake.chorley@googlemail.com"}

	a, err := NewStubAuthenticator(&config.DevModeConfig{
		AdminEmail:    "jakechorley@gmail.com",
		VolunteersCSV: "test_data/volunteers.csv",
	}, srv, zap.NewNop(), nil)

	require.NoError(t, err)
	assert.True(t, a.isAdmin("jakechorley@gmail.com"))
}
