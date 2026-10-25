package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/jakechorley/ilford-drop-in/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// testAccess is one Organisation's allowlists: one person at each level.
func testAccess() access {
	return accessFor(&config.Organisation{
		OrganiserEmails:  []string{"organiser@example.com"},
		RotaEditorEmails: []string{"editor@example.com"},
	})
}

// testAuth is an Authenticator with no Organisation registered on it yet.
func testAuth() *Authenticator {
	return newTestAuthenticator()
}

// testOrgHandler builds a Handler for org on a, which registers org with it.
func testOrgHandler(a *Authenticator, org *config.Organisation) *Handler {
	return NewHandler(&mockStore{}, testVolunteers(), org, a, nil, nil, nil, nil, zap.NewNop())
}

// sessionCookieFor builds a valid session cookie for email.
func sessionCookieFor(a *Authenticator, email string) *http.Cookie {
	return &http.Cookie{
		Name:  sessionCookieName,
		Value: signSession(a.secret, email, time.Now().Add(time.Hour)),
	}
}

func TestNormaliseEmail(t *testing.T) {
	cases := map[string]string{
		// Case and whitespace.
		"  Organiser@Example.com  ": "organiser@example.com",
		// Gmail: googlemail alias, dots, and +tags all fold to one address.
		"jakechorley@googlemail.com":    "jakechorley@gmail.com",
		"jake.chorley@gmail.com":        "jakechorley@gmail.com",
		"jakechorley+rota@gmail.com":    "jakechorley@gmail.com",
		"Jake.Chorley+x@googlemail.com": "jakechorley@gmail.com",
		// Non-Gmail domains: dots and +tags are significant, left untouched.
		"j.smith@company.com":    "j.smith@company.com",
		"jsmith+ops@company.com": "jsmith+ops@company.com",
		// No @ — returned lowercased/trimmed as-is.
		"not-an-email": "not-an-email",
	}
	for in, want := range cases {
		assert.Equal(t, want, normaliseEmail(in), "normaliseEmail(%q)", in)
	}
}

func TestLevelOf_FoldsGmailVariants(t *testing.T) {
	// Allowlist stores googlemail; a login as any equivalent Gmail form matches.
	a := accessFor(&config.Organisation{OrganiserEmails: []string{"jakechorley@googlemail.com"}})
	for _, email := range []string{"jakechorley@gmail.com", "jake.chorley@gmail.com", "jakechorley+x@googlemail.com"} {
		level, ok := a.levelOf(email)
		assert.True(t, ok, email)
		assert.Equal(t, LevelOrganiser, level, email)
	}
	_, ok := a.levelOf("someoneelse@gmail.com")
	assert.False(t, ok)
}

func TestLevelOf_CaseInsensitive(t *testing.T) {
	a := testAccess()
	for _, email := range []string{"organiser@example.com", "ORGANISER@example.com", "  Organiser@Example.com  "} {
		level, ok := a.levelOf(email)
		assert.True(t, ok, email)
		assert.Equal(t, LevelOrganiser, level, email)
	}
	_, ok := a.levelOf("someone@example.com")
	assert.False(t, ok)
}

func TestLevelOf_EachAllowlistGivesItsLevel(t *testing.T) {
	a := testAccess()

	level, ok := a.levelOf("editor@example.com")
	assert.True(t, ok)
	assert.Equal(t, LevelRotaEditor, level)

	level, ok = a.levelOf("organiser@example.com")
	assert.True(t, ok)
	assert.Equal(t, LevelOrganiser, level)
}

// Someone on both lists can do everything either list allows, which is
// everything an Organiser can: the more senior level wins.
func TestLevelOf_OnBothListsIsAnOrganiser(t *testing.T) {
	a := testAccess()
	a.rotaEditors[normaliseEmail("organiser@example.com")] = struct{}{}

	level, ok := a.levelOf("organiser@example.com")
	assert.True(t, ok)
	assert.Equal(t, LevelOrganiser, level)
}

