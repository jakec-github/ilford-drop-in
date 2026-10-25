package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"go.uber.org/zap"
	"golang.org/x/oauth2"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/utils"
)

const (
	// googleIssuer is the OIDC issuer used for provider discovery and ID-token verification.
	googleIssuer = "https://accounts.google.com"

	sessionCookieName = "session"
	stateCookieName   = "oauth_state"
	// stateCookieMaxAge bounds how long a login attempt may sit at Google's consent screen.
	stateCookieMaxAge = 10 * time.Minute
)

// Level is how much a signed-in person may do. There are two, and one contains
// the other: an Organiser can do everything a Rota Editor can.
type Level string

const (
	// LevelRotaEditor changes shifts and nothing else: Alterations and Cover
	// on an allocated rota, Preallocations on the rota in flight.
	LevelRotaEditor Level = "rotaEditor"
	// LevelOrganiser makes every edit the app offers.
	LevelOrganiser Level = "organiser"
)

// atLeast reports whether l may do what min may.
func (l Level) atLeast(min Level) bool {
	return l == LevelOrganiser || l == min
}

// Session is who a request comes from and what they may do.
type Session struct {
	Email string `json:"email"`
	Level Level  `json:"level"`
}

// Authenticator handles the OIDC login flow and session cookies, for every
// Organisation the server runs. It proves identity and nothing more: the
// signed cookie carries an email, and what that person may do is decided per
// request by the Organisation in the path, from its own allowlists (ADR 0011).
// That is also why a level is never stored in the cookie: moving someone
// between lists takes effect on their next request.
type Authenticator struct {
	oauth2Config *oauth2.Config
	verifier     *oidc.IDTokenVerifier
	secret       []byte
	secure       bool // set the cookie Secure flag (prod only)
	logger       *zap.Logger
	// organisations is each Organisation's allowlists and the way to finish a
	// send it started, keyed by id. Filled by NewHandler, one entry per
	// Organisation, before the server serves anything.
	organisations map[string]registeredOrganisation
	// stubEmail, when non-empty, replaces the Google round-trip: login mints a
	// session for this address directly. Set only by NewStubAuthenticator, which
	// the dev environment alone can reach (see authstub.go).
	stubEmail string
	// stubRotaEditor is the address stub login signs in as when asked for a
	// Rota Editor. Empty when dev mode names none.
	stubRotaEditor string
}

// registeredOrganisation is what the shared /auth endpoints need to know about
// one Organisation: who may sign in for it, and how to finish a send it started.
type registeredOrganisation struct {
	access access
	// completeSend finishes an incremental gmail.send grant arriving at the
	// shared callback. It is the Organisation's Handler's, because the send
	// needs its store and its roster and this type has neither.
	completeSend http.HandlerFunc
}

// register makes an Organisation known to the shared endpoints. Called by
// NewHandler, at startup.
func (a *Authenticator) register(id string, acc access, completeSend http.HandlerFunc) {
	if a.organisations == nil {
		a.organisations = make(map[string]registeredOrganisation)
	}
	a.organisations[id] = registeredOrganisation{access: acc, completeSend: completeSend}
}

// onAnyAllowlist reports whether email may act for some Organisation, which is
// all login asks: a session is identity, and each Organisation decides for
// itself what it allows (ADR 0011).
func (a *Authenticator) onAnyAllowlist(email string) bool {
	for _, org := range a.organisations {
		if _, ok := org.access.levelOf(email); ok {
			return true
		}
	}
	return false
}

// returnTo is where a browser lands once it has signed in: the Organisation it
// set out from, or the root when it named none.
//
// The id is only checked to be shaped like one, never looked up: an id that
// names no Organisation lands on the same 404 as any other unknown address, so
// login cannot be used to find out which ids exist. The shape is what keeps the
// redirect on this site.
func returnTo(organisationID string) string {
	if !validOrganisationID.MatchString(organisationID) {
		return "/"
	}
	return organisationPath(organisationID) + "/"
}

