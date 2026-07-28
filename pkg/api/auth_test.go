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
		secret:      testSecret,
		adminEmails: map[string]struct{}{"admin@example.com": {}},
		logger:      zap.NewNop(),
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
		"  Admin@Example.com  ": "admin@example.com",
		// Gmail: googlemail alias, dots, and +tags all fold to one address.
		"jakechorley@googlemail.com":    "jakechorley@gmail.com",
		"jake.chorley@gmail.com":        "jakechorley@gmail.com",
		"jakechorley+admin@gmail.com":   "jakechorley@gmail.com",
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

func TestIsAdmin_FoldsGmailVariants(t *testing.T) {
	// Allowlist stores googlemail; a login as any equivalent Gmail form matches.
	a := &Authenticator{
		adminEmails: map[string]struct{}{normaliseEmail("jakechorley@googlemail.com"): {}},
		logger:      zap.NewNop(),
	}
	assert.True(t, a.isAdmin("jakechorley@gmail.com"))
	assert.True(t, a.isAdmin("jake.chorley@gmail.com"))
	assert.True(t, a.isAdmin("jakechorley+admin@googlemail.com"))
	assert.False(t, a.isAdmin("someoneelse@gmail.com"))
}

func TestIsAdmin_CaseInsensitive(t *testing.T) {
	a := testAuth()
	assert.True(t, a.isAdmin("admin@example.com"))
	assert.True(t, a.isAdmin("ADMIN@example.com"))
	assert.True(t, a.isAdmin("  Admin@Example.com  "))
	assert.False(t, a.isAdmin("someone@example.com"))
}

func TestAdminFromRequest_ValidSession(t *testing.T) {
	a := testAuth()
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req.AddCookie(sessionCookieFor(a, "admin@example.com"))

	email, ok := a.adminFromRequest(req)
	assert.True(t, ok)
	assert.Equal(t, "admin@example.com", email)
}

func TestAdminFromRequest_NoCookie(t *testing.T) {
	a := testAuth()
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)

	_, ok := a.adminFromRequest(req)
	assert.False(t, ok)
}

func TestAdminFromRequest_TamperedCookie(t *testing.T) {
	a := testAuth()
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "forged.value"})

	_, ok := a.adminFromRequest(req)
	assert.False(t, ok)
}

func TestAdminFromRequest_ValidCookieButNotOnAllowlist(t *testing.T) {
	a := testAuth()
	// A properly signed session for an email that is no longer an admin: the
	// cookie proves identity, but authority is re-checked against config.
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req.AddCookie(sessionCookieFor(a, "removed@example.com"))

	_, ok := a.adminFromRequest(req)
	assert.False(t, ok)
}

func TestRequireAdmin_AllowsAdmin(t *testing.T) {
	a := testAuth()
	called := false
	handler := a.requireAdmin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(sessionCookieFor(a, "admin@example.com"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRequireAdmin_RejectsNonAdmin(t *testing.T) {
	a := testAuth()
	handler := a.requireAdmin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("wrapped handler should not run")
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandleMe_LoggedIn(t *testing.T) {
	a := testAuth()
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req.AddCookie(sessionCookieFor(a, "admin@example.com"))
	rec := httptest.NewRecorder()

	a.handleMe(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"email":"admin@example.com"}`, rec.Body.String())
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
