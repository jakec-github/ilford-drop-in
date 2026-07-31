package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/db/dbtest"
)

// TestAvailabilityLoopIntegration drives the whole slice over HTTP against a
// real Postgres, in the order an admin and a volunteer actually meet it: define
// a rota, mint a round, open a link, submit, change your mind. It is the
// acceptance criterion "the whole loop is drivable over HTTP" executed rather
// than described, and the only test that proves the migration, the handlers and
// the latest-generation read agree with each other.
func TestAvailabilityLoopIntegration(t *testing.T) {
	database, _ := dbtest.New(t)
	handler := NewHandler(database, testVolunteers(), apiTestCfg, newTestAuthenticator(), nil, zap.NewNop()).Routes()

	rec := doRequest(t, handler, http.MethodPost, "/rotations", `{"shiftCount":3}`, adminCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	// Minting a round asks every active volunteer, each with their own link.
	round := mintRoundOverHTTP(t, handler)
	require.Len(t, round.Entries, 3)
	require.Len(t, round.Shifts, 3)
	for _, e := range round.Entries {
		assert.False(t, e.Replied)
		assert.Contains(t, e.Link, "/availability/")
	}

	// Minting again is a no-op: the same three volunteers, holding the same
	// links, so anything already distributed still works.
	second := mintRoundOverHTTP(t, handler)
	require.Len(t, second.Entries, 3)
	for i, e := range second.Entries {
		assert.Equal(t, round.Entries[i].Link, e.Link)
	}

	bob := entryFor(t, round, "bob")
	token := tokenFromLink(bob.Link)

	// The volunteer's link is public: no session, no cookie.
	form := formOverHTTP(t, handler, http.MethodGet, token, "")
	assert.Equal(t, "Bob Barnes", form.VolunteerName)
	assert.False(t, form.Submitted)
	assert.Len(t, form.SelectedShiftIDs, 3, "every open shift lands pre-selected")

	// Submitting writes one generation; re-opening the link shows it.
	chosen := []string{round.Shifts[0].ID, round.Shifts[2].ID}
	submitted := formOverHTTP(t, handler, http.MethodPost, token, body(t, chosen))
	assert.True(t, submitted.Submitted)
	assert.ElementsMatch(t, chosen, submitted.SelectedShiftIDs)

	reopened := formOverHTTP(t, handler, http.MethodGet, token, "")
	assert.True(t, reopened.Submitted)
	assert.ElementsMatch(t, chosen, reopened.SelectedShiftIDs)
	assert.NotEmpty(t, reopened.SubmittedAt)

	// Resubmitting appends a generation and the latest wins wholesale — the
	// shift dropped here must not survive from the earlier answer.
	narrowed := []string{round.Shifts[1].ID}
	formOverHTTP(t, handler, http.MethodPost, token, body(t, narrowed))
	reopened = formOverHTTP(t, handler, http.MethodGet, token, "")
	assert.Equal(t, narrowed, reopened.SelectedShiftIDs)

	// The admin roster now reports bob as replied, and the other two as not.
	current := roundOverHTTP(t, handler)
	assert.True(t, entryFor(t, current, "bob").Replied)
	assert.Equal(t, narrowed, entryFor(t, current, "bob").AvailableShiftIDs)
	assert.False(t, entryFor(t, current, "alice").Replied)

	// Submitting nothing is an answer, and reads differently from silence.
	aliceToken := tokenFromLink(entryFor(t, current, "alice").Link)
	empty := formOverHTTP(t, handler, http.MethodPost, aliceToken, `{"shiftIds":[]}`)
	assert.True(t, empty.Submitted)
	assert.Empty(t, empty.SelectedShiftIDs)

	current = roundOverHTTP(t, handler)
	alice := entryFor(t, current, "alice")
	assert.True(t, alice.Replied, "\"none of these\" is a reply")
	assert.Empty(t, alice.AvailableShiftIDs)
	assert.False(t, entryFor(t, current, "charlie").Replied, "nobody submitted for charlie")
}

// TestAvailabilityFormServesThePageToABrowser proves the volunteer's link is a
// single URL for both audiences: a browser navigating to it gets the app, and a
// client asking for JSON gets the payload with the real status code. Without
// this the link in the email and the link an agent drives would have to differ.
func TestAvailabilityFormServesThePageToABrowser(t *testing.T) {
	database, _ := dbtest.New(t)
	handler := NewHandler(database, testVolunteers(), apiTestCfg, newTestAuthenticator(), testFrontend, zap.NewNop()).Routes()

	rec := doRequest(t, handler, http.MethodPost, "/rotations", `{"shiftCount":1}`, adminCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	round := mintRoundOverHTTP(t, handler)
	token := tokenFromLink(round.Entries[0].Link)

	rec = doBrowserRequest(t, handler, "/availability/"+token)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "<html>app</html>", rec.Body.String(), "a browser gets the app shell")

	// An unknown token still renders the app, which reports the dead link from
	// its own fetch — the same shape as every other client-side route.
	rec = doBrowserRequest(t, handler, "/availability/not-a-token")
	assert.Equal(t, http.StatusOK, rec.Code)

	// That fetch, and anything else consuming JSON, gets the honest status.
	rec = doRequest(t, handler, http.MethodGet, "/availability/not-a-token", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// doBrowserRequest issues a GET carrying the Accept header a browser sends when
// a person navigates to a URL, which is what distinguishes wanting the page from
// wanting the payload.
func doBrowserRequest(t *testing.T, handler http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func mintRoundOverHTTP(t *testing.T, handler http.Handler) availabilityRoundResponse {
	t.Helper()
	rec := doRequest(t, handler, http.MethodPost, "/availability-rounds", `{}`, adminCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var round availabilityRoundResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &round))
	return round
}

func roundOverHTTP(t *testing.T, handler http.Handler) availabilityRoundResponse {
	t.Helper()
	rec := doRequest(t, handler, http.MethodGet, "/availability-rounds", "", adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var round availabilityRoundResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &round))
	return round
}

// formOverHTTP hits the volunteer's link with no session at all, which is the
// point: the link is the identity.
func formOverHTTP(t *testing.T, handler http.Handler, method, token, payload string) availabilityFormResponse {
	t.Helper()
	rec := doRequest(t, handler, method, "/availability/"+token, payload)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var form availabilityFormResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &form))
	return form
}

func entryFor(t *testing.T, round availabilityRoundResponse, volunteerID string) availabilityEntryResponse {
	t.Helper()
	for _, e := range round.Entries {
		if e.VolunteerID == volunteerID {
			return e
		}
	}
	t.Fatalf("no availability entry for volunteer %s", volunteerID)
	return availabilityEntryResponse{}
}

func tokenFromLink(link string) string {
	_, token, _ := strings.Cut(link, "/availability/")
	return token
}

func body(t *testing.T, shiftIDs []string) string {
	t.Helper()
	encoded, err := json.Marshal(submitAvailabilityRequest{ShiftIDs: shiftIDs})
	require.NoError(t, err)
	return string(encoded)
}
