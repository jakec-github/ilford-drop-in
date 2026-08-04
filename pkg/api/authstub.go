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
// checked and expired exactly like a real one, and requireAdmin still refuses
// requests without it, so what an agent sees is the real gate rather than an
// open door. Reaching this constructor needs a devMode block in config, which
// only the dev environment may carry (internal/config.checkDevMode).
func NewStubAuthenticator(dev *config.DevModeConfig, srv *config.ServerConfig, logger *zap.Logger, syncVolunteers VolunteerSyncFunc) (*Authenticator, error) {
	a := &Authenticator{
		secret:      []byte(srv.SessionSecret),
		adminEmails: adminAllowlist(srv.AdminEmails),
		// Dev runs over plain HTTP: a Secure cookie would be set and never sent back.
		secure:         false,
		logger:         logger,
		syncVolunteers: syncVolunteers,
		stubEmail:      dev.AdminEmail,
	}

	// A session for an address off the allowlist carries no authority, so login
	// would appear to work and every admin route would still 401. Refuse to
	// start rather than hand over that puzzle.
	if !a.isAdmin(dev.AdminEmail) {
		return nil, fmt.Errorf("devMode.adminEmail %q is not in server.adminEmails, so the session it mints would have no admin rights", dev.AdminEmail)
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

// handleStubLogin mints the admin session directly. It stands in for the whole
// login redirect, code exchange and ID-token verification, and is reached only
// when stubEmail is set.
func (a *Authenticator) handleStubLogin(w http.ResponseWriter, r *http.Request) {
	a.setSessionCookie(w, a.stubEmail)
	a.logger.Warn("Dev mode: issued an admin session without verifying identity",
		zap.String("email", a.stubEmail))
	http.Redirect(w, r, "/", http.StatusFound)
}
