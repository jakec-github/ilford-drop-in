package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// requestFrom builds a request to the server as a browser would send it from
// a page at the given Sec-Fetch-Site, signed in as east's Organiser.
func requestFrom(method, target, body, fetchSite string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(sessionCookie("east@example.com"))
	if fetchSite != "" {
		req.Header.Set("Sec-Fetch-Site", fetchSite)
	}
	return req
}

func serve(server http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	return rec
}

// A write from a page on another origin is refused, even though the browser
// sends the Organiser's cookie with it — which it does from a sibling host
// such as preprod's, since SameSite counts that as the same site.
func TestHarden_AWriteFromAnotherOriginIsRefused(t *testing.T) {
	server, _ := newServer(t, databaseUp)
	body := `{"name":"Cook","priority":1,"colour":"teal"}`

	for _, site := range []string{"cross-site", "same-site"} {
		rec := serve(server, requestFrom(http.MethodPost, "/org/east/api/roles", body, site))
		assert.Equal(t, http.StatusForbidden, rec.Code, site)
	}

	// An older browser sends no Sec-Fetch-Site; its Origin says the same thing.
	req := requestFrom(http.MethodPost, "http://example.org/org/east/api/roles", body, "")
	req.Header.Set("Origin", "https://preprod.example.org")
	assert.Equal(t, http.StatusForbidden, serve(server, req).Code)

	rec := serve(server, requestFrom(http.MethodPost, "/org/east/api/roles", body, "same-origin"))
	assert.NotEqual(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

// Starting a send mails every volunteer, so another site must not be able to
// submit the form that does it.
func TestHarden_ASendCannotBeStartedFromAnotherOrigin(t *testing.T) {
	server, _ := newServer(t, databaseUp)

	req := requestFrom(http.MethodPost, "/org/east/auth/gmail", "mode=round&deadline=Friday", "cross-site")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	assert.Equal(t, http.StatusForbidden, serve(server, req).Code)
}

// Reading is not a write: the rota and a volunteer's calendar feed are read
// from anywhere — a calendar app is not on this origin.
func TestHarden_AReadFromAnotherOriginIsServed(t *testing.T) {
	server, _ := newServer(t, databaseUp)

	for _, path := range []string{"/org/east/api/shifts", "/org/east/calendars/alice.ics"} {
		rec := serve(server, requestFrom(http.MethodGet, path, "", "cross-site"))
		assert.Equal(t, http.StatusOK, rec.Code, path)
	}
}

// The volunteer's form is public and reads its body before it knows whether
// the link is real, so the body is capped for everyone.
func TestHarden_AnOversizedBodyIsRefused(t *testing.T) {
	server, _ := newServer(t, databaseUp)
	huge := `{"shiftIds":["` + strings.Repeat("x", maxRequestBody) + `"]}`

	rec := serve(server, requestFrom(http.MethodPost, "/org/east/api/availability/a-token", huge, ""))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "too large")
}

func TestHarden_EveryResponseCarriesTheSecurityHeaders(t *testing.T) {
	server, _ := newServer(t, databaseUp)

	for _, path := range []string{"/", "/org/east/", "/org/east/api/shifts", "/org/nowhere/"} {
		rec := doRequest(t, server, http.MethodGet, path, "")
		header := rec.Header()
		assert.Contains(t, header.Get("Content-Security-Policy"), "frame-ancestors 'none'", path)
		assert.Equal(t, "nosniff", header.Get("X-Content-Type-Options"), path)
		assert.Equal(t, "DENY", header.Get("X-Frame-Options"), path)
		assert.Equal(t, "no-referrer", header.Get("Referrer-Policy"), path)
		// The test server is not hosted: dev runs over plain HTTP.
		assert.Empty(t, header.Get("Strict-Transport-Security"), path)
	}
}

// HSTS is a promise that the site is HTTPS, so only a hosted server makes it.
func TestHarden_AHostedServerPromisesHTTPS(t *testing.T) {
	auth := newTestAuthenticator()
	auth.secure = true
	east := NewHandler(serverStore(), testVolunteers(), eastOrganisation, auth, testFrontend, nil, nil, nil, zap.NewNop())
	server := Mount(auth, []*Handler{east}, testFrontend, databaseUp, zap.NewNop())

	rec := doRequest(t, server, http.MethodGet, "/org/east/", "")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "max-age=31536000", rec.Header().Get("Strict-Transport-Security"))
}
