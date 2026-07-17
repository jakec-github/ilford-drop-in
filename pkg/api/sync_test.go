package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
)

// newSyncTestAuthenticator builds an Authenticator wired for the sync paths: a
// real oauth2Config whose endpoints point wherever the test needs (a stub token
// server for the exchange), plus the injected sync function. The OIDC verifier
// stays nil — sync never touches the login branch.
func newSyncTestAuthenticator(tokenURL, authURL string, syncFn VolunteerSyncFunc) *Authenticator {
	return &Authenticator{
		oauth2Config: &oauth2.Config{
			ClientID:     "test-client",
			ClientSecret: "test-secret",
			Endpoint:     oauth2.Endpoint{AuthURL: authURL, TokenURL: tokenURL},
			RedirectURL:  "http://localhost:5173/auth/callback",
			Scopes:       []string{"openid", "email", "profile"},
		},
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

// stubTokenServer returns an OAuth token endpoint that hands back a fixed access
// token, standing in for Google's code exchange.
func stubTokenServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"fake-token","token_type":"Bearer","expires_in":3600}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func syncStateCookie(value string) *http.Cookie {
	return &http.Cookie{Name: syncStateCookieName, Value: value}
}

func TestSyncStart_RequiresAdmin(t *testing.T) {
	a := newSyncTestAuthenticator("", "https://accounts.google.com/o/oauth2/auth", nil)

	rec := doRequest(t, syncTestHandler(a), http.MethodGet, "/auth/sync", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "starting a sync without an admin session must be rejected")
}

func TestSyncStart_RedirectsWithIncrementalSheetsScope(t *testing.T) {
	a := newSyncTestAuthenticator("", "https://accounts.google.com/o/oauth2/auth", nil)

	rec := doRequest(t, syncTestHandler(a), http.MethodGet, "/auth/sync", "", adminCookie())
	require.Equal(t, http.StatusFound, rec.Code)

	loc := rec.Header().Get("Location")
	assert.Contains(t, loc, "spreadsheets.readonly", "sync must request the Sheets scope")
	assert.Contains(t, loc, "include_granted_scopes=true", "sync must ask Google to merge, not replace, existing grants")
	assert.Contains(t, rec.Header().Get("Set-Cookie"), syncStateCookieName, "a sync state cookie marks the return trip")
}

func TestSyncCallback_Success(t *testing.T) {
	tokenSrv := stubTokenServer(t)

	var gotToken *oauth2.Token
	syncFn := func(_ *http.Request, token *oauth2.Token) error {
		gotToken = token
		return nil
	}
	a := newSyncTestAuthenticator(tokenSrv.URL, "", syncFn)

	rec := doRequest(t, syncTestHandler(a), http.MethodGet,
		"/auth/callback?state=state123&code=abc", "", syncStateCookie("state123"), adminCookie())

	require.Equal(t, http.StatusFound, rec.Code, rec.Body.String())
	assert.Equal(t, "/admin?synced=1", rec.Header().Get("Location"))
	require.NotNil(t, gotToken, "the exchanged token must reach the sync function")
	assert.Equal(t, "fake-token", gotToken.AccessToken)
}

func TestSyncCallback_RequiresAdminSession(t *testing.T) {
	called := false
	syncFn := func(_ *http.Request, _ *oauth2.Token) error {
		called = true
		return nil
	}
	a := newSyncTestAuthenticator("", "", syncFn)

	// Valid sync state cookie but no admin session.
	rec := doRequest(t, syncTestHandler(a), http.MethodGet,
		"/auth/callback?state=state123&code=abc", "", syncStateCookie("state123"))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.False(t, called, "sync must not run without a verified admin session")
}

func TestSyncCallback_SyncFailureRedirectsError(t *testing.T) {
	tokenSrv := stubTokenServer(t)
	syncFn := func(_ *http.Request, _ *oauth2.Token) error {
		return errors.New("sheets access denied")
	}
	a := newSyncTestAuthenticator(tokenSrv.URL, "", syncFn)

	rec := doRequest(t, syncTestHandler(a), http.MethodGet,
		"/auth/callback?state=state123&code=abc", "", syncStateCookie("state123"), adminCookie())

	require.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "/admin?synced=error", rec.Header().Get("Location"), "a failed sync must report the error to the dashboard")
}

// TestSyncCallback_StateMismatchFallsToLogin proves the callback only treats a
// request as a sync when the sync state cookie matches; otherwise it is a login
// (which here has no valid login state and is rejected as such).
func TestSyncCallback_StateMismatchFallsToLogin(t *testing.T) {
	called := false
	syncFn := func(_ *http.Request, _ *oauth2.Token) error {
		called = true
		return nil
	}
	a := newSyncTestAuthenticator("", "", syncFn)

	rec := doRequest(t, syncTestHandler(a), http.MethodGet,
		"/auth/callback?state=state123&code=abc", "", syncStateCookie("different"), adminCookie())

	assert.Equal(t, http.StatusBadRequest, rec.Code, "a non-matching sync cookie is not a sync; login rejects the bad state")
	assert.False(t, called)
}
