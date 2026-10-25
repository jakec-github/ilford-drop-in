package api

import (
	"context"
	"fmt"
	"net/http"

	"go.uber.org/zap"
	"golang.org/x/oauth2"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// NewStubAuthenticator builds an Authenticator that signs in as a configured
// address instead of redirecting to Google, so the app can be driven with no
// credentials and no browser consent screen — the point of dev mode.
//
// Only the identity provider is stubbed. The session it mints is signed,
// checked and expired exactly like a real one, and requireLevel still refuses
// requests without it, so what an agent sees is the real gate rather than an
// open door. Reaching this constructor needs a devMode block in config, which
// only the dev environment may carry (internal/config.checkDevMode).
func NewStubAuthenticator(dev *config.DevModeConfig, srv *config.ServerConfig, organisations []config.Organisation, logger *zap.Logger) (*Authenticator, error) {
	a := &Authenticator{
		secret: []byte(srv.SessionSecret),
		// Dev runs over plain HTTP: a Secure cookie would be set and never sent back.
		secure:         false,
		logger:         logger,
		stubEmail:      dev.OrganiserEmail,
		stubRotaEditor: dev.RotaEditorEmail,
	}

	// A session for an address that does not hold the level it is signed in
	// for carries the wrong authority, so login would appear to work and the
	// screens would be the wrong person's. Refuse to start rather than hand
	// over that puzzle. The levels are per Organisation, so the organiser must
	// be one somewhere, and the Rota Editor must be one somewhere and an
	// Organiser nowhere — or some Organisation would show them an Organiser's
	// screens.
	levels := func(email string) map[Level]bool {
		held := make(map[Level]bool)
		for i := range organisations {
			if level, ok := accessFor(&organisations[i]).levelOf(email); ok {
				held[level] = true
			}
		}
		return held
	}
	if !levels(dev.OrganiserEmail)[LevelOrganiser] {
		return nil, fmt.Errorf("devMode.organiserEmail %q is not an Organiser of any organisation, so the session it mints would not be an Organiser's", dev.OrganiserEmail)
	}
	if dev.RotaEditorEmail != "" {
		if held := levels(dev.RotaEditorEmail); !held[LevelRotaEditor] || held[LevelOrganiser] {
			return nil, fmt.Errorf("devMode.rotaEditorEmail %q must be a Rota Editor of some organisation and an Organiser of none, so the session it mints is a Rota Editor's", dev.RotaEditorEmail)
		}
	}

	return a, nil
}

// NewStubMailer builds a MailerFunc that writes emails to the log instead of
// sending them, so the whole send flow — the deadline, the job, the
// per-volunteer report, sent_at landing on each request — is exercisable on a
// checkout with no Google credentials.
//
// It is the mail half of what NewStubAuthenticator does for identity, and stubs
// as little: recipient selection, the email's wording and the database writes
// are all the real ones, so what an agent drives here is the real flow rather
// than a mock of it.
func NewStubMailer(logger *zap.Logger) MailerFunc {
	return func(context.Context, *oauth2.Token) (services.GmailClient, error) {
		return stubMailer{logger: logger}, nil
	}
}

// stubMailer logs what would have gone out. The body is logged in full because
// it carries the volunteer's link, which is the one thing worth reading out of a
// dev-stack send.
type stubMailer struct {
	logger *zap.Logger
}

func (m stubMailer) SendEmail(to, subject, body string) error {
	m.logger.Warn("Dev mode: pretending to send an email",
		zap.String("to", to),
		zap.String("subject", subject),
		zap.String("body", body))
	return nil
}

// handleStubLogin mints a session directly. It stands in for the whole login
// redirect, code exchange and ID-token verification, and is reached only when
// stubEmail is set.
//
// Plain /auth/login signs in as the Organiser, who can reach every screen;
// ?level=rotaEditor signs in as the Rota Editor instead, so their narrower
// screens can be driven too.
func (a *Authenticator) handleStubLogin(w http.ResponseWriter, r *http.Request) {
	email := a.stubEmail
	switch Level(r.URL.Query().Get("level")) {
	case "", LevelOrganiser:
	case LevelRotaEditor:
		if a.stubRotaEditor == "" {
			http.Error(w, "dev mode has no Rota Editor to sign in as: set devMode.rotaEditorEmail", http.StatusNotFound)
			return
		}
		email = a.stubRotaEditor
	default:
		http.Error(w, "unknown level: use organiser or rotaEditor", http.StatusBadRequest)
		return
	}

	a.setSessionCookie(w, email)
	a.logger.Warn("Dev mode: issued a session without verifying identity",
		zap.String("email", email))
	http.Redirect(w, r, returnTo(r.URL.Query().Get("org")), http.StatusFound)
}
