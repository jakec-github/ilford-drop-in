package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// pingerFunc is a Pinger from a function.
type pingerFunc func(ctx context.Context) error

func (f pingerFunc) Ping(ctx context.Context) error { return f(ctx) }

var databaseUp = pingerFunc(func(context.Context) error { return nil })

// eastOrganisation and westOrganisation are the two drop-ins the server tests
// mount. Two, so every test that one is reachable at its address is also a
// test that the other's is not that one.
var (
	eastOrganisation = &config.Organisation{ID: "east", DisplayName: "East Drop-In", OrganiserEmails: []string{"east@example.com"}}
	westOrganisation = &config.Organisation{ID: "west", DisplayName: "West Drop-In", OrganiserEmails: []string{"west@example.com"}}
)

// newServer is the whole server as it is deployed: both Organisations mounted,
// each with its own store, plus the shared root.
func newServer(t *testing.T, health Pinger) (http.Handler, *Authenticator) {
	t.Helper()
	auth := newTestAuthenticator()
	east := NewHandler(serverStore(), testVolunteers(), eastOrganisation, auth, testFrontend, nil, nil, nil, zap.NewNop())
	west := NewHandler(&mockStore{}, testVolunteers(), westOrganisation, auth, testFrontend, nil, nil, nil, zap.NewNop())
	return Mount(auth, []*Handler{east, west}, health, zap.NewNop()), auth
}

func serverStore() *mockStore {
	return &mockStore{
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2026-01-11", RoleID: "role-team-lead", VolunteerID: "alice"},
		},
	}
}

// Everything an Organisation has is under its own path: its pages, its API, its
// calendars and its assets. The URLs that leave the app — bookmarks, emailed
// links, calendar subscriptions — are at the addresses they always were.
func TestServer_AnOrganisationIsServedUnderItsPath(t *testing.T) {
	server, _ := newServer(t, databaseUp)

	for path, want := range map[string]int{
		"/org/east/":                     http.StatusOK,
		"/org/east/organiser/allocation": http.StatusOK,
		"/org/east/availability/a-token": http.StatusOK,
		"/org/east/calendars/alice.ics":  http.StatusOK,
		"/org/east/chunk-abc.js":         http.StatusOK,
		"/org/east/api/shifts":           http.StatusOK,
		"/org/east/api/nonsense":         http.StatusNotFound,
		"/org/east/api/me":               http.StatusUnauthorized,
	} {
		rec := doRequest(t, server, http.MethodGet, path, "")
		assert.Equal(t, want, rec.Code, path)
	}

	// And none of it answers outside one.
	for _, path := range []string{
		"/organiser/allocation", "/availability/a-token", "/calendars/alice.ics", "/chunk-abc.js", "/api/shifts", "/auth/me",
	} {
		rec := doRequest(t, server, http.MethodGet, path, "")
		assert.Equal(t, http.StatusNotFound, rec.Code, path)
	}
}

// An id nobody configured is a missing page like any other, so which drop-ins a
// server runs cannot be found out by asking it.
func TestServer_AnUnknownOrganisationIsAPlainNotFound(t *testing.T) {
	server, _ := newServer(t, databaseUp)

	unknown := doRequest(t, server, http.MethodGet, "/org/north/", "")
	missing := doRequest(t, server, http.MethodGet, "/no-such-page", "")

	assert.Equal(t, http.StatusNotFound, unknown.Code)
	assert.Equal(t, missing.Body.String(), unknown.Body.String())
	assert.Equal(t, http.StatusNotFound, doRequest(t, server, http.MethodGet, "/org/north/api/shifts", "").Code)
}

// Each Organisation reads its own store, and calls itself by its own name.
func TestServer_EachOrganisationHasItsOwnData(t *testing.T) {
	server, _ := newServer(t, databaseUp)

	east := doRequest(t, server, http.MethodGet, "/org/east/calendars/alice.ics", "")
	west := doRequest(t, server, http.MethodGet, "/org/west/calendars/alice.ics", "")

	require.Equal(t, http.StatusOK, east.Code)
	require.Equal(t, http.StatusOK, west.Code)
	assert.Contains(t, east.Body.String(), "BEGIN:VEVENT")
	assert.NotContains(t, west.Body.String(), "BEGIN:VEVENT")
	assert.Contains(t, east.Body.String(), "East Drop-In")
	assert.Contains(t, west.Body.String(), "West Drop-In")
}

