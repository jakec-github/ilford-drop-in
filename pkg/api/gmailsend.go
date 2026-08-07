package api

import (
	"context"
	"crypto/hmac"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/oauth2"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
	"github.com/jakechorley/ilford-drop-in/pkg/utils"
)

// gmailStatePrefix marks an OAuth state as belonging to the incremental
// gmail.send grant rather than to login. Both flows come back to the one
// redirect URI — it is registered with Google, and adding a second one is a
// manual step in the console for every environment — so the state is what tells
// the callback which flow it is completing.
const gmailStatePrefix = "send."

// gmailStateMaxAge bounds how long a pending send may sit at Google's consent
// screen before its state stops being accepted.
const gmailStateMaxAge = 10 * time.Minute

// sendReturnPath is where the browser lands once a send has been started: the
// Allocation tab, which is where the round is asked from (issue #145). The job
// id goes in the query so the page can pick the send back up — the redirect
// returns immediately and the emails go out behind it.
const sendReturnPath = "/admin/allocation"

// gmailSendState is the pending send, carried through Google and back. It is
// signed rather than stored: the round trip is the only thing that needs to
// remember it, and a signature makes it unforgeable without a server-side table
// of half-finished sends to expire.
//
// Nothing here is secret — it is the admin's own instruction coming back to
// them — but every field is authority, so all of it is covered by the signature.
type gmailSendState struct {
	Email       string            `json:"email"`
	Mode        services.SendMode `json:"mode"`
	RotaID      string            `json:"rotaId,omitempty"`
	Deadline    string            `json:"deadline"`
	VolunteerID string            `json:"volunteerId,omitempty"`
	Expiry      int64             `json:"exp"`
}

// signGmailState encodes a pending send as an OAuth state parameter.
func signGmailState(secret []byte, state gmailSendState) (string, error) {
	payload, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return gmailStatePrefix + encoded + "." + base64.RawURLEncoding.EncodeToString(hmacSum(secret, encoded)), nil
}

// verifyGmailState returns the pending send a state carries, if it is one of
// ours and has not expired.
func verifyGmailState(secret []byte, raw string, now time.Time) (gmailSendState, error) {
	var state gmailSendState

	trimmed, isSend := strings.CutPrefix(raw, gmailStatePrefix)
	if !isSend {
		return state, errInvalidSession
	}
	encoded, sig, found := strings.Cut(trimmed, ".")
	if !found {
		return state, errInvalidSession
	}

	gotSig, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(gotSig, hmacSum(secret, encoded)) {
		return state, errInvalidSession
	}

	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return state, errInvalidSession
	}
	if err := json.Unmarshal(payload, &state); err != nil {
		return state, errInvalidSession
	}
	if now.After(time.Unix(state.Expiry, 0)) {
		return state, errInvalidSession
	}
	return state, nil
}

// isGmailState reports whether a callback's state belongs to the send flow. Only
// the prefix is checked; the signature is verified by the handler that acts on
// it.
func isGmailState(raw string) bool {
	return strings.HasPrefix(raw, gmailStatePrefix)
}

// gmailAuthCodeURL is Google's consent screen for the gmail.send scope alone.
//
// include_granted_scopes makes the grant additive rather than a replacement, and
// no prompt is set on purpose: with the scope already granted Google approves
// silently and bounces straight back, which is what makes the second send of a
// session no different from a button press. login_hint pins the account to the
// admin already signed in, so the mail cannot go out from a personal address
// they happen to also be logged into.
func (a *Authenticator) gmailAuthCodeURL(state, adminEmail string) string {
	cfg := a.gmailOAuthConfig()
	return cfg.AuthCodeURL(state,
		oauth2.SetAuthURLParam("include_granted_scopes", "true"),
		oauth2.SetAuthURLParam("login_hint", adminEmail),
	)
}

// gmailOAuthConfig is the login config with the send scope in place of the
// identity ones. Same client and same redirect URI — only the scope differs,
// which is the whole point of asking for it separately.
func (a *Authenticator) gmailOAuthConfig() *oauth2.Config {
	cfg := *a.oauth2Config
	cfg.Scopes = []string{utils.ScopeGmailSend}
	return &cfg
}