func TestSession_ValidSession(t *testing.T) {
	a := testAuth()
	h := testOrgHandler(a, apiTestCfg)
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(sessionCookieFor(a, testRotaEditorEmail))

	session, ok := h.session(req)
	assert.True(t, ok)
	assert.Equal(t, testRotaEditorEmail, session.Email)
	assert.Equal(t, LevelRotaEditor, session.Level)
}

func TestSession_NoCookie(t *testing.T) {
	h := testOrgHandler(testAuth(), apiTestCfg)
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)

	_, ok := h.session(req)
	assert.False(t, ok)
}

func TestSession_TamperedCookie(t *testing.T) {
	h := testOrgHandler(testAuth(), apiTestCfg)
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "forged.value"})

	_, ok := h.session(req)
	assert.False(t, ok)
}

func TestSession_ValidCookieButNotOnAllowlist(t *testing.T) {
	a := testAuth()
	h := testOrgHandler(a, apiTestCfg)
	// A properly signed session for an email on neither list any more: the
	// cookie proves identity, but authority is re-checked against config.
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(sessionCookieFor(a, "removed@example.com"))

	_, ok := h.session(req)
	assert.False(t, ok)
}

// A session is who someone is, everywhere; what they may do is each
// Organisation's own business. An Organiser of one drop-in is nobody at
// another, on the same cookie.
func TestSession_AllowlistsAreEachOrganisations(t *testing.T) {
	a := testAuth()
	east := testOrgHandler(a, &config.Organisation{ID: "east", OrganiserEmails: []string{"east@example.com"}})
	west := testOrgHandler(a, &config.Organisation{ID: "west", OrganiserEmails: []string{"west@example.com"}, RotaEditorEmails: []string{"east@example.com"}})
	south := testOrgHandler(a, &config.Organisation{ID: "south", OrganiserEmails: []string{"south@example.com"}})

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(sessionCookieFor(a, "east@example.com"))

	session, ok := east.session(req)
	require.True(t, ok)
	assert.Equal(t, LevelOrganiser, session.Level)

	session, ok = west.session(req)
	require.True(t, ok)
	assert.Equal(t, LevelRotaEditor, session.Level, "the level is the one this Organisation gives")

	_, ok = south.session(req)
	assert.False(t, ok, "being signed in for one drop-in says nothing about another")
}