// returnCookieName carries the Organisation a login set out from through
// Google and back, alongside the state.
const returnCookieName = "oauth_return"

// isStubbed reports whether the Google round-trip has been replaced, which is
// dev mode and nothing else.
func (a *Authenticator) isStubbed() bool {
	return a.stubEmail != ""
}

// samePerson reports whether two addresses name the same person, folded the
// way the allowlists fold them so an equivalent form still matches.
func (a *Authenticator) samePerson(x, y string) bool {
	return normaliseEmail(x) == normaliseEmail(y)
}

// NewAuthenticator builds an Authenticator. It performs OIDC provider discovery
// against Google, so it makes a network call and can fail.
func NewAuthenticator(ctx context.Context, webCfg *config.OAuthClientWebConfig, srv *config.ServerConfig, env string, logger *zap.Logger) (*Authenticator, error) {
	provider, err := oidc.NewProvider(ctx, googleIssuer)
	if err != nil {
		return nil, fmt.Errorf("failed to discover OIDC provider: %w", err)
	}

	redirectURL, err := resolveRedirectURI(webCfg.Web.RedirectURIs, env, srv.RedirectURI)
	if err != nil {
		return nil, err
	}

	oauth2Config := &oauth2.Config{
		ClientID:     webCfg.Web.ClientID,
		ClientSecret: webCfg.Web.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  redirectURL,
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
	}

	return &Authenticator{
		oauth2Config: oauth2Config,
		verifier:     provider.Verifier(&oidc.Config{ClientID: webCfg.Web.ClientID}),
		secret:       []byte(srv.SessionSecret),
		secure:       config.Hosted(env),
		logger:       logger,
	}, nil
}

// registerRoutes attaches the shared /auth endpoints to mux. What a signed-in
// person is, and anything they may do, is under each Organisation's own path;
// these only prove who they are.
func (a *Authenticator) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/login", a.handleLogin)
	mux.HandleFunc("GET /auth/callback", a.handleCallback)
	mux.HandleFunc("POST /auth/logout", a.handleLogout)
}

