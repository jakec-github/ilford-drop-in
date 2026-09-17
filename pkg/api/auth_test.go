package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func testAuth() *Authenticator {
	return &Authenticator{
		secret:           testSecret,
		organiserEmails:  map[string]struct{}{"organiser@example.com": {}},
		rotaEditorEmails: map[string]struct{}{"editor@example.com": {}},
		logger:           zap.NewNop(),
	}
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
	a := &Authenticator{
		organiserEmails: map[string]struct{}{normaliseEmail("jakechorley@googlemail.com"): {}},
		logger:          zap.NewNop(),
	}
	for _, email := range []string{"jakechorley@gmail.com", "jake.chorley@gmail.com", "jakechorley+x@googlemail.com"} {
		level, ok := a.levelOf(email)
		assert.True(t, ok, email)
		assert.Equal(t, LevelOrganiser, level, email)
	}
	_, ok := a.levelOf("someoneelse@gmail.com")
	assert.False(t, ok)
}

func TestLevelOf_CaseInsensitive(t *testing.T) {
	a := testAuth()
	for _, email := range []string{"organiser@example.com", "ORGANISER@example.com", "  Organiser@Example.com  "} {
		level, ok := a.levelOf(email)
		assert.True(t, ok, email)
		assert.Equal(t, LevelOrganiser, level, email)
	}
	_, ok := a.levelOf("someone@example.com")
	assert.False(t, ok)
}

func TestLevelOf_EachAllowlistGivesItsLevel(t *testing.T) {
	a := testAuth()

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
	a := testAuth()
	a.rotaEditorEmails[normaliseEmail("organiser@example.com")] = struct{}{}

	level, ok := a.levelOf("organiser@example.com")
	assert.True(t, ok)
	assert.Equal(t, LevelOrganiser, level)
}

func TestSessionFromRequest_ValidSession(t *testing.T) {
	a := testAuth()
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req.AddCookie(sessionCookieFor(a, "editor@example.com"))

	session, ok := a.sessionFromRequest(req)
	assert.True(t, ok)
	assert.Equal(t, "editor@example.com", session.Email)
	assert.Equal(t, LevelRotaEditor, session.Level)
}

func TestSessionFromRequest_NoCookie(t *testing.T) {
	a := testAuth()
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)

	_, ok := a.sessionFromRequest(req)
	assert.False(t, ok)
}

func TestSessionFromRequest_TamperedCookie(t *testing.T) {
	a := testAuth()
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "forged.value"})

	_, ok := a.sessionFromRequest(req)
	assert.False(t, ok)
}

func TestSessionFromRequest_ValidCookieButNotOnAllowlist(t *testing.T) {
	a := testAuth()
	// A properly signed session for an email on neither list any more: the
	// cookie proves identity, but authority is re-checked against config.
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req.AddCookie(sessionCookieFor(a, "removed@example.com"))

	_, ok := a.sessionFromRequest(req)
	assert.False(t, ok)
}

func TestRequireLevel(t *testing.T) {
	cases := []struct {
		name   string
		min    Level
		email  string // empty means no session
		status int
	}{
		{"organiser route, organiser", LevelOrganiser, "organiser@example.com", http.StatusOK},
		{"organiser route, rota editor", LevelOrganiser, "editor@example.com", http.StatusForbidden},
		{"organiser route, nobody", LevelOrganiser, "", http.StatusUnauthorized},
		{"organiser route, off both lists", LevelOrganiser, "removed@example.com", http.StatusUnauthorized},
		{"rota editor route, organiser", LevelRotaEditor, "organiser@example.com", http.StatusOK},
		{"rota editor route, rota editor", LevelRotaEditor, "editor@example.com", http.StatusOK},
		{"rota editor route, nobody", LevelRotaEditor, "", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := testAuth()
			var got string
			handler := a.requireLevel(tc.min, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	for email, level := range map[string]string{
		"organiser@example.com": "organiser",
		"editor@example.com":    "rotaEditor",
	} {
		req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
		req.AddCookie(sessionCookieFor(a, email))
		rec := httptest.NewRecorder()

		a.handleMe(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"email":"`+email+`","level":"`+level+`"}`, rec.Body.String())
	}
}

func TestHandleMe_NotLoggedIn(t *testing.T) {
	a := testAuth()
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	rec := httptest.NewRecorder()

	a.handleMe(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
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