func TestRequireLevel(t *testing.T) {
	cases := []struct {
		name   string
		min    Level
		email  string // empty means no session
		status int
	}{
		{"organiser route, organiser", LevelOrganiser, testOrganiserEmail, http.StatusOK},
		{"organiser route, rota editor", LevelOrganiser, testRotaEditorEmail, http.StatusForbidden},
		{"organiser route, nobody", LevelOrganiser, "", http.StatusUnauthorized},
		{"organiser route, off both lists", LevelOrganiser, "removed@example.com", http.StatusUnauthorized},
		{"rota editor route, organiser", LevelRotaEditor, testOrganiserEmail, http.StatusOK},
		{"rota editor route, rota editor", LevelRotaEditor, testRotaEditorEmail, http.StatusOK},
		{"rota editor route, nobody", LevelRotaEditor, "", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := testAuth()
			h := testOrgHandler(a, apiTestCfg)
			var got string
			handler := h.requireLevel(tc.min, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = sessionEmail(r.Context())
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if tc.email != "" {
				req.AddCookie(sessionCookieFor(a, tc.email))
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assert.Equal(t, tc.status, rec.Code)
			if tc.status == http.StatusOK {
				assert.Equal(t, tc.email, got, "the handler should see who is asking")
			} else {
				assert.Empty(t, got, "the wrapped handler should not run")
			}
		})
	}
}

func TestHandleMe_LoggedIn(t *testing.T) {
	a := testAuth()
	handler := testOrgHandler(a, apiTestCfg).Routes()
	for email, level := range map[string]string{
		testOrganiserEmail:  "organiser",
		testRotaEditorEmail: "rotaEditor",
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		req.AddCookie(sessionCookieFor(a, email))
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"email":"`+email+`","level":"`+level+`"}`, rec.Body.String())
	}
}

func TestHandleMe_NotLoggedIn(t *testing.T) {
	handler := testOrgHandler(testAuth(), apiTestCfg).Routes()
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// Login is shared, so an Organisation's own login link says which one to come
// back to — and passes along anything else it was asked, like dev mode's level.
func TestOrganisationLoginNamesItself(t *testing.T) {
	handler := testOrgHandler(testAuth(), apiTestCfg).Routes()
	req := httptest.NewRequest(http.MethodGet, "/auth/login?level=rotaEditor", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)
	location, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "/auth/login", location.Path)
	assert.Equal(t, "test-org", location.Query().Get("org"))
	assert.Equal(t, "rotaEditor", location.Query().Get("level"))
}

// returnTo keeps the redirect on this site and never says whether an id is
// configured: anything shaped like one goes to its path, where an unknown one
// meets the same 404 as any missing page.
func TestReturnTo(t *testing.T) {
	assert.Equal(t, "/org/test-org/", returnTo("test-org"))
	assert.Equal(t, "/org/not-configured/", returnTo("not-configured"))
	for _, bad := range []string{"", "//evil.example", "../x", "Test-Org", "a/b", "-x"} {
		assert.Equal(t, "/", returnTo(bad), "%q", bad)
	}
}

// Login asks only whether someone may act for some Organisation.
func TestOnAnyAllowlist(t *testing.T) {
	a := testAuth()
	testOrgHandler(a, &config.Organisation{ID: "east", OrganiserEmails: []string{"east@example.com"}})
	testOrgHandler(a, &config.Organisation{ID: "west", OrganiserEmails: []string{"west@example.com"}, RotaEditorEmails: []string{"editor@example.com"}})

	assert.True(t, a.onAnyAllowlist("east@example.com"))
	assert.True(t, a.onAnyAllowlist("EDITOR@example.com"))
	assert.False(t, a.onAnyAllowlist("nobody@example.com"))
}

func TestHandleLogout_ClearsCookie(t *testing.T) {
	a := testAuth()
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	rec := httptest.NewRecorder()

	a.handleLogout(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	var cleared *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			cleared = c
		}
	}
	require.NotNil(t, cleared)
	assert.Equal(t, "", cleared.Value)
	assert.True(t, cleared.MaxAge < 0)
}

func TestResolveRedirectURI(t *testing.T) {
	uris := []string{
		"https://dropin.example.org/auth/callback",
		"http://localhost:5173/auth/callback",
		"http://localhost:5175/auth/callback",
	}

	t.Run("picks by locality when no preference is given", func(t *testing.T) {
		got, err := resolveRedirectURI(uris, "test", "")
		require.NoError(t, err)
		assert.Equal(t, "http://localhost:5173/auth/callback", got)

		got, err = resolveRedirectURI(uris, "prod", "")
		require.NoError(t, err)
		assert.Equal(t, "https://dropin.example.org/auth/callback", got)

		got, err = resolveRedirectURI(uris, "preprod", "")
		require.NoError(t, err)
		assert.Equal(t, "https://dropin.example.org/auth/callback", got)
	})

	t.Run("falls back to the first URI when none matches the wanted locality", func(t *testing.T) {
		got, err := resolveRedirectURI([]string{"http://localhost:5173/auth/callback"}, "prod", "")
		require.NoError(t, err)
		assert.Equal(t, "http://localhost:5173/auth/callback", got)
	})

	t.Run("errors when no URIs are registered", func(t *testing.T) {
		_, err := resolveRedirectURI(nil, "test", "")
		require.Error(t, err)
	})

	// A worktree serves the frontend on its own port, so it needs to name the
	// registered callback that matches it rather than take the first localhost one.
	t.Run("honours a preferred URI that is registered", func(t *testing.T) {
		got, err := resolveRedirectURI(uris, "test", "http://localhost:5175/auth/callback")
		require.NoError(t, err)
		assert.Equal(t, "http://localhost:5175/auth/callback", got)
	})

	t.Run("rejects a preferred URI that is not registered", func(t *testing.T) {
		_, err := resolveRedirectURI(uris, "test", "http://localhost:9999/auth/callback")
		require.Error(t, err)
		// The message should point at what is actually registered, since the fix
		// is either to register the URI or to correct the config.
		assert.Contains(t, err.Error(), "http://localhost:5173/auth/callback")
	})

}
