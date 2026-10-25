package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"golang.org/x/oauth2"

	"github.com/jakechorley/ilford-drop-in/internal/config"
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
	auth.stubEmail = testOrganiserEmail

	volunteers := testVolunteers()
	for i := range volunteers.volunteers {
		volunteers.volunteers[i].Email = volunteers.volunteers[i].ID + "@example.com"
	}

	newMailer := func(context.Context, *oauth2.Token) (services.GmailClient, error) { return mailer, nil }
	return NewHandler(store, volunteers, apiTestCfg, auth, nil, newMailer, nil, nil, zap.NewNop()).Routes()
}

// postSendForm submits the form that starts a send, the way the Allocation tab
// does: a POST whose body is the instruction.
func postSendForm(t *testing.T, handler http.Handler, form string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/auth/gmail", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// startSendRequest returns the job id a started send redirected to.
func startSendRequest(t *testing.T, handler http.Handler, query string) string {
	t.Helper()
	rec := postSendForm(t, handler, query, organiserCookie())
	require.Equal(t, http.StatusFound, rec.Code, rec.Body.String())

	location, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "/org/test-org"+sendReturnPath, location.Path)

	jobID := location.Query().Get("send")
	require.NotEmpty(t, jobID, "the redirect must name the job to watch: %s", rec.Header().Get("Location"))
	return jobID
}

// awaitSend polls the send endpoint the way the Organiser page does, and returns the
// finished job.
func awaitSend(t *testing.T, handler http.Handler, jobID string) sendResponse {
	t.Helper()
	var resp sendResponse
	require.Eventually(t, func() bool {
		rec := doRequest(t, handler, http.MethodGet, "/api/availability-sends/"+jobID, "", organiserCookie())
		if rec.Code != http.StatusOK {
			return false
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		return resp.Finished
	}, 5*time.Second, 10*time.Millisecond, "the send never finished")
	return resp
}

// TestSendEndpointsRequireAnOrganiser: a send mails every volunteer on the roster as
// the Organiser, and reading one back lists their addresses. Neither is anonymous.
func TestSendEndpointsRequireAnOrganiser(t *testing.T) {
	handler := newSendTestHandler(sendTestStore(), &recordingMailer{})

	rec := postSendForm(t, handler, "mode=round&deadline=Friday")
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

	rec := postSendForm(t, handler, "mode=round", organiserCookie())

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, mailer.recipients())
}

// TestSendIsRefusedForADeadlineThatIsNotOneLine: the deadline is quoted in the
// Subject header, so a line break in it would end that header and start one of
// the sender's choosing — a Bcc collecting every volunteer's link, or a body
// written by someone else. It is refused before anyone is sent to Google.
func TestSendIsRefusedForADeadlineThatIsNotOneLine(t *testing.T) {
	mailer := &recordingMailer{}
	handler := newSendTestHandler(sendTestStore(), mailer)

	for _, deadline := range []string{
		"Friday\r\nBcc: someone@example.com",
		"Friday\nBcc: someone@example.com",
		"Friday\x00",
		strings.Repeat("x", 101),
	} {
		form := url.Values{"mode": {"round"}, "deadline": {deadline}}.Encode()
		rec := postSendForm(t, handler, form, organiserCookie())
		assert.Equal(t, http.StatusBadRequest, rec.Code, "%q", deadline)
	}
	assert.Empty(t, mailer.recipients())
}

// TestSendCannotBeStartedByALink: a send mails every volunteer, so it must not
// be something another site can set off by sending an Organiser to an address. A
// GET is not routed at all, and a POST reads its instruction from the form
// alone — the same fields on the URL are not an instruction.
func TestSendCannotBeStartedByALink(t *testing.T) {
	mailer := &recordingMailer{}
	handler := newSendTestHandler(sendTestStore(), mailer)

	rec := doRequest(t, handler, http.MethodGet, "/auth/gmail?mode=round&deadline=Friday", "", organiserCookie())
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)

	req := httptest.NewRequest(http.MethodPost, "/auth/gmail?mode=round&deadline=Friday", nil)
	req.AddCookie(organiserCookie())
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	assert.Empty(t, mailer.recipients())
}

