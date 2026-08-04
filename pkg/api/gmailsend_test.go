package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"golang.org/x/oauth2"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// recordingMailer stands in for Gmail. It is mutex-guarded because a send runs
// in its own goroutine, which is the whole point of the job it runs behind.
type recordingMailer struct {
	mu   sync.Mutex
	sent []string
}

func (m *recordingMailer) SendEmail(to, _, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, to)
	return nil
}

func (m *recordingMailer) recipients() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.sent...)
}

// sendTestStore is a round of three volunteers, all holding links and none of
// them sent.
func sendTestStore() *mockStore {
	return &mockStore{
		rotations: []db.Rotation{{ID: "rota-1", Start: "2026-08-02", End: "2026-08-09", ShiftCount: 2}},
		shifts: []db.Shift{
			{ID: "shift-1", RotaID: "rota-1", Date: "2026-08-02"},
			{ID: "shift-2", RotaID: "rota-1", Date: "2026-08-09"},
		},
		availabilityRequests: []db.AvailabilityRequest{
			{ID: "req-alice", RotaID: "rota-1", VolunteerID: "alice", Token: "tok-alice"},
			{ID: "req-bob", RotaID: "rota-1", VolunteerID: "bob", Token: "tok-bob"},
			{ID: "req-charlie", RotaID: "rota-1", VolunteerID: "charlie", Token: "tok-charlie"},
		},
	}
}

// newSendTestHandler wires a handler whose identity provider and mailer are both
// stubbed, which is the dev-mode shape: the OAuth round-trip is skipped and the
// send runs for real against the store.
func newSendTestHandler(store *mockStore, mailer services.GmailClient) http.Handler {
	auth := newTestAuthenticator()
	// Dev mode: /auth/gmail goes straight to the send rather than to Google.
	auth.stubEmail = testAdminEmail

	volunteers := testVolunteers()
	for i := range volunteers.volunteers {
		volunteers.volunteers[i].Email = volunteers.volunteers[i].ID + "@example.com"
	}

	newMailer := func(context.Context, *oauth2.Token) (services.GmailClient, error) { return mailer, nil }
	return NewHandler(store, volunteers, apiTestCfg, auth, nil, newMailer, zap.NewNop()).Routes()
}

// startSendRequest returns the job id a started send redirected to.
func startSendRequest(t *testing.T, handler http.Handler, query string) string {
	t.Helper()
	rec := doRequest(t, handler, http.MethodGet, "/auth/gmail?"+query, "", adminCookie())
	require.Equal(t, http.StatusFound, rec.Code, rec.Body.String())

	location, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, sendReturnPath, location.Path)

	jobID := location.Query().Get("send")
	require.NotEmpty(t, jobID, "the redirect must name the job to watch: %s", rec.Header().Get("Location"))
	return jobID
}

