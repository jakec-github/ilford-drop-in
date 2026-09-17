package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"golang.org/x/oauth2"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// testBasePath stands in for the segment a deployment picks. The real value is
// a deployment's business and is not in this repo, so the tests name their own.
const testBasePath = "/rota"

// newBasePathHandler is the full stack — API, auth and frontend — served under
// a base path, which is how the deployed site runs.
func newBasePathHandler(store *mockStore) http.Handler {
	cfg := &config.Config{Server: &config.ServerConfig{BasePath: testBasePath}}
	auth := newTestAuthenticator()
	auth.basePath = testBasePath
	return NewHandler(store, testVolunteers(), cfg, auth, testFrontend, nil, zap.NewNop()).Routes()
}

func basePathStore() *mockStore {
	return &mockStore{
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2026-01-11", Role: "Team lead", VolunteerID: "alice"},
		},
	}
}

// TestBasePathMovesTheWholeSite: every namespace moves together. Anything that
// answered at the root and still does is a link that will break the day the
// site moves again.
func TestBasePathMovesTheWholeSite(t *testing.T) {
	handler := newBasePathHandler(basePathStore())

	// The API, the OAuth endpoints, the calendar feeds and the SPA's own
	// routes, all under the base path.
	for path, want := range map[string]int{
		testBasePath + "/api/shifts":           http.StatusOK,
		testBasePath + "/api/nonsense":         http.StatusNotFound,
		testBasePath + "/auth/me":              http.StatusUnauthorized,
		testBasePath + "/calendars/alice.ics":  http.StatusOK,
		testBasePath + "/":                     http.StatusOK,
		testBasePath + "/admin/allocation":     http.StatusOK,
		testBasePath + "/availability/a-token": http.StatusOK,
	} {
		rec := doRequest(t, handler, http.MethodGet, path, "")
		assert.Equal(t, want, rec.Code, path)
	}

	// And nothing at the root, which is where they all used to be.
	for _, path := range []string{
		"/api/shifts", "/auth/me", "/calendars/alice.ics", "/admin/allocation", "/chunk-abc.js",
	} {
		rec := doRequest(t, handler, http.MethodGet, path, "")
		assert.Equal(t, http.StatusNotFound, rec.Code, path)
	}
}

// TestHealthStaysAtTheRootUnderABasePath: the one deliberate exception. The
// deploy workflow, scripts/deploy-config.sh and scripts/dev-stack.sh all poll
// it, and none of them knows the path.
func TestHealthStaysAtTheRootUnderABasePath(t *testing.T) {
	handler := newBasePathHandler(basePathStore())

	rec := doRequest(t, handler, http.MethodGet, "/health", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}

// TestRootRedirectsToTheBasePath: the root of the domain belongs to nobody, but
// it should still land a visitor somewhere rather than 404.
func TestRootRedirectsToTheBasePath(t *testing.T) {
	handler := newBasePathHandler(basePathStore())

	rec := doRequest(t, handler, http.MethodGet, "/", "")
	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, testBasePath+"/", rec.Header().Get("Location"))
}

// TestBareBasePathRedirectsToItsRoot: /rota and /rota/ are the same page, and
// the second is the one every relative asset reference resolves against.
func TestBareBasePathRedirectsToItsRoot(t *testing.T) {
	handler := newBasePathHandler(basePathStore())

	rec := doRequest(t, handler, http.MethodGet, testBasePath, "")
	assert.Equal(t, http.StatusMovedPermanently, rec.Code)
	assert.Equal(t, testBasePath+"/", rec.Header().Get("Location"))
}

// TestIndexCarriesTheBasePathInItsBaseTag: the one token the server rewrites.
// It is what every relative asset reference and the router's own base resolve
// against, so a hard navigation to a nested route depends on it.
func TestIndexCarriesTheBasePathInItsBaseTag(t *testing.T) {
	store := basePathStore()

	handler := newBasePathHandler(store)
	rec := doRequest(t, handler, http.MethodGet, testBasePath+"/admin/allocation", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `<base href="`+testBasePath+`/" />`)

	// With no base path the tag is already right, and index.html is served
	// byte for byte as it was built.
	rec = doRequest(t, newFullStackHandler(store), http.MethodGet, "/admin/allocation", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `<base href="/" />`)
}

