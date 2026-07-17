package api

import (
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/oauth2"
)

const (
	// syncStateCookieName distinguishes a sync round-trip from a plain login on
	// the shared /auth/callback endpoint: both flows return to the same
	// registered redirect URI, and the callback tells them apart by which state
	// cookie validates.
	syncStateCookieName = "oauth_sync_state"

	// sheetsReadonlyScope is requested incrementally the first time an admin
	// syncs, on top of the identity scopes granted at login. It is the read-only
	// Sheets scope — sync only reads the volunteer sheet.
	sheetsReadonlyScope = "https://www.googleapis.com/auth/spreadsheets.readonly"

	// syncResultParam carries the sync outcome back to the admin dashboard so the
	// SPA can show a result after the redirect completes.
	syncResultParam = "synced"
	syncResultOK    = "1"
	syncResultError = "error"
)

// VolunteerSyncFunc repopulates the volunteer roster using an admin's OAuth
// access token. The token carries the incrementally granted Sheets scope; it is
// used once and discarded (never stored). Injected by the composition root so
// the Authenticator owns the OAuth dance without importing the Sheets client.
type VolunteerSyncFunc func(r *http.Request, token *oauth2.Token) error

// handleSyncStart begins the volunteer sync. It is gated by requireAdmin, so
// only a logged-in admin reaches it. It re-enters Google's consent flow asking
// for the Sheets scope on top of the already-granted identity scopes
// (incremental authorization), so the consent screen appears only the first
// time an admin syncs.
func (a *Authenticator) handleSyncStart(w http.ResponseWriter, r *http.Request) {
	state, err := randomToken()
	if err != nil {
		a.logger.Error("Failed to generate sync OAuth state", zap.Error(err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     syncStateCookieName,
		Value:    state,
		Path:     "/auth",
		MaxAge:   int(stateCookieMaxAge.Seconds()),
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, a.syncAuthCodeURL(state), http.StatusFound)
}

// syncAuthCodeURL builds the consent URL for a sync: the login scopes plus the
// read-only Sheets scope, with include_granted_scopes so Google merges it with
// the identity grant rather than replacing it. access_type stays online — the
// token is used once, so no refresh token is wanted.
func (a *Authenticator) syncAuthCodeURL(state string) string {
	scopes := append([]string{}, a.oauth2Config.Scopes...)
	scopes = append(scopes, sheetsReadonlyScope)
	return a.oauth2Config.AuthCodeURL(state,
		oauth2.SetAuthURLParam("scope", strings.Join(scopes, " ")),
		oauth2.SetAuthURLParam("include_granted_scopes", "true"),
		oauth2.AccessTypeOnline,
	)
}

// handleSyncCallback completes a sync round-trip. The shared callback has
// already confirmed the sync state cookie matches. It re-checks the admin
// session (defence in depth), exchanges the code for an access token, runs the
// sync once with that token, and redirects back to the dashboard with the
// outcome. The token is never stored.
func (a *Authenticator) handleSyncCallback(w http.ResponseWriter, r *http.Request) {
	// The state is single-use; clear it whatever follows.
	a.clearCookie(w, syncStateCookieName, "/auth")

	if _, ok := a.adminFromRequest(r); !ok {
		http.Error(w, "not authorised", http.StatusUnauthorized)
		return
	}

	token, err := a.oauth2Config.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		a.logger.Warn("Sync code exchange failed", zap.Error(err))
		a.redirectSyncResult(w, r, syncResultError)
		return
	}

	if a.syncVolunteers == nil {
		a.logger.Error("Sync requested but no sync function configured")
		a.redirectSyncResult(w, r, syncResultError)
		return
	}

	if err := a.syncVolunteers(r, token); err != nil {
		a.logger.Warn("Volunteer sync failed", zap.Error(err))
		a.redirectSyncResult(w, r, syncResultError)
		return
	}

	a.logger.Info("Volunteers synced", zap.Time("at", time.Now()))
	a.redirectSyncResult(w, r, syncResultOK)
}

// redirectSyncResult sends the admin back to the dashboard with the outcome
// encoded in a query parameter the SPA reads.
func (a *Authenticator) redirectSyncResult(w http.ResponseWriter, r *http.Request, result string) {
	http.Redirect(w, r, "/admin?"+syncResultParam+"="+result, http.StatusFound)
}