// TestSendIsRefusedForAnUnknownMode: a typo in the mode must not be read as one
// of the three real ones.
func TestSendIsRefusedForAnUnknownMode(t *testing.T) {
	mailer := &recordingMailer{}
	handler := newSendTestHandler(sendTestStore(), mailer)

	for _, query := range []string{"deadline=Friday", "mode=everyone&deadline=Friday", "mode=resend&deadline=Friday"} {
		rec := postSendForm(t, handler, query, organiserCookie())
		assert.Equal(t, http.StatusBadRequest, rec.Code, query)
	}
	assert.Empty(t, mailer.recipients())
}

// gatedMailer holds every email until it is released, so a test can act while a
// send is still running.
type gatedMailer struct {
	recordingMailer
	gate chan struct{}
}

func (m *gatedMailer) SendEmail(to, subject, body string) error {
	<-m.gate
	return m.recordingMailer.SendEmail(to, subject, body)
}

// startSendRefused returns the error a refused send redirected back with.
func startSendRefused(t *testing.T, handler http.Handler, query string) string {
	t.Helper()
	rec := postSendForm(t, handler, query, organiserCookie())
	require.Equal(t, http.StatusFound, rec.Code, rec.Body.String())

	location, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "/org/test-org"+sendReturnPath, location.Path)
	assert.Empty(t, location.Query().Get("send"), "a refused send must not start a job")
	return location.Query().Get("sendError")
}

