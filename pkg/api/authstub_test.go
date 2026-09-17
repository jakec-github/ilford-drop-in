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
		Port:             8080,
		SessionSecret:    string(testSecret),
		OrganiserEmails:  []string{"organiser@example.com"},
		RotaEditorEmails: []string{"editor@example.com"},
	}
}

func testDevModeConfig() *config.DevModeConfig {
	return &config.DevModeConfig{
		OrganiserEmail:  "organiser@example.com",
		RotaEditorEmail: "editor@example.com",
		VolunteersCSV:   "test_data/volunteers.csv",
	}
}

func testStubAuth(t *testing.T) *Authenticator {
	t.Helper()
	a, err := NewStubAuthenticator(testDevModeConfig(), testServerConfig(), zap.NewNop(), nil)
	require.NoError(t, err)
	return a
}

// stubLogin logs in through the stub and returns the /auth/me body the session
// it minted answers with.
func stubLogin(t *testing.T, a *Authenticator, target string) string {
	t.Helper()
	mux := http.NewServeMux()
	a.registerRoutes(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))

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

	// The minted cookie is a real session: it satisfies the same gate every
	// protected route uses.
	me := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	me.AddCookie(session)
	meRec := httptest.NewRecorder()
	mux.ServeHTTP(meRec, me)

	require.Equal(t, http.StatusOK, meRec.Code)
	return meRec.Body.String()
}

// The stub's whole purpose: a session with no Google round-trip. Login must be
// a single request that comes back with a cookie the rest of the app accepts.
// Plain login is an Organiser, who can reach every screen.
func TestStubAuthenticator_LoginMintsOrganiserSession(t *testing.T) {
	body := stubLogin(t, testStubAuth(t), "/auth/login")
	assert.JSONEq(t, `{"email":"organiser@example.com","level":"organiser"}`, body)
}

// The dev stack can sign in at either level, so the Rota Editor's narrower UI
// can be driven as well as the Organiser's.
func TestStubAuthenticator_LoginAsRotaEditor(t *testing.T) {
	body := stubLogin(t, testStubAuth(t), "/auth/login?level=rotaEditor")
	assert.JSONEq(t, `{"email":"editor@example.com","level":"rotaEditor"}`, body)
}

func TestStubAuthenticator_LoginRefusesAnUnknownLevel(t *testing.T) {
	a := testStubAuth(t)
	mux := http.NewServeMux()
	a.registerRoutes(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/login?level=admin", nil))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, rec.Result().Cookies())
}

// A dev config naming no Rota Editor still boots; asking to be one says why it
// cannot rather than quietly signing in as an Organiser.
func TestStubAuthenticator_LoginAsRotaEditorWithNoneConfigured(t *testing.T) {
	dev := testDevModeConfig()
	dev.RotaEditorEmail = ""
	a, err := NewStubAuthenticator(dev, testServerConfig(), zap.NewNop(), nil)
	require.NoError(t, err)

	mux := http.NewServeMux()
	a.registerRoutes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/login?level=rotaEditor", nil))

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "rotaEditorEmail")
	assert.Empty(t, rec.Result().Cookies())
}

// Nothing about the stub weakens the gate itself: an unauthenticated request is
// still refused, so a snapshot of the logged-out UI is honest.
func TestStubAuthenticator_StillRejectsSessionlessRequests(t *testing.T) {
	a := testStubAuth(t)
	rec := httptest.NewRecorder()
	a.requireLevel(LevelRotaEditor, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
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

// A session for an address the allowlist does not hold at the level it is meant
// for carries the wrong authority, so the server would boot into a login that
// silently does the wrong thing. Fail loudly at construction instead.
func TestNewStubAuthenticator_RequiresEachEmailAtItsLevel(t *testing.T) {
	cases := map[string]struct {
		dev  func(*config.DevModeConfig)
		want []string
	}{
		"organiser off every list": {
			dev:  func(d *config.DevModeConfig) { d.OrganiserEmail = "stranger@example.com" },
			want: []string{"stranger@example.com", "organiserEmails"},
		},
		"organiser only a rota editor": {
			dev:  func(d *config.DevModeConfig) { d.OrganiserEmail = "editor@example.com" },
			want: []string{"editor@example.com", "organiserEmails"},
		},
		"rota editor off every list": {
			dev:  func(d *config.DevModeConfig) { d.RotaEditorEmail = "stranger@example.com" },
			want: []string{"stranger@example.com", "rotaEditorEmails"},
		},
		// An Organiser would sign in with more than a Rota Editor may do, and
		// the screens being checked would not be the Rota Editor's.
		"rota editor actually an organiser": {
			dev:  func(d *config.DevModeConfig) { d.RotaEditorEmail = "organiser@example.com" },
			want: []string{"organiser@example.com", "rotaEditorEmails"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dev := testDevModeConfig()
			tc.dev(dev)
			_, err := NewStubAuthenticator(dev, testServerConfig(), zap.NewNop(), nil)

			require.Error(t, err)
			for _, want := range tc.want {
				assert.Contains(t, err.Error(), want)
			}
		})
	}
}

// The allowlist folds Gmail-equivalent addresses; the stub email is checked
// against the same folding, so the two configs cannot disagree over a dot.
func TestNewStubAuthenticator_FoldsGmailVariants(t *testing.T) {
	srv := testServerConfig()
	srv.OrganiserEmails = []string{"jake.chorley@googlemail.com"}
	dev := testDevModeConfig()
	dev.OrganiserEmail = "jakechorley@gmail.com"

	a, err := NewStubAuthenticator(dev, srv, zap.NewNop(), nil)

	require.NoError(t, err)
	level, ok := a.levelOf("jakechorley@gmail.com")
	assert.True(t, ok)
	assert.Equal(t, LevelOrganiser, level)
}

// The deprecated adminEmails key still names Organisers for one release, so a
// deploy that has not yet rewritten its config does not lock everybody out.
func TestAuthenticator_ReadsAdminEmailsAsOrganisers(t *testing.T) {
	srv := testServerConfig()
	srv.OrganiserEmails = nil
	srv.AdminEmails = []string{"organiser@example.com"}

	a, err := NewStubAuthenticator(testDevModeConfig(), srv, zap.NewNop(), nil)

	require.NoError(t, err)
	level, ok := a.levelOf("organiser@example.com")
	assert.True(t, ok)
	assert.Equal(t, LevelOrganiser, level)
}