// handleLogin starts the OIDC flow: stash a random state in a short-lived cookie
// and redirect to Google's consent screen for identity scopes only. In dev mode
// there is no consent screen to visit, so it signs the stub account in on the spot.
//
// ?org= names the Organisation to land on afterwards. Each Organisation's
// pages link to their own /org/<id>/auth/login, which adds it.
func (a *Authenticator) handleLogin(w http.ResponseWriter, r *http.Request) {
	if a.stubEmail != "" {
		a.handleStubLogin(w, r)
		return
	}

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
	http.SetCookie(w, &http.Cookie{
		Name:     returnCookieName,
		Value:    r.URL.Query().Get("org"),
		Path:     "/auth",
		MaxAge:   int(stateCookieMaxAge.Seconds()),
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, a.oauth2Config.AuthCodeURL(state), http.StatusFound)
}

// handleCallback completes the flow: verify state, exchange the code, verify the
// ID token, check the allowlists, and set the session cookie. Someone on
// neither list is rejected here with no cookie set.
//
// A send comes back through this same URI, because it is the one registered with
// Google and adding a second is a manual step in the console for every
// environment. Its state is signed rather than random, which is what tells the
// two flows apart before any of the login handling below runs — a send carries
// no state cookie to check against.
func (a *Authenticator) handleCallback(w http.ResponseWriter, r *http.Request) {
	if isGmailState(r.URL.Query().Get("state")) {
		// The Organisation the send belongs to is in the signed state, which is
		// the only thing that comes back from Google to say which one it was.
		state, err := verifyGmailState(a.secret, r.URL.Query().Get("state"), time.Now())
		if err != nil {
			a.logger.Warn("Rejected an availability send with an invalid state", zap.Error(err))
			http.Error(w, "invalid OAuth state", http.StatusBadRequest)
			return
		}
		org, ok := a.organisations[state.Organisation]
		if !ok || org.completeSend == nil {
			http.Error(w, "sending is not available on this server", http.StatusNotFound)
			return
		}
		org.completeSend(w, r)
		return
	}

	if a.stubEmail != "" {
		// No OAuth client exists to exchange a code with; say so rather than
		// reach for the verifier the stub was never given.
		http.Error(w, "OAuth callback is not available in dev mode", http.StatusNotFound)
		return
	}

	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil || stateCookie.Value == "" || stateCookie.Value != r.URL.Query().Get("state") {
		http.Error(w, "invalid OAuth state", http.StatusBadRequest)
		return
	}
	// The state is single-use; clear it regardless of what follows.
	a.clearCookie(w, stateCookieName, "/auth")
	var organisationID string
	if c, err := r.Cookie(returnCookieName); err == nil {
		organisationID = c.Value
	}
	a.clearCookie(w, returnCookieName, "/auth")

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

	if !claims.EmailVerified || !a.onAnyAllowlist(claims.Email) {
		// On no Organisation's lists: no session is created. A session existing
		// means an Organiser or a Rota Editor of somewhere.
		a.logger.Warn("Rejected login from someone on no allowlist",
			zap.String("email", claims.Email),
			zap.Bool("email_verified", claims.EmailVerified))
		http.Error(w, "not authorised", http.StatusForbidden)
		return
	}

	a.setSessionCookie(w, claims.Email)

	http.Redirect(w, r, returnTo(organisationID), http.StatusFound)
}

// setSessionCookie issues the signed session for email.
func (a *Authenticator) setSessionCookie(w http.ResponseWriter, email string) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName,
		// Store the address the identity provider asserted, not the folded form,
		// so /api/me shows the email they recognise. Authority is re-checked by
		// levelOf, which folds both sides, so the stored form need not be
		// canonical.
		Value:    signSession(a.secret, email, time.Now().Add(sessionDuration)),
		Path:     "/",
		MaxAge:   int(sessionDuration.Seconds()),
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// handleLogout clears the session cookie.
func (a *Authenticator) handleLogout(w http.ResponseWriter, _ *http.Request) {
	a.clearCookie(w, sessionCookieName, "/")
	w.WriteHeader(http.StatusNoContent)
}

// emailFromRequest returns the identity a valid session cookie proves, if any.
// It says nothing about what that person may do: that is each Organisation's
// to decide (Handler.session).
func (a *Authenticator) emailFromRequest(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return "", false
	}
	email, err := verifySession(a.secret, cookie.Value, time.Now())
	if err != nil {
		return "", false
	}
	return email, true
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

// resolveRedirectURI selects the redirect URI to use from those registered with
// the OAuth client. A non-empty preferred URI wins, but only if it is one of the
// registered ones — Google rejects anything else, so failing here gives a far
// clearer error than the consent screen would. Otherwise the URI is chosen by
// locality: a non-localhost URI for a hosted environment (config.Hosted), a
// localhost one otherwise, falling back to the first registered URI if none matches.
func resolveRedirectURI(uris []string, env, preferred string) (string, error) {
	if len(uris) == 0 {
		return "", fmt.Errorf("no redirect URI registered with the OAuth client for env %q", env)
	}

	if preferred != "" {
		if slices.Contains(uris, preferred) {
			return preferred, nil
		}
		return "", fmt.Errorf("redirect URI %q is not registered with the OAuth client; registered: %v", preferred, uris)
	}

	wantLocal := !config.Hosted(env)
	for _, u := range uris {
		if isLocalhostURI(u) == wantLocal {
			return u, nil
		}
	}
	return uris[0], nil
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
// so a person matches regardless of which equivalent form they type. It always
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

// randomToken returns a URL-safe random token for OAuth state. It shares an
// implementation with the availability links, which are bearer credentials of
// the same shape: one definition means one entropy decision.
func randomToken() (string, error) {
	return utils.RandomToken()
}
