package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// testBasePath stands in for the segment a deployment picks. The real value is
// a deployment's business and is not in this repo, so the tests name their own.
const testBasePath = "/rota"

// newBasePathHandler is the full stack — API, auth and frontend — served with a
// base path configured, which is how the deployed site runs.
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

// TestBasePathMovesWhatLeavesTheApp: the URLs that end up somewhere we cannot
// reach to correct them — a bookmark, an emailed link, a calendar subscription —
// move under the base path, because they have to be at their final address
// before anyone holds one.
func TestBasePathMovesWhatLeavesTheApp(t *testing.T) {
	handler := newBasePathHandler(basePathStore())

	for path, want := range map[string]int{
		testBasePath + "/":                     http.StatusOK,
		testBasePath + "/admin/allocation":     http.StatusOK,
		testBasePath + "/availability/a-token": http.StatusOK,
		testBasePath + "/calendars/alice.ics":  http.StatusOK,
		testBasePath + "/chunk-abc.js":         http.StatusOK,
	} {
		rec := doRequest(t, handler, http.MethodGet, path, "")
		assert.Equal(t, want, rec.Code, path)
	}

	// And none of them answers at the root any more.
	for _, path := range []string{
		"/admin/allocation", "/availability/a-token", "/calendars/alice.ics", "/chunk-abc.js",
	} {
		rec := doRequest(t, handler, http.MethodGet, path, "")
		assert.Equal(t, http.StatusNotFound, rec.Code, path)
	}
}

// TestApiAndAuthStayAtTheRoot: the URLs whose only client is the page's own
// JavaScript do not move, because that client ships with the server and can be
// pointed anywhere later. /auth is the stronger case of the two — its callback
// is registered by hand in the Google console, so one shared /auth/callback is
// a one-time step no matter how many organisations a server ends up serving.
func TestApiAndAuthStayAtTheRoot(t *testing.T) {
	handler := newBasePathHandler(basePathStore())

	for path, want := range map[string]int{
		"/api/shifts":   http.StatusOK,
		"/api/nonsense": http.StatusNotFound,
		"/auth/me":      http.StatusUnauthorized,
	} {
		rec := doRequest(t, handler, http.MethodGet, path, "")
		assert.Equal(t, want, rec.Code, path)
	}

	// Under the base path there is no API and no login: those paths are inside
	// the SPA's namespace now, so they get the app shell like any other client
	// route the router does not recognise. A client that prefixes its requests
	// gets HTML where it expected JSON, which is the loud failure.
	for _, path := range []string{
		testBasePath + "/api/shifts", testBasePath + "/auth/me",
	} {
		rec := doRequest(t, handler, http.MethodGet, path, "")
		require.Equal(t, http.StatusOK, rec.Code, path)
		assert.Contains(t, rec.Header().Get("Content-Type"), "text/html", path)
	}
}

// TestHealthStaysAtTheRootUnderABasePath: infrastructure rather than site. The
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
// once it is out — this one is emailed. It is minted by an API handler, which
// is not under the base path, so the path can only come from config.
func TestAvailabilityLinkCarriesTheBasePath(t *testing.T) {
	cfg := &config.Config{Server: &config.ServerConfig{BasePath: testBasePath}}
	h := NewHandler(basePathStore(), testVolunteers(), cfg, newTestAuthenticator(), nil, nil, zap.NewNop())
	req := httptest.NewRequest(http.MethodGet, "/api/availability-rounds", nil)

	assert.Equal(t, "http://example.com"+testBasePath+"/availability/a-token", h.availabilityLink(req, "a-token"))

	// And with no base path it is the URL it has always been.
	plain := NewHandler(basePathStore(), testVolunteers(), apiTestCfg, newTestAuthenticator(), nil, nil, zap.NewNop())
	assert.Equal(t, "http://example.com/availability/a-token", plain.availabilityLink(req, "a-token"))
}

// TestSendReturnsToTheAllocationTabUnderTheBasePath: the send's round trip ends
// on a page, and a page is under the path even though the /auth endpoint that
// redirects to it is not.
func TestSendReturnsToTheAllocationTabUnderTheBasePath(t *testing.T) {
	cfg := &config.Config{Server: &config.ServerConfig{BasePath: testBasePath}}
	h := NewHandler(basePathStore(), testVolunteers(), cfg, newTestAuthenticator(), nil, nil, zap.NewNop())

	assert.Equal(t, testBasePath+sendReturnPath, h.sendReturnURL())
}

// TestLoginCookiesStayAtTheRoot: /auth did not move, so neither did the scope
// of the cookies it sets. The session cookie covers the whole domain because
// the API it authenticates is at the root too.
func TestLoginCookiesStayAtTheRoot(t *testing.T) {
	a := newTestAuthenticator()
	a.basePath = testBasePath

	rec := httptest.NewRecorder()
	a.setSessionCookie(rec, testAdminEmail)
	assert.Equal(t, "/", cookieNamed(t, rec, sessionCookieName).Path)

	rec = httptest.NewRecorder()
	a.handleLogout(rec, httptest.NewRequest(http.MethodPost, "/auth/logout", nil))
	assert.Equal(t, "/", cookieNamed(t, rec, sessionCookieName).Path)
}

// TestLoginLandsOnTheSiteUnderTheBasePath: the one thing the Authenticator
// needs the base path for. The callback is at the root, but what it sends the
// browser to afterwards is the app's home page, which is not.
func TestLoginLandsOnTheSiteUnderTheBasePath(t *testing.T) {
	a := newTestAuthenticator()
	a.basePath = testBasePath
	assert.Equal(t, testBasePath+"/", a.siteRoot())

	plain := newTestAuthenticator()
	assert.Equal(t, "/", plain.siteRoot())
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