// awaitSend polls the send endpoint the way the admin page does, and returns the
// finished job.
func awaitSend(t *testing.T, handler http.Handler, jobID string) sendResponse {
	t.Helper()
	var resp sendResponse
	require.Eventually(t, func() bool {
		rec := doRequest(t, handler, http.MethodGet, "/api/availability-sends/"+jobID, "", adminCookie())
		if rec.Code != http.StatusOK {
			return false
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		return resp.Finished
	}, 5*time.Second, 10*time.Millisecond, "the send never finished")
	return resp
}

// TestSendEndpointsRequireAdmin: a send mails every volunteer on the roster as
// the admin, and reading one back lists their addresses. Neither is anonymous.
func TestSendEndpointsRequireAdmin(t *testing.T) {
	handler := newSendTestHandler(sendTestStore(), &recordingMailer{})

	rec := doRequest(t, handler, http.MethodGet, "/auth/gmail?mode=round&deadline=Friday", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = doRequest(t, handler, http.MethodGet, "/api/availability-sends/anything", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestSendRoundRunsAndStampsSentAt: the acceptance criterion for the whole
// slice — the round goes out, and every request whose email sent is stamped so
// the next send does not ask them again.
func TestSendRoundRunsAndStampsSentAt(t *testing.T) {
	store := sendTestStore()
	mailer := &recordingMailer{}
	handler := newSendTestHandler(store, mailer)

	jobID := startSendRequest(t, handler, "mode=round&deadline=Friday+7+August")
	resp := awaitSend(t, handler, jobID)

	assert.Equal(t, "round", resp.Mode)
	assert.Empty(t, resp.Error)
	assert.Len(t, resp.Sent, 3)
	assert.Empty(t, resp.Failed)
	assert.Equal(t, 3, resp.Total)
	assert.ElementsMatch(t,
		[]string{"alice@example.com", "bob@example.com", "charlie@example.com"},
		mailer.recipients())

	for _, req := range store.availabilityRequests {
		assert.NotEmpty(t, req.SentAt, "%s", req.VolunteerID)
	}
}

// TestSendIsRefusedWithoutADeadline: the deadline is quoted in every email, so a
// send without one is rejected before anybody is sent to a consent screen to
// approve an action that was never going to run.
func TestSendIsRefusedWithoutADeadline(t *testing.T) {
	mailer := &recordingMailer{}
	handler := newSendTestHandler(sendTestStore(), mailer)

	rec := doRequest(t, handler, http.MethodGet, "/auth/gmail?mode=round", "", adminCookie())

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, mailer.recipients())
}

// TestSendIsRefusedForAnUnknownMode: a typo in the mode must not be read as one
// of the three real ones.
func TestSendIsRefusedForAnUnknownMode(t *testing.T) {
	mailer := &recordingMailer{}
	handler := newSendTestHandler(sendTestStore(), mailer)

	for _, query := range []string{"deadline=Friday", "mode=everyone&deadline=Friday", "mode=resend&deadline=Friday"} {
		rec := doRequest(t, handler, http.MethodGet, "/auth/gmail?"+query, "", adminCookie())
		assert.Equal(t, http.StatusBadRequest, rec.Code, query)
	}
	assert.Empty(t, mailer.recipients())
}

// TestResendMailsOneVolunteer: the answer to a bounced email, addressed at the
// grain a request is minted at.
func TestResendMailsOneVolunteer(t *testing.T) {
	store := sendTestStore()
	mailer := &recordingMailer{}
	handler := newSendTestHandler(store, mailer)

	jobID := startSendRequest(t, handler, "mode=resend&volunteerId=bob&deadline=Friday")
	resp := awaitSend(t, handler, jobID)

	assert.Equal(t, []string{"bob@example.com"}, mailer.recipients())
	require.Len(t, resp.Sent, 1)
	assert.Equal(t, "bob", resp.Sent[0].VolunteerID)
}

// TestSendResultIsReadableOnlyByTheAdminWhoStartedIt: a finished send names
// every volunteer it reached and every address it failed on, which belongs to
// the admin who asked for it and to nobody else on the allowlist.
func TestSendResultIsReadableOnlyByTheAdminWhoStartedIt(t *testing.T) {
	auth := newTestAuthenticator()
	auth.stubEmail = testAdminEmail
	auth.adminEmails["other@example.com"] = struct{}{}

	handler := NewHandler(sendTestStore(), testVolunteers(), apiTestCfg, auth, nil,
		func(context.Context, *oauth2.Token) (services.GmailClient, error) { return &recordingMailer{}, nil },
		zap.NewNop()).Routes()

	jobID := startSendRequest(t, handler, "mode=round&deadline=Friday")

	otherAdmin := &http.Cookie{
		Name:  sessionCookieName,
		Value: signSession(testSecret, "other@example.com", time.Now().Add(time.Hour)),
	}
	rec := doRequest(t, handler, http.MethodGet, "/api/availability-sends/"+jobID, "", otherAdmin)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec = doRequest(t, handler, http.MethodGet, "/api/availability-sends/"+jobID, "", adminCookie())
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestUnknownSendIsNotFound: a job that has aged out reads the same as one that
// never existed, which is what it means to the page asking.
func TestUnknownSendIsNotFound(t *testing.T) {
	handler := newSendTestHandler(sendTestStore(), &recordingMailer{})

	rec := doRequest(t, handler, http.MethodGet, "/api/availability-sends/no-such-job", "", adminCookie())
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestGmailStateRoundTrips: the pending send survives the trip to Google and
// back unchanged, which is the only reason it can be carried in a query
// parameter at all.
func TestGmailStateRoundTrips(t *testing.T) {
	want := gmailSendState{
		Email:       testAdminEmail,
		Mode:        services.SendModeResend,
		RotaID:      "rota-1",
		Deadline:    "Friday 7 August",
		VolunteerID: "bob",
		Expiry:      time.Now().Add(time.Minute).Unix(),
	}

	signed, err := signGmailState(testSecret, want)
	require.NoError(t, err)
	assert.True(t, isGmailState(signed), "the callback tells the two flows apart by this prefix")

	got, err := verifyGmailState(testSecret, signed, time.Now())
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// TestGmailStateRejectsTamperingAndAge: the state is the instruction, so an
// edited one would be an admin action nobody authorised — a deadline swapped,
// or a resend re-pointed at somebody else.
func TestGmailStateRejectsTamperingAndAge(t *testing.T) {
	state := gmailSendState{
		Email:    testAdminEmail,
		Mode:     services.SendModeRound,
		Deadline: "Friday",
		Expiry:   time.Now().Add(time.Minute).Unix(),
	}
	signed, err := signGmailState(testSecret, state)
	require.NoError(t, err)

	_, err = verifyGmailState(testSecret, signed+"x", time.Now())
	assert.Error(t, err, "a changed signature must not verify")

	_, err = verifyGmailState([]byte("a-different-secret-entirely!!!!!"), signed, time.Now())
	assert.Error(t, err, "a state signed by anyone else must not verify")

	_, err = verifyGmailState(testSecret, signed, time.Now().Add(2*time.Minute))
	assert.Error(t, err, "a send left at the consent screen must not stay startable")

	// A login state is a bare random token, and must not be mistaken for a send.
	assert.False(t, isGmailState("Kx9mQp2vL8nR4tY7wZ1aB3cD5eF6gH0j"))
}

// TestGmailConsentAsksOnlyForTheSendScope: the grant has to be additive and
// minimal. Asking for the identity scopes again would replace the login grant,
// and prompt=consent would make every send after the first re-prompt.
func TestGmailConsentAsksOnlyForTheSendScope(t *testing.T) {
	auth := newTestAuthenticator()
	auth.oauth2Config = &oauth2.Config{
		ClientID:    "test-client",
		RedirectURL: "http://localhost:5173/auth/callback",
		Endpoint:    oauth2.Endpoint{AuthURL: "https://accounts.google.com/o/oauth2/auth"},
		Scopes:      []string{"openid", "email", "profile"},
	}
	handler := NewHandler(sendTestStore(), testVolunteers(), apiTestCfg, auth, nil, nil, zap.NewNop()).Routes()

	rec := doRequest(t, handler, http.MethodGet, "/auth/gmail?mode=round&deadline=Friday", "", adminCookie())
	require.Equal(t, http.StatusFound, rec.Code)

	consent, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	query := consent.Query()

	assert.Equal(t, "https://www.googleapis.com/auth/gmail.send", query.Get("scope"))
	assert.Equal(t, "true", query.Get("include_granted_scopes"))
	assert.Empty(t, query.Get("prompt"), "a second send in the same session must not re-prompt")
	assert.NotEqual(t, "offline", query.Get("access_type"), "a refresh token would be a standing credential")
	assert.Equal(t, testAdminEmail, query.Get("login_hint"))
	assert.True(t, isGmailState(query.Get("state")))
}

// TestSendCallbackRejectsAStateForAnotherAdmin: the signature proves the
// instruction is ours, the session proves who is presenting it. A state captured
// from one admin must not run in another's browser, under their Gmail account.
func TestSendCallbackRejectsAStateForAnotherAdmin(t *testing.T) {
	auth := newTestAuthenticator()
	auth.adminEmails["other@example.com"] = struct{}{}
	mailer := &recordingMailer{}
	handler := NewHandler(sendTestStore(), testVolunteers(), apiTestCfg, auth, nil,
		func(context.Context, *oauth2.Token) (services.GmailClient, error) { return mailer, nil },
		zap.NewNop()).Routes()

	signed, err := signGmailState(testSecret, gmailSendState{
		Email:    "other@example.com",
		Mode:     services.SendModeRound,
		Deadline: "Friday",
		Expiry:   time.Now().Add(time.Minute).Unix(),
	})
	require.NoError(t, err)

	rec := doRequest(t, handler, http.MethodGet, "/auth/callback?code=x&state="+url.QueryEscape(signed), "", adminCookie())

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, mailer.recipients())
}

// TestSendCallbackRequiresASession: the state alone is not authority. Someone
// who captured one out of a browser's history must not be able to replay it.
func TestSendCallbackRequiresASession(t *testing.T) {
	mailer := &recordingMailer{}
	handler := NewHandler(sendTestStore(), testVolunteers(), apiTestCfg, newTestAuthenticator(), nil,
		func(context.Context, *oauth2.Token) (services.GmailClient, error) { return mailer, nil },
		zap.NewNop()).Routes()

	signed, err := signGmailState(testSecret, gmailSendState{
		Email:    testAdminEmail,
		Mode:     services.SendModeRound,
		Deadline: "Friday",
		Expiry:   time.Now().Add(time.Minute).Unix(),
	})
	require.NoError(t, err)

	rec := doRequest(t, handler, http.MethodGet, "/auth/callback?code=x&state="+url.QueryEscape(signed), "")

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, mailer.recipients())
}

// TestSendKeepsNoCredentialOnTheSession: the point of asking for gmail.send at
// send time is that nothing survives the send. Whatever the callback does, it
// must not come back having put a credential in the admin's cookie jar.
func TestSendKeepsNoCredentialOnTheSession(t *testing.T) {
	handler := newSendTestHandler(sendTestStore(), &recordingMailer{})

	rec := doRequest(t, handler, http.MethodGet, "/auth/gmail?mode=round&deadline=Friday", "", adminCookie())

	require.Equal(t, http.StatusFound, rec.Code)
	assert.Empty(t, rec.Result().Cookies(), "starting a send must not write any cookie")
}