// One cookie, two drop-ins: being an Organiser of one is nothing at the other.
func TestServer_AnOrganiserOfOneIsNobodyAtAnother(t *testing.T) {
	server, auth := newServer(t, databaseUp)
	cookie := sessionCookieFor(auth, "east@example.com")

	assert.Equal(t, http.StatusOK, doRequest(t, server, http.MethodGet, "/org/east/api/me", "", cookie).Code)
	assert.Equal(t, http.StatusUnauthorized, doRequest(t, server, http.MethodGet, "/org/west/api/me", "", cookie).Code)
	assert.Equal(t, http.StatusUnauthorized, doRequest(t, server, http.MethodPost, "/org/west/api/sync", "", cookie).Code)
}

// The frontend learns whose site it is from the server rather than having it
// written into the page.
func TestServer_OrganisationSaysWhoItIs(t *testing.T) {
	server, _ := newServer(t, databaseUp)

	rec := doRequest(t, server, http.MethodGet, "/org/west/api/organisation", "")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"id":"west","displayName":"West Drop-In"}`, rec.Body.String())
}

// The root names no Organisation: an address is the only way in.
func TestServer_RootIsASplashThatLinksNowhere(t *testing.T) {
	server, _ := newServer(t, databaseUp)

	rec := doRequest(t, server, http.MethodGet, "/", "")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	body := rec.Body.String()
	assert.NotContains(t, body, "<a ")
	for _, org := range []*config.Organisation{eastOrganisation, westOrganisation} {
		assert.NotContains(t, body, org.ID)
		assert.NotContains(t, body, org.DisplayName)
	}
}

// /org/east and /org/east/ are the same page, and the second is the one every
// relative asset reference resolves against.
func TestServer_BareOrganisationPathRedirectsToItsRoot(t *testing.T) {
	server, _ := newServer(t, databaseUp)

	rec := doRequest(t, server, http.MethodGet, "/org/east", "")
	assert.Equal(t, http.StatusMovedPermanently, rec.Code)
	assert.Equal(t, "/org/east/", rec.Header().Get("Location"))
}

// The one token the server rewrites in index.html. It is what every relative
// asset reference and the router's own base resolve against, so a hard
// navigation to a nested route depends on it.
func TestServer_IndexCarriesTheOrganisationInItsBaseTag(t *testing.T) {
	server, _ := newServer(t, databaseUp)

	rec := doRequest(t, server, http.MethodGet, "/org/east/organiser/allocation", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `<base href="/org/east/" />`)
}

// The links a volunteer keeps — the calendar's link back to the rota, the
// emailed availability link — carry the Organisation's path, and the send's
// round trip ends on that Organisation's page.
func TestServer_LinksThatLeaveTheAppCarryTheOrganisation(t *testing.T) {
	server, _ := newServer(t, databaseUp)

	rec := doRequest(t, server, http.MethodGet, "/org/east/calendars/alice.ics", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "http://example.com/org/east")

	h := NewHandler(&mockStore{}, testVolunteers(), westOrganisation, newTestAuthenticator(), nil, nil, nil, nil, zap.NewNop())
	req := httptest.NewRequest(http.MethodGet, "/api/availability-rounds", nil)
	assert.Equal(t, "http://example.com/org/west/availability/a-token", h.availabilityLink(req, "a-token"))
	assert.Equal(t, "/org/west"+sendReturnPath, h.sendReturnURL())
}

// /auth is shared: the session cookie it sets covers every Organisation, which
// is what makes one sign-in work at each drop-in someone is on the lists of.
func TestServer_LoginCookiesCoverTheWholeSite(t *testing.T) {
	a := newTestAuthenticator()

	rec := httptest.NewRecorder()
	a.setSessionCookie(rec, testOrganiserEmail)
	assert.Equal(t, "/", cookieNamed(t, rec, sessionCookieName).Path)

	rec = httptest.NewRecorder()
	a.handleLogout(rec, httptest.NewRequest(http.MethodPost, "/auth/logout", nil))
	assert.Equal(t, "/", cookieNamed(t, rec, sessionCookieName).Path)
}

// A stack runner needs one URL that answers "the app is up" and nothing else.
// Each Organisation's SPA fallback makes every other path under it a 200, so
// this endpoint is the only honest readiness signal. It belongs to none of
// them, and needs no session.
func TestServer_Health(t *testing.T) {
	server, _ := newServer(t, databaseUp)

	rec := doRequest(t, server, http.MethodGet, "/health", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}

// Readiness means the app can serve, not that the process is alive: a server
// that cannot reach its database has nothing to show, so it must not report
// ready and let a runner proceed into failures.
func TestServer_HealthWhenTheDatabaseIsUnreachable(t *testing.T) {
	server, _ := newServer(t, pingerFunc(func(context.Context) error { return errors.New("connection refused") }))

	rec := doRequest(t, server, http.MethodGet, "/health", "")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.JSONEq(t, `{"status":"unavailable"}`, rec.Body.String())
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
