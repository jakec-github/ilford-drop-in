package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"go.uber.org/zap"
	"golang.org/x/oauth2"

	"github.com/jakechorley/ilford-drop-in/internal/config"
)

const (
	// googleIssuer is the OIDC issuer used for provider discovery and ID-token verification.
	googleIssuer = "https://accounts.google.com"

	sessionCookieName = "session"
	stateCookieName   = "oauth_state"
	// stateCookieMaxAge bounds how long a login attempt may sit at Google's consent screen.
	stateCookieMaxAge = 10 * time.Minute
)

// Authenticator handles the OIDC login flow and admin session cookies. It proves
// identity via a signed cookie and re-checks the admin allowlist from config on
// every request, so the cookie carries identity, not authority.
type Authenticator struct {
	oauth2Config *oauth2.Config
	verifier     *oidc.IDTokenVerifier
	secret       []byte
	adminEmails  map[string]struct{} // lowercased allowlist
	secure       bool                // set the cookie Secure flag (prod only)
	logger       *zap.Logger
}

// NewAuthenticator builds an Authenticator. It performs OIDC provider discovery
// against Google, so it makes a network call and can fail.
func NewAuthenticator(ctx context.Context, webCfg *config.OAuthClientWebConfig, srv *config.ServerConfig, env string, logger *zap.Logger) (*Authenticator, error) {
	provider, err := oidc.NewProvider(ctx, googleIssuer)
	if err != nil {
		return nil, fmt.Errorf("failed to discover OIDC provider: %w", err)
	}

	redirectURL := pickRedirectURI(webCfg.Web.RedirectURIs, env)
	if redirectURL == "" {
		return nil, fmt.Errorf("no redirect URI configured for env %q", env)
	}

	oauth2Config := &oauth2.Config{
		ClientID:     webCfg.Web.ClientID,
		ClientSecret: webCfg.Web.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  redirectURL,
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
	}

	admin := make(map[string]struct{}, len(srv.AdminEmails))
	for _, e := range srv.AdminEmails {
		admin[normaliseEmail(e)] = struct{}{}
	}

	return &Authenticator{
		oauth2Config: oauth2Config,
		verifier:     provider.Verifier(&oidc.Config{ClientID: webCfg.Web.ClientID}),
		secret:       []byte(srv.SessionSecret),
		adminEmails:  admin,
		secure:       env == "prod",
		logger:       logger,
	}, nil
}

// registerRoutes attaches the /auth endpoints to mux.
func (a *Authenticator) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/login", a.handleLogin)
	mux.HandleFunc("GET /auth/callback", a.handleCallback)
	mux.HandleFunc("POST /auth/logout", a.handleLogout)
	mux.HandleFunc("GET /auth/me", a.handleMe)
}

// handleLogin starts the OIDC flow: stash a random state in a short-lived cookie
// and redirect to Google's consent screen for identity scopes only.
func (a *Authenticator) handleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := randomToken()
	if err != nil {
		a.logger.Error("Failed to generate OAuth state", zap.Error(err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/auth",
		MaxAge:   int(stateCookieMaxAge.Seconds()),
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, a.oauth2Config.AuthCodeURL(state), http.StatusFound)
}

// handleCallback completes the flow: verify state, exchange the code, verify the
// ID token, check the allowlist, and set the session cookie. Non-admins are
// rejected here with no cookie set.
func (a *Authenticator) handleCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil || stateCookie.Value == "" || stateCookie.Value != r.URL.Query().Get("state") {
		http.Error(w, "invalid OAuth state", http.StatusBadRequest)
		return
	}
	// The state is single-use; clear it regardless of what follows.
	a.clearCookie(w, stateCookieName, "/auth")

	oauth2Token, err := a.oauth2Config.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		a.logger.Warn("OAuth code exchange failed", zap.Error(err))
		http.Error(w, "authentication failed", http.StatusBadRequest)
		return
	}

	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		a.logger.Error("OAuth response missing id_token")
		http.Error(w, "authentication failed", http.StatusInternalServerError)
		return
	}

	idToken, err := a.verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		a.logger.Warn("ID token verification failed", zap.Error(err))
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}

	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := idToken.Claims(&claims); err != nil {
		a.logger.Error("Failed to parse ID token claims", zap.Error(err))
		http.Error(w, "authentication failed", http.StatusInternalServerError)
		return
	}

	if !claims.EmailVerified || !a.isAdmin(claims.Email) {
		// Not an admin: no session is created. A session existing means admin.
		a.logger.Warn("Rejected non-admin login",
			zap.String("email", claims.Email),
			zap.Bool("email_verified", claims.EmailVerified))
		http.Error(w, "not authorised", http.StatusForbidden)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName,
		// Store the address Google asserted, not the folded form, so /auth/me
		// shows the admin the email they recognise. Authority is re-checked by
		// isAdmin, which folds both sides, so the stored form need not be canonical.
		Value:    signSession(a.secret, claims.Email, time.Now().Add(sessionDuration)),
		Path:     "/",
		MaxAge:   int(sessionDuration.Seconds()),
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/", http.StatusFound)
}