// TestCalendarFeedCarriesTheBasePath: the URL in a calendar event points at the
// rota. Both the feed's own URL and this one live in a volunteer's calendar app
// once they subscribe, out of the app's reach — which is the whole reason the
// path had to land before anyone had subscribed.
func TestCalendarFeedCarriesTheBasePath(t *testing.T) {
	handler := newBasePathHandler(basePathStore())

	rec := doRequest(t, handler, http.MethodGet, testBasePath+"/calendars/alice.ics", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "http://example.com"+testBasePath)
}

// TestAvailabilityLinkCarriesTheBasePath: the other link the app cannot reach
// once it is out — this one is emailed.
func TestAvailabilityLinkCarriesTheBasePath(t *testing.T) {
	cfg := &config.Config{Server: &config.ServerConfig{BasePath: testBasePath}}
	h := NewHandler(basePathStore(), testVolunteers(), cfg, newTestAuthenticator(), nil, nil, zap.NewNop())
	req := httptest.NewRequest(http.MethodGet, "/api/availability-rounds", nil)

	assert.Equal(t, "http://example.com"+testBasePath+"/availability/a-token", h.availabilityLink(req, "a-token"))

	// And with no base path it is the URL it has always been.
	plain := NewHandler(basePathStore(), testVolunteers(), apiTestCfg, newTestAuthenticator(), nil, nil, zap.NewNop())
	assert.Equal(t, "http://example.com/availability/a-token", plain.availabilityLink(req, "a-token"))
}

// TestStateCookieIsScopedToTheOAuthEndpointsUnderTheBasePath: the sharpest of
// the path mistakes. The browser simply does not send a cookie scoped to /auth
// back to <base>/auth/callback, and the callback can only report that as an
// invalid state.
func TestStateCookieIsScopedToTheOAuthEndpointsUnderTheBasePath(t *testing.T) {
	a := newTestAuthenticator()
	a.basePath = testBasePath
	a.oauth2Config = &oauth2.Config{
		ClientID:    "client",
		Endpoint:    oauth2.Endpoint{AuthURL: "https://accounts.google.com/o/oauth2/auth"},
		RedirectURL: "https://dropin.example.org" + testBasePath + "/auth/callback",
	}

	rec := httptest.NewRecorder()
	a.handleLogin(rec, httptest.NewRequest(http.MethodGet, testBasePath+"/auth/login", nil))

	require.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, testBasePath+"/auth", cookieNamed(t, rec, stateCookieName).Path)
}

// TestLoginCookiesAreScopedToTheBasePath: both cookies are scoped by path, and
// a scope that does not cover the site is a cookie the browser never sends
// back. The state cookie is the sharper of the two — without it every login
// fails at the callback with "invalid OAuth state", which says nothing about a
// path.
func TestLoginCookiesAreScopedToTheBasePath(t *testing.T) {
	a := newTestAuthenticator()
	a.basePath = testBasePath

	rec := httptest.NewRecorder()
	a.setSessionCookie(rec, testAdminEmail)
	session := cookieNamed(t, rec, sessionCookieName)
	assert.Equal(t, testBasePath+"/", session.Path)

	// Logging out has to clear it at the path it was set at, or the browser
	// keeps the old cookie alongside the expired one.
	rec = httptest.NewRecorder()
	a.handleLogout(rec, httptest.NewRequest(http.MethodPost, "/auth/logout", nil))
	assert.Equal(t, testBasePath+"/", cookieNamed(t, rec, sessionCookieName).Path)
}

func cookieNamed(t *testing.T, rec *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %q cookie was set", name)
	return nil
}