// TestSecondSendIsRefusedWhileOneRuns: a send stamps each volunteer as their
// email goes, so a second one started mid-run would mail again everyone the
// first had not reached yet. It is refused — from this tab, another tab or
// another Organiser — until the first has finished, and then sending works
// again.
func TestSecondSendIsRefusedWhileOneRuns(t *testing.T) {
	mailer := &gatedMailer{gate: make(chan struct{})}
	handler := newSendTestHandler(sendTestStore(), mailer)

	first := startSendRequest(t, handler, "mode=round&deadline=Friday")

	for _, query := range []string{"mode=round&deadline=Friday", "mode=reminder&deadline=Friday", "mode=resend&volunteerId=bob&deadline=Friday"} {
		assert.Contains(t, startSendRefused(t, handler, query), "already", query)
	}

	close(mailer.gate)
	resp := awaitSend(t, handler, first)
	assert.Len(t, resp.Sent, 3)
	assert.Len(t, mailer.recipients(), 3, "nobody may be mailed twice")

	again := startSendRequest(t, handler, "mode=resend&volunteerId=bob&deadline=Friday")
	awaitSend(t, handler, again)
	assert.Len(t, mailer.recipients(), 4)
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

// TestSendResultIsReadableOnlyByTheOrganiserWhoStartedIt: a finished send names
// every volunteer it reached and every address it failed on, which belongs to
// the Organiser who asked for it and to nobody else on the allowlist.
func TestSendResultIsReadableOnlyByTheOrganiserWhoStartedIt(t *testing.T) {
	auth := newTestAuthenticator()
	auth.stubEmail = testOrganiserEmail

	handler := NewHandler(sendTestStore(), testVolunteers(), withOrganiser(apiTestCfg, "other@example.com"), auth, nil,
		func(context.Context, *oauth2.Token) (services.GmailClient, error) { return &recordingMailer{}, nil }, nil, nil,
		zap.NewNop()).Routes()

	jobID := startSendRequest(t, handler, "mode=round&deadline=Friday")

	otherOrganiser := &http.Cookie{
		Name:  sessionCookieName,
		Value: signSession(testSecret, "other@example.com", time.Now().Add(time.Hour)),
	}
	rec := doRequest(t, handler, http.MethodGet, "/api/availability-sends/"+jobID, "", otherOrganiser)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec = doRequest(t, handler, http.MethodGet, "/api/availability-sends/"+jobID, "", organiserCookie())
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestUnknownSendIsNotFound: a job that has aged out reads the same as one that
// never existed, which is what it means to the page asking.
func TestUnknownSendIsNotFound(t *testing.T) {
	handler := newSendTestHandler(sendTestStore(), &recordingMailer{})

	rec := doRequest(t, handler, http.MethodGet, "/api/availability-sends/no-such-job", "", organiserCookie())
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestGmailStateRoundTrips: the pending send survives the trip to Google and
// back unchanged, which is the only reason it can be carried in a query
// parameter at all.
func TestGmailStateRoundTrips(t *testing.T) {
	want := gmailSendState{
		Organisation: "test-org",
		Email:        testOrganiserEmail,
		Mode:         services.SendModeResend,
		RotaID:       "rota-1",
		Deadline:     "Friday 7 August",
		VolunteerID:  "bob",
		Expiry:       time.Now().Add(time.Minute).Unix(),
	}

	signed, err := signGmailState(testSecret, want)
	require.NoError(t, err)
	assert.True(t, isGmailState(signed), "the callback tells the two flows apart by this prefix")

	got, err := verifyGmailState(testSecret, signed, time.Now())
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// TestGmailStateRejectsTamperingAndAge: the state is the instruction, so an
// edited one would be an Organiser action nobody authorised — a deadline swapped,
// or a resend re-pointed at somebody else.
func TestGmailStateRejectsTamperingAndAge(t *testing.T) {
	state := gmailSendState{
		Organisation: "test-org",
		Email:        testOrganiserEmail,
		Mode:         services.SendModeRound,
		Deadline:     "Friday",
		Expiry:       time.Now().Add(time.Minute).Unix(),
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
	handler := NewHandler(sendTestStore(), testVolunteers(), apiTestCfg, auth, nil, nil, nil, nil, zap.NewNop()).Routes()

	rec := postSendForm(t, handler, "mode=round&deadline=Friday", organiserCookie())
	require.Equal(t, http.StatusFound, rec.Code)

	consent, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	query := consent.Query()

	assert.Equal(t, "https://www.googleapis.com/auth/gmail.send", query.Get("scope"))
	assert.Equal(t, "true", query.Get("include_granted_scopes"))
	assert.Empty(t, query.Get("prompt"), "a second send in the same session must not re-prompt")
	assert.NotEqual(t, "offline", query.Get("access_type"), "a refresh token would be a standing credential")
	assert.Equal(t, testOrganiserEmail, query.Get("login_hint"))
	assert.True(t, isGmailState(query.Get("state")))
}

// TestSendCallbackRejectsAStateForAnotherOrganiser: the signature proves the
// instruction is ours, the session proves who is presenting it. A state captured
// from one Organiser must not run in another's browser, under their Gmail account.
func TestSendCallbackRejectsAStateForAnotherOrganiser(t *testing.T) {
	auth := newTestAuthenticator()
	mailer := &recordingMailer{}
	handler := sendCallbackServer(NewHandler(sendTestStore(), testVolunteers(), withOrganiser(apiTestCfg, "other@example.com"), auth, nil,
		func(context.Context, *oauth2.Token) (services.GmailClient, error) { return mailer, nil }, nil, nil,
		zap.NewNop()))

	signed, err := signGmailState(testSecret, gmailSendState{
		Organisation: "test-org",
		Email:        "other@example.com",
		Mode:         services.SendModeRound,
		Deadline:     "Friday",
		Expiry:       time.Now().Add(time.Minute).Unix(),
	})
	require.NoError(t, err)

	rec := doRequest(t, handler, http.MethodGet, "/auth/callback?code=x&state="+url.QueryEscape(signed), "", organiserCookie())

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, mailer.recipients())
}

// TestSendCallbackRequiresASession: the state alone is not authority. Someone
// who captured one out of a browser's history must not be able to replay it.
func TestSendCallbackRequiresASession(t *testing.T) {
	mailer := &recordingMailer{}
	handler := sendCallbackServer(NewHandler(sendTestStore(), testVolunteers(), apiTestCfg, newTestAuthenticator(), nil,
		func(context.Context, *oauth2.Token) (services.GmailClient, error) { return mailer, nil }, nil, nil,
		zap.NewNop()))

	signed, err := signGmailState(testSecret, gmailSendState{
		Organisation: "test-org",
		Email:        testOrganiserEmail,
		Mode:         services.SendModeRound,
		Deadline:     "Friday",
		Expiry:       time.Now().Add(time.Minute).Unix(),
	})
	require.NoError(t, err)

	rec := doRequest(t, handler, http.MethodGet, "/auth/callback?code=x&state="+url.QueryEscape(signed), "")

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, mailer.recipients())
}

// TestSendKeepsNoCredentialOnTheSession: the point of asking for gmail.send at
// send time is that nothing survives the send. Whatever the callback does, it
// must not come back having put a credential in the Organiser's cookie jar.
func TestSendKeepsNoCredentialOnTheSession(t *testing.T) {
	handler := newSendTestHandler(sendTestStore(), &recordingMailer{})

	rec := postSendForm(t, handler, "mode=round&deadline=Friday", organiserCookie())

	require.Equal(t, http.StatusFound, rec.Code)
	assert.Empty(t, rec.Result().Cookies(), "starting a send must not write any cookie")
}

// TestSendCallbackRequiresAnOrganiser: the callback is not behind requireLevel,
// so it checks the level itself. Someone moved to the Rota Editors between
// starting a send and coming back from Google may no longer send, even with a
// state signed for them.
func TestSendCallbackRequiresAnOrganiser(t *testing.T) {
	mailer := &recordingMailer{}
	handler := sendCallbackServer(NewHandler(sendTestStore(), testVolunteers(), apiTestCfg, newTestAuthenticator(), nil,
		func(context.Context, *oauth2.Token) (services.GmailClient, error) { return mailer, nil }, nil, nil,
		zap.NewNop()))

	signed, err := signGmailState(testSecret, gmailSendState{
		Organisation: "test-org",
		Email:        testRotaEditorEmail,
		Mode:         services.SendModeRound,
		Deadline:     "Friday",
		Expiry:       time.Now().Add(time.Minute).Unix(),
	})
	require.NoError(t, err)

	rec := doRequest(t, handler, http.MethodGet, "/auth/callback?code=x&state="+url.QueryEscape(signed), "", rotaEditorCookie())

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Empty(t, mailer.recipients())
}

// withOrganiser is org with email added to its Organisers.
func withOrganiser(org *config.Organisation, email string) *config.Organisation {
	copied := *org
	copied.OrganiserEmails = append(append([]string(nil), org.OrganiserEmails...), email)
	return &copied
}

// sendCallbackServer mounts h the way the server does, because a send finishes
// at the shared /auth/callback rather than under the Organisation.
func sendCallbackServer(h *Handler) http.Handler {
	return Mount(h.auth, []*Handler{h}, testFrontend, databaseUp, zap.NewNop())
}

// TestSendCallbackGoesToTheOrganisationInTheState: the callback is shared, so
// the signed state is what says whose send it is — and that Organisation's own
// allowlist decides. An Organiser of one drop-in cannot finish a send for
// another, and a state naming no drop-in this server runs sends nothing.
func TestSendCallbackGoesToTheOrganisationInTheState(t *testing.T) {
	auth := newTestAuthenticator()
	eastMailer, westMailer := &recordingMailer{}, &recordingMailer{}
	east := NewHandler(sendTestStore(), testVolunteers(), eastOrganisation, auth, nil,
		func(context.Context, *oauth2.Token) (services.GmailClient, error) { return eastMailer, nil }, nil, nil, zap.NewNop())
	west := NewHandler(sendTestStore(), testVolunteers(), westOrganisation, auth, nil,
		func(context.Context, *oauth2.Token) (services.GmailClient, error) { return westMailer, nil }, nil, nil, zap.NewNop())
	server := Mount(auth, []*Handler{east, west}, testFrontend, databaseUp, zap.NewNop())
	eastOrganiser := sessionCookieFor(auth, "east@example.com")

	for organisation, want := range map[string]int{
		"west":  http.StatusUnauthorized,
		"north": http.StatusNotFound,
	} {
		signed, err := signGmailState(testSecret, gmailSendState{
			Organisation: organisation,
			Email:        "east@example.com",
			Mode:         services.SendModeRound,
			Deadline:     "Friday",
			Expiry:       time.Now().Add(time.Minute).Unix(),
		})
		require.NoError(t, err)

		rec := doRequest(t, server, http.MethodGet, "/auth/callback?code=x&state="+url.QueryEscape(signed), "", eastOrganiser)
		assert.Equal(t, want, rec.Code, organisation)
	}
	assert.Empty(t, eastMailer.recipients())
	assert.Empty(t, westMailer.recipients())
}
