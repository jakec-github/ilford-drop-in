package api

import (
	"encoding/json"
	"net/http"
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
	dbtest.SeedRoles(t, database)
	// What a Shift asks for is the stored default Shape (#129), so the round's
	// coverage numbers are zero until one is stated.
	dbtest.SeedDefaultShape(t, database)
	handler := NewHandler(database, testVolunteers(), apiTestCfg, newTestAuthenticator(), nil, nil, zap.NewNop()).Routes()

	rec := doRequest(t, handler, http.MethodPost, "/api/rotations", `{"shiftCount":3}`, adminCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	// Minting a round asks every active volunteer, each with their own link.
	round := mintRoundOverHTTP(t, handler)
	require.Len(t, roundMembers(round), 3)
	require.Len(t, round.Shifts, 3)
	for _, e := range roundMembers(round) {
		assert.False(t, e.Replied)
		assert.Contains(t, e.Link, "/availability/", "the link is the page a volunteer opens, not the endpoint behind it")
	}
	// The Roles ride along with the round, so a reader can ask which of these
	// people could lead without fetching the roster and joining it back.
	assert.Equal(t, []string{"Team lead", "Service volunteer"}, entryFor(t, round, "alice").Roles)
	assert.Equal(t, []string{"Service volunteer"}, entryFor(t, round, "bob").Roles)

	// Nobody has answered, so nothing is available for anything yet.
	for _, s := range round.Shifts {
		assert.Equal(t, 0, s.Available)
		assert.Equal(t, 0, leadCoverage(t, s).Available)
		assert.Equal(t, 1, leadCoverage(t, s).Needed, "every open shift still wants a lead")
	}

	// Minting again is a no-op: the same three volunteers, holding the same
	// links, so anything already distributed still works.
	second := mintRoundOverHTTP(t, handler)
	require.Len(t, roundMembers(second), 3)
	for i, e := range roundMembers(second) {
		assert.Equal(t, roundMembers(round)[i].Link, e.Link)
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

	// And the shift bob is available for now counts him, while the two he
	// dropped count nobody. Alice is a team lead, so her silence leaves every
	// date without cover.
	for _, s := range current.Shifts {
		if s.ID == narrowed[0] {
			assert.Equal(t, 1, s.Available, "bob is available for this one")
		} else {
			assert.Equal(t, 0, s.Available)
		}
		assert.Equal(t, 0, leadCoverage(t, s).Available)
	}

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

	// Alice is the only team lead, so her answer is what decides whether a date
	// has cover. She holds the uncapped Role too, so she is counted for both —
	// she could take either Seat, though only one of them.
	formOverHTTP(t, handler, http.MethodPost, aliceToken, body(t, narrowed))
	current = roundOverHTTP(t, handler)
	for _, s := range current.Shifts {
		if s.ID == narrowed[0] {
			assert.Equal(t, 1, leadCoverage(t, s).Available)
			assert.Equal(t, 2, s.Available, "alice and bob could both take an ordinary Seat")
		} else {
			assert.Equal(t, 0, leadCoverage(t, s).Available)
			assert.Equal(t, 0, s.Available)
		}
	}
}

// leadCoverage picks the Team lead Seat out of a shift's per-Role picture.
func leadCoverage(t *testing.T, shift availabilityCoverageResponse) availabilityRoleCoverage {
	t.Helper()
	for _, r := range shift.Roles {
		if r.Role == "Team lead" {
			return r
		}
	}
	t.Fatalf("no Team lead coverage for shift %s", shift.ID)
	return availabilityRoleCoverage{}
}

// TestAvailabilityLinkOpensThePage proves the two halves of a volunteer's link
// line up: the URL that goes in the email renders the app on a hard navigation,
// and the payload the app then fetches sits under /api answering with real
// status codes. Get this wrong and the link either shows JSON to a volunteer or
// hides a dead token behind a 200.
func TestAvailabilityLinkOpensThePage(t *testing.T) {
	database, _ := dbtest.New(t)
	dbtest.SeedRoles(t, database)
	handler := NewHandler(database, testVolunteers(), apiTestCfg, newTestAuthenticator(), testFrontend, nil, zap.NewNop()).Routes()

	rec := doRequest(t, handler, http.MethodPost, "/api/rotations", `{"shiftCount":1}`, adminCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	round := mintRoundOverHTTP(t, handler)
	token := tokenFromLink(roundMembers(round)[0].Link)

	rec = doRequest(t, handler, http.MethodGet, "/availability/"+token, "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "<html>app</html>", rec.Body.String(), "the link opens the app shell")

	// An unknown token still renders the app, which reports the dead link from
	// its own fetch — the same shape as every other client-side route.
	rec = doRequest(t, handler, http.MethodGet, "/availability/not-a-token", "")
	assert.Equal(t, http.StatusOK, rec.Code)

	// That fetch, and anything else consuming JSON, gets the honest status.
	rec = doRequest(t, handler, http.MethodGet, "/api/availability/not-a-token", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func mintRoundOverHTTP(t *testing.T, handler http.Handler) availabilityRoundResponse {
	t.Helper()
	rec := doRequest(t, handler, http.MethodPost, "/api/availability-rounds", `{}`, adminCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var round availabilityRoundResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &round))
	return round
}

func roundOverHTTP(t *testing.T, handler http.Handler) availabilityRoundResponse {
	t.Helper()
	rec := doRequest(t, handler, http.MethodGet, "/api/availability-rounds", "", adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var round availabilityRoundResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &round))
	return round
}

// formOverHTTP hits the volunteer's link with no session at all, which is the
// point: the link is the identity.
func formOverHTTP(t *testing.T, handler http.Handler, method, token, payload string) availabilityFormResponse {
	t.Helper()
	rec := doRequest(t, handler, method, "/api/availability/"+token, payload)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var form availabilityFormResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &form))
	return form
}

// roundMembers flattens a round back to the per-volunteer grain the links live
// at, which is what most of these assertions are about.
func roundMembers(round availabilityRoundResponse) []availabilityEntryResponse {
	members := make([]availabilityEntryResponse, 0)
	for _, g := range round.Groups {
		members = append(members, g.Members...)
	}
	return members
}

func entryFor(t *testing.T, round availabilityRoundResponse, volunteerID string) availabilityEntryResponse {
	t.Helper()
	for _, e := range roundMembers(round) {
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