// handleGmailConsent starts a send. It does not send anything: it signs the
// pending action into an OAuth state and hands the browser to Google for the
// gmail.send scope, which the callback completes.
//
// Requesting the scope here rather than at login is the point of the design. The
// session cookie carries identity, not authority, and re-checks the allowlist on
// every request; a login-time grant would have the server holding a live Google
// credential for sixty days that removing someone from adminEmails would not
// revoke. It would also demand Gmail permission from an admin who only signed in
// to look at a shift.
func (h *Handler) handleGmailConsent(w http.ResponseWriter, r *http.Request) {
	admin := adminEmail(r.Context())

	state := gmailSendState{
		Email:       admin,
		Mode:        services.SendMode(r.URL.Query().Get("mode")),
		RotaID:      r.URL.Query().Get("rotaId"),
		Deadline:    r.URL.Query().Get("deadline"),
		VolunteerID: r.URL.Query().Get("volunteerId"),
		Expiry:      time.Now().Add(gmailStateMaxAge).Unix(),
	}

	// Rejected here rather than after the consent screen: sending someone to
	// Google only to fail on the way back would ask them to approve access for
	// an action that was never going to run.
	if err := validateSendState(state); err != nil {
		h.writeServiceError(w, err)
		return
	}

	// Dev mode has no Google to ask and no mailbox to send from. Skipping
	// straight to the send keeps the whole flow — the deadline, the job, the
	// per-volunteer report — exercisable on a checkout with no credentials.
	if h.auth.isStubbed() {
		h.auth.logger.Warn("Dev mode: starting an availability send with no Gmail grant and no real mail",
			zap.String("mode", string(state.Mode)))
		h.startSend(w, r, admin, nil, state)
		return
	}

	signed, err := signGmailState(h.auth.secret, state)
	if err != nil {
		h.auth.logger.Error("Failed to sign the pending send", zap.Error(err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, h.auth.gmailAuthCodeURL(signed, admin), http.StatusFound)
}

// validateSendState rejects an instruction that could not be carried out, before
// anyone is asked to approve anything.
func validateSendState(state gmailSendState) error {
	switch state.Mode {
	case services.SendModeRound, services.SendModeReminder:
	case services.SendModeResend:
		if state.VolunteerID == "" {
			return wrapInvalid("a resend needs the volunteer to resend to")
		}
	default:
		return wrapInvalid("unknown send mode " + string(state.Mode))
	}
	if strings.TrimSpace(state.Deadline) == "" {
		return wrapInvalid("a deadline is required: it is quoted in the subject and the body of every email")
	}
	return nil
}

// wrapInvalid classifies a rejected send instruction so writeServiceError gives
// it a 400 with its own message.
func wrapInvalid(msg string) error {
	return errors.Join(errors.New(msg), services.ErrInvalidInput)
}

// completeGmailSend finishes the incremental grant: it exchanges the code for an
// access token and sets the send going with it. The Authenticator calls it from
// the shared callback once the state proves this is a send rather than a login.
//
// The token is never persisted — not to the database, not to the session cookie,
// not to a log. It is handed to the goroutine doing the sending and goes out of
// scope with it, which is the whole reason the scope is asked for here instead
// of at login.
func (h *Handler) completeGmailSend(w http.ResponseWriter, r *http.Request) {
	// The signature proves the instruction is ours; the session proves who is
	// asking now. Both are required, and they must name the same admin — a state
	// signed for one admin must not be replayable in another's browser.
	admin, ok := h.auth.adminFromRequest(r)
	if !ok {
		http.Error(w, "not authorised", http.StatusUnauthorized)
		return
	}

	state, err := verifyGmailState(h.auth.secret, r.URL.Query().Get("state"), time.Now())
	if err != nil || !h.auth.sameAdmin(state.Email, admin) {
		h.auth.logger.Warn("Rejected an availability send with an invalid state", zap.Error(err))
		http.Error(w, "invalid OAuth state", http.StatusBadRequest)
		return
	}

	// The admin declined at the consent screen, or Google refused. Nothing has
	// been sent, so this is a cancellation rather than an error.
	if reason := r.URL.Query().Get("error"); reason != "" {
		h.auth.logger.Info("Availability send cancelled at the consent screen", zap.String("reason", reason))
		http.Redirect(w, r, sendReturnPath+"?sendError="+url.QueryEscape("Gmail access was not granted, so nothing was sent."), http.StatusFound)
		return
	}

	token, err := h.auth.gmailOAuthConfig().Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		h.auth.logger.Warn("Gmail code exchange failed", zap.Error(err))
		http.Redirect(w, r, sendReturnPath+"?sendError="+url.QueryEscape("Could not get permission to send mail, so nothing was sent."), http.StatusFound)
		return
	}

	h.startSend(w, r, admin, token, state)
}

// startSend registers the job, launches the send behind it, and redirects the
// browser to the page that watches it.
//
// The redirect goes out before a single email does. A round is thirty-odd emails
// at one every three seconds, so holding the response until the last one landed
// would leave a blank tab for a minute and a half with nothing to say whether it
// was working or hung.
func (h *Handler) startSend(w http.ResponseWriter, r *http.Request, admin string, token *oauth2.Token, state gmailSendState) {
	mailer, err := h.newMailer(r.Context(), token)
	if err != nil {
		h.logger.Error("Failed to build a mail client for the send", zap.Error(err))
		http.Redirect(w, r, sendReturnPath+"?sendError="+url.QueryEscape("Could not reach Gmail, so nothing was sent."), http.StatusFound)
		return
	}

	jobID := uuid.New().String()
	h.sends.start(jobID, admin, state.Mode)

	// Built from the request that started the send, not from the callback's
	// context: this is the address the app answers on, and it is what the
	// volunteer has to be able to paste into a browser.
	link := func(token string) string { return availabilityLink(r, token) }

	params := services.SendParams{
		RotaID:      state.RotaID,
		Mode:        state.Mode,
		Deadline:    state.Deadline,
		VolunteerID: state.VolunteerID,
		Link:        link,
		Progress:    func(done, total int) { h.sends.progress(jobID, done, total) },
	}

	go func() {
		// Deliberately not the request's context: the browser is being
		// redirected away right now, and cancelling the send with it would stop
		// the emails at whichever volunteer happened to be next. The timeout
		// bounds it instead — comfortably over the ninety seconds a full round
		// takes, comfortably under the hour the access token lives.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 15*time.Minute)
		defer cancel()

		report, err := services.SendAvailabilityEmails(ctx, h.store, h.volunteers, mailer, h.cfg, h.logger, params)
		if err != nil {
			h.logger.Error("Availability send failed", zap.String("job", jobID), zap.Error(err))
		}
		h.sends.finish(jobID, report, err)
	}()

	http.Redirect(w, r, sendReturnPath+"?send="+url.QueryEscape(jobID), http.StatusFound)
}

type sendEmailResponse struct {
	VolunteerID   string `json:"volunteerId"`
	VolunteerName string `json:"volunteerName"`
	Email         string `json:"email,omitempty"`
	Error         string `json:"error,omitempty"`
}

// sendResponse is a send as the admin screen reads it: how far it has got while
// it runs, and who it reached and failed on once it has finished.
type sendResponse struct {
	ID       string              `json:"id"`
	Mode     string              `json:"mode"`
	Done     int                 `json:"done"`
	Total    int                 `json:"total"`
	Finished bool                `json:"finished"`
	Sent     []sendEmailResponse `json:"sent"`
	Failed   []sendEmailResponse `json:"failed"`
	Error    string              `json:"error,omitempty"`
}

// handleGetSend reports on a send in progress or just finished. Admin-gated, and
// scoped to the admin who started it: a send lists every volunteer it reached
// and every address it failed on.
func (h *Handler) handleGetSend(w http.ResponseWriter, r *http.Request) {
	snapshot, ok := h.sends.snapshot(r.PathValue("id"), adminEmail(r.Context()))
	if !ok {
		// Also the answer for a send that has aged out, which is indistinguishable
		// from one that never existed and means the same thing to the page asking.
		h.writeError(w, http.StatusNotFound, "no send with that id — it may have finished long enough ago to have been forgotten")
		return
	}

	resp := sendResponse{
		ID:       snapshot.ID,
		Mode:     string(snapshot.Mode),
		Done:     snapshot.Done,
		Total:    snapshot.Total,
		Finished: snapshot.Finished,
		Error:    snapshot.Err,
		Sent:     make([]sendEmailResponse, 0, len(snapshot.Sent)),
		Failed:   make([]sendEmailResponse, 0, len(snapshot.Failed)),
	}
	for _, s := range snapshot.Sent {
		resp.Sent = append(resp.Sent, sendEmailResponse{VolunteerID: s.VolunteerID, VolunteerName: s.VolunteerName, Email: s.Email})
	}
	for _, f := range snapshot.Failed {
		resp.Failed = append(resp.Failed, sendEmailResponse{VolunteerID: f.VolunteerID, VolunteerName: f.VolunteerName, Email: f.Email, Error: f.Error})
	}

	h.writeJSON(w, http.StatusOK, resp)
}