// handleLogout clears the session cookie.
func (a *Authenticator) handleLogout(w http.ResponseWriter, _ *http.Request) {
	a.clearCookie(w, sessionCookieName, "/")
	w.WriteHeader(http.StatusNoContent)
}

// handleMe reports the logged-in admin's email, or 401 if there is no valid
// admin session. Used by the frontend to show logged-in state.
func (a *Authenticator) handleMe(w http.ResponseWriter, r *http.Request) {
	email, ok := a.adminFromRequest(r)
	if !ok {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(map[string]string{"email": email}); err != nil {
		a.logger.Error("Failed to encode /auth/me response", zap.Error(err))
	}
}

// adminEmailContextKey keys the verified admin email stashed in a request's
// context by requireAdmin. Unexported so only this package can set or read it.
type adminEmailContextKey struct{}

// requireAdmin wraps a handler, allowing it through only for a valid admin
// session. It re-checks the allowlist on every request, so revoking an admin in
// config locks out their still-valid cookie on the next request after reload.
// The verified admin email is stashed in the request context so gated handlers
// can attribute the action without re-parsing the cookie.
func (a *Authenticator) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		email, ok := a.adminFromRequest(r)
		if !ok {
			http.Error(w, "not authorised", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), adminEmailContextKey{}, email)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// adminEmail returns the verified admin email requireAdmin stashed in ctx. It is
// only present on requests that passed through requireAdmin; the empty string
// means the handler was not gated.
func adminEmail(ctx context.Context) string {
	email, _ := ctx.Value(adminEmailContextKey{}).(string)
	return email
}

// adminFromRequest returns the email of a valid admin session on the request, if
// any. It checks both cookie integrity (identity) and allowlist membership
// (authority).
func (a *Authenticator) adminFromRequest(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return "", false
	}
	email, err := verifySession(a.secret, cookie.Value, time.Now())
	if err != nil {
		return "", false
	}
	if !a.isAdmin(email) {
		return "", false
	}
	return email, true
}

// isAdmin reports whether email is on the allowlist (case-insensitive).
func (a *Authenticator) isAdmin(email string) bool {
	_, ok := a.adminEmails[normaliseEmail(email)]
	return ok
}

// clearCookie expires the named cookie at path.
func (a *Authenticator) clearCookie(w http.ResponseWriter, name, path string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     path,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// pickRedirectURI selects the registered redirect URI matching the environment:
// a localhost URI for dev (env != "prod"), a non-localhost URI for prod. Falls
// back to the first URI if none matches.
func pickRedirectURI(uris []string, env string) string {
	wantLocal := env != "prod"
	for _, u := range uris {
		if isLocalhostURI(u) == wantLocal {
			return u
		}
	}
	if len(uris) > 0 {
		return uris[0]
	}
	return ""
}

func isLocalhostURI(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// normaliseEmail folds an email to a canonical form for allowlist comparison,
// so an admin matches regardless of which equivalent form they type. It always
// lowercases and trims. For Gmail addresses it additionally folds the
// googlemail.com alias, drops the insignificant dots Gmail ignores, and strips
// +tag subaddressing — all three are Gmail-specific, so they are applied only to
// gmail/googlemail addresses; dots and + are significant on every other domain.
func normaliseEmail(email string) string {
	e := strings.ToLower(strings.TrimSpace(email))

	local, domain, ok := strings.Cut(e, "@")
	if !ok {
		return e
	}

	if domain == "googlemail.com" {
		domain = "gmail.com"
	}
	if domain == "gmail.com" {
		if plus := strings.IndexByte(local, '+'); plus >= 0 {
			local = local[:plus]
		}
		local = strings.ReplaceAll(local, ".", "")
	}

	return local + "@" + domain
}

// randomToken returns a URL-safe random token for OAuth state.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
