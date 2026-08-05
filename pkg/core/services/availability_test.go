package services

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// mockAvailabilityStore is an in-memory AvailabilityStore. It reproduces the two
// behaviours the service leans on rather than mocking them away: minting skips a
// volunteer who already holds a request, and reads take the newest generation.
type mockAvailabilityStore struct {
	rotations   []db.Rotation
	shifts      []db.Shift
	requests    []db.AvailabilityRequest
	generations []db.AvailabilityGeneration
	manualPins  []db.ManualPreallocation
	nextID      int
}

func (m *mockAvailabilityStore) GetManualPreallocationsByShiftIDs(_ context.Context, shiftIDs []string) ([]db.ManualPreallocation, error) {
	want := make(map[string]bool, len(shiftIDs))
	for _, id := range shiftIDs {
		want[id] = true
	}
	var out []db.ManualPreallocation
	for _, p := range m.manualPins {
		if want[p.ShiftID] {
			out = append(out, p)
		}
	}
	return out, nil
}

func (m *mockAvailabilityStore) GetRotations(context.Context) ([]db.Rotation, error) {
	return m.rotations, nil
}

func (m *mockAvailabilityStore) GetShiftsByRotaID(_ context.Context, rotaID string) ([]db.Shift, error) {
	var out []db.Shift
	for _, s := range m.shifts {
		if s.RotaID == rotaID {
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *mockAvailabilityStore) MintAvailabilityRequests(_ context.Context, requests []db.AvailabilityRequest) (int, error) {
	inserted := 0
	for _, req := range requests {
		if _, exists := m.findRequest(req.RotaID, req.VolunteerID); exists {
			continue
		}
		m.requests = append(m.requests, req)
		inserted++
	}
	return inserted, nil
}

func (m *mockAvailabilityStore) findRequest(rotaID, volunteerID string) (db.AvailabilityRequest, bool) {
	for _, r := range m.requests {
		if r.RotaID == rotaID && r.VolunteerID == volunteerID {
			return r, true
		}
	}
	return db.AvailabilityRequest{}, false
}

func (m *mockAvailabilityStore) GetAvailabilityRequestsByRotaID(_ context.Context, rotaID string) ([]db.AvailabilityRequest, error) {
	var out []db.AvailabilityRequest
	for _, r := range m.requests {
		if r.RotaID == rotaID {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].VolunteerID < out[j].VolunteerID })
	return out, nil
}

func (m *mockAvailabilityStore) MarkAvailabilityRequestSent(_ context.Context, id string) error {
	for i := range m.requests {
		if m.requests[i].ID == id {
			m.requests[i].SentAt = time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
			return nil
		}
	}
	return fmt.Errorf("no availability request %s", id)
}

func (m *mockAvailabilityStore) GetAvailabilityRequestByToken(_ context.Context, token string) (*db.AvailabilityRequest, error) {
	for i := range m.requests {
		if m.requests[i].Token == token {
			return &m.requests[i], nil
		}
	}
	return nil, nil
}

func (m *mockAvailabilityStore) GetLatestAvailability(_ context.Context, requestIDs []string, cutoff *time.Time) (map[string]db.AvailabilityGeneration, error) {
	want := make(map[string]bool, len(requestIDs))
	for _, id := range requestIDs {
		want[id] = true
	}

	latest := make(map[string]db.AvailabilityGeneration)
	for _, g := range m.generations {
		if !want[g.RequestID] {
			continue
		}
		if cutoff != nil && g.SubmittedAt.After(*cutoff) {
			continue
		}
		if existing, ok := latest[g.RequestID]; ok && !g.SubmittedAt.After(existing.SubmittedAt) {
			continue
		}
		latest[g.RequestID] = g
	}
	return latest, nil
}

func (m *mockAvailabilityStore) InsertAvailabilityResponse(_ context.Context, requestID string, answers []db.ShiftAnswer) (*db.AvailabilityGeneration, error) {
	m.nextID++
	generation := db.AvailabilityGeneration{
		RequestID:   requestID,
		ResponseID:  "response-" + string(rune('a'+m.nextID)),
		SubmittedAt: time.Date(2026, 7, 30, 12, 0, m.nextID, 0, time.UTC),
		Answers:     answers,
	}
	m.generations = append(m.generations, generation)
	return &generation, nil
}

// availabilityFixture is a rota of three weekly shifts with nobody minted into
// it yet. The third date is shut by a rota override, so every test runs against
// a round that has an open/closed distinction to get wrong.
func availabilityFixture() (*mockAvailabilityStore, *config.Config) {
	store := &mockAvailabilityStore{
		rotations: []db.Rotation{{ID: "rota-1", Start: "2026-08-02", End: "2026-08-16", ShiftCount: 3}},
		shifts: []db.Shift{
			{ID: "shift-1", RotaID: "rota-1", Date: "2026-08-02"},
			{ID: "shift-2", RotaID: "rota-1", Date: "2026-08-09"},
			{ID: "shift-3", RotaID: "rota-1", Date: "2026-08-16"},
		},
	}
	cfg := &config.Config{
		// Coverage is counted per Role now, so a config without Roles describes
		// a rota with no Seats and nothing to be short of.
		Roles: testCfg.Roles,
		RotaOverrides: []config.RotaOverride{
			// 16 August, the rota's last date.
			{RRule: "FREQ=YEARLY;BYMONTH=8;BYMONTHDAY=16", Closed: true},
		},
	}
	return store, cfg
}

// availabilityVolunteers is a roster with a two-person group (michael and emma),
// a lone volunteer, and someone who has stopped.
func availabilityVolunteers() *mockVolunteerClient {
	return &mockVolunteerClient{
		volunteers: []model.Volunteer{
			{ID: "michael", FirstName: "Michael", LastName: "Smith", DisplayName: "Michael", Status: "Active", GroupKey: "smiths", Roles: []string{"Service volunteer"}},
			{ID: "emma", FirstName: "Emma", LastName: "Williams", DisplayName: "Emma", Status: "Active", GroupKey: "smiths", Roles: []string{"Service volunteer"}},
			{ID: "aaliyah", FirstName: "Aaliyah", LastName: "Khan", DisplayName: "Aaliyah", Status: "Active", Roles: []string{"Team lead", "Service volunteer"}},
			{ID: "gone", FirstName: "Gordon", LastName: "Past", DisplayName: "Gordon", Status: "Inactive", Roles: []string{"Service volunteer"}},
		},
	}
}

func mintRound(t *testing.T, store *mockAvailabilityStore, volunteers *mockVolunteerClient, cfg *config.Config) *AvailabilityRound {
	t.Helper()
	round, err := MintAvailabilityRound(context.Background(), store, volunteers, cfg, zap.NewNop(), "")
	require.NoError(t, err)
	return round
}

// roundEntries flattens a round back to the per-volunteer grain, which is what
// most of these tests are asserting about.
func roundEntries(round *AvailabilityRound) []AvailabilityEntry {
	entries := make([]AvailabilityEntry, 0)
	for _, g := range round.Groups {
		entries = append(entries, g.Members...)
	}
	return entries
}

func tokenFor(t *testing.T, round *AvailabilityRound, volunteerID string) string {
	t.Helper()
	for _, e := range roundEntries(round) {
		if e.VolunteerID == volunteerID {
			require.NotEmpty(t, e.Token)
			return e.Token
		}
	}
	t.Fatalf("no availability entry for volunteer %s", volunteerID)
	return ""
}

// TestMintAvailabilityRoundAsksEveryActiveVolunteer pins who is in a round: the
// active roster, one request each, with distinct links. Someone who has stopped
// volunteering is on the roster but not in the round.
func TestMintAvailabilityRoundAsksEveryActiveVolunteer(t *testing.T) {
	store, cfg := availabilityFixture()
	round := mintRound(t, store, availabilityVolunteers(), cfg)

	entries := roundEntries(round)
	require.Len(t, entries, 3)
	assert.Equal(t, "rota-1", round.RotaID)

	tokens := map[string]bool{}
	for _, e := range entries {
		assert.False(t, e.Replied)
		assert.False(t, tokens[e.Token], "every volunteer gets their own link")
		tokens[e.Token] = true
	}

	names := []string{}
	for _, e := range entries {
		names = append(names, e.VolunteerName)
	}
	sort.Strings(names)
	assert.Equal(t, []string{"Aaliyah", "Emma", "Michael"}, names,
		"the round is an admin screen, so it names people the way the rota does")
}

// TestMintAvailabilityRoundTwiceIsANoOp proves minting is repeatable: the second
// run keeps the links the first handed out, so anything already distributed
// still works, and only tops the round up with volunteers who joined since.
func TestMintAvailabilityRoundTwiceIsANoOp(t *testing.T) {
	store, cfg := availabilityFixture()
	volunteers := availabilityVolunteers()

	first := mintRound(t, store, volunteers, cfg)
	michaelToken := tokenFor(t, first, "michael")

	// A volunteer joins the roster between rounds.
	volunteers.volunteers = append(volunteers.volunteers, model.Volunteer{
		ID: "nina", FirstName: "Nina", LastName: "Osei", DisplayName: "Nina", Status: "Active", Roles: []string{"Service volunteer"},
	})

	second := mintRound(t, store, volunteers, cfg)
	require.Len(t, roundEntries(second), 4)
	assert.Equal(t, michaelToken, tokenFor(t, second, "michael"), "an existing link must survive a re-mint")
	assert.NotEmpty(t, tokenFor(t, second, "nina"))
}

// TestMintAvailabilityRoundRefusesAnAllocatedRota: links stop working at
// allocation, so minting one then would hand out a link that is dead on arrival.
func TestMintAvailabilityRoundRefusesAnAllocatedRota(t *testing.T) {
	store, cfg := availabilityFixture()
	store.rotations[0].AllocatedDatetime = "2026-07-30T09:00:00Z"

	_, err := MintAvailabilityRound(context.Background(), store, availabilityVolunteers(), cfg, zap.NewNop(), "")
	require.ErrorIs(t, err, ErrConflict)
}

// TestAvailabilityFormLandsOptedIn is the opt-out default of ADR 0004: a
// volunteer opening their link for the first time finds every open shift already
// ticked, and the closed one present but not among them.
func TestAvailabilityFormLandsOptedIn(t *testing.T) {
	store, cfg := availabilityFixture()
	round := mintRound(t, store, availabilityVolunteers(), cfg)

	form, err := GetAvailabilityForm(context.Background(), store, availabilityVolunteers(), cfg, zap.NewNop(),
		tokenFor(t, round, "michael"))
	require.NoError(t, err)

	assert.Equal(t, "Michael Smith", form.VolunteerName)
	assert.Equal(t, []string{"Emma Williams"}, form.GroupMembers, "the form says who else the answer covers")
	assert.False(t, form.Submitted)

	require.Len(t, form.Shifts, 3)
	assert.False(t, form.Shifts[0].Closed)
	assert.True(t, form.Shifts[2].Closed, "the override shuts the last date")
	assert.Equal(t, []string{"shift-1", "shift-2"}, form.SelectedShiftIDs)
}

// TestAvailabilityFormShowsTheSubmittedState: re-opening the link is how a
// volunteer changes their mind, so it must show what they said rather than
// offering the opt-out default again — which would read as an invitation to
// overwrite an answer with a full set.
func TestAvailabilityFormShowsTheSubmittedState(t *testing.T) {
	store, cfg := availabilityFixture()
	volunteers := availabilityVolunteers()
	round := mintRound(t, store, volunteers, cfg)
	token := tokenFor(t, round, "michael")

	_, err := SubmitAvailability(context.Background(), store, volunteers, cfg, zap.NewNop(), token, []string{"shift-2"})
	require.NoError(t, err)

	form, err := GetAvailabilityForm(context.Background(), store, volunteers, cfg, zap.NewNop(), token)
	require.NoError(t, err)
	assert.True(t, form.Submitted)
	assert.False(t, form.SubmittedAt.IsZero())
	assert.Equal(t, []string{"shift-2"}, form.SelectedShiftIDs)

	// Resubmitting appends a generation and the latest wins wholesale.
	_, err = SubmitAvailability(context.Background(), store, volunteers, cfg, zap.NewNop(), token, []string{"shift-1"})
	require.NoError(t, err)

	form, err = GetAvailabilityForm(context.Background(), store, volunteers, cfg, zap.NewNop(), token)
	require.NoError(t, err)
	assert.Equal(t, []string{"shift-1"}, form.SelectedShiftIDs)
}

// TestSubmitNothingIsAnAnswer: "I can't do any of these" is a reply, and must be
// distinguishable from silence — the state the Forms encoding could not express.
func TestSubmitNothingIsAnAnswer(t *testing.T) {
	store, cfg := availabilityFixture()
	volunteers := availabilityVolunteers()
	round := mintRound(t, store, volunteers, cfg)
	token := tokenFor(t, round, "michael")

	form, err := SubmitAvailability(context.Background(), store, volunteers, cfg, zap.NewNop(), token, nil)
	require.NoError(t, err)
	assert.True(t, form.Submitted)
	assert.Empty(t, form.SelectedShiftIDs)

	updated, err := GetAvailabilityRound(context.Background(), store, volunteers, cfg, zap.NewNop(), "")
	require.NoError(t, err)
	for _, e := range roundEntries(updated) {
		if e.VolunteerID == "michael" {
			assert.True(t, e.Replied, "an empty answer is still an answer")
			assert.Empty(t, e.AvailableShiftIDs)
		}
	}
}

// TestSubmitRejectsShiftsNotOnOffer: the form never offers a closed shift or one
// from another rota, so a submission naming either is a broken client. Quietly
// dropping it would record an answer the volunteer did not give.
func TestSubmitRejectsShiftsNotOnOffer(t *testing.T) {
	store, cfg := availabilityFixture()
	volunteers := availabilityVolunteers()
	token := tokenFor(t, mintRound(t, store, volunteers, cfg), "michael")

	for _, shiftID := range []string{"shift-3", "shift-elsewhere"} {
		_, err := SubmitAvailability(context.Background(), store, volunteers, cfg, zap.NewNop(), token, []string{shiftID})
		require.ErrorIs(t, err, ErrInvalidInput, "shift %s must be refused", shiftID)
	}
}

// TestAvailabilityLinkStopsAtAllocation: allocation, not any advertised
// deadline, is the real cutoff, and it closes reading as well as writing.
func TestAvailabilityLinkStopsAtAllocation(t *testing.T) {
	store, cfg := availabilityFixture()
	volunteers := availabilityVolunteers()
	token := tokenFor(t, mintRound(t, store, volunteers, cfg), "michael")

	store.rotations[0].AllocatedDatetime = "2026-07-30T09:00:00Z"

	_, err := GetAvailabilityForm(context.Background(), store, volunteers, cfg, zap.NewNop(), token)
	require.ErrorIs(t, err, ErrGone)

	_, err = SubmitAvailability(context.Background(), store, volunteers, cfg, zap.NewNop(), token, []string{"shift-1"})
	require.ErrorIs(t, err, ErrGone)
}

// TestAvailabilityUnknownTokenIsNotFound keeps "this was never a link" apart
// from "you are too late", which is the difference between the two messages the
// holder of a link can be given.
func TestAvailabilityUnknownTokenIsNotFound(t *testing.T) {
	store, cfg := availabilityFixture()

	_, err := GetAvailabilityForm(context.Background(), store, availabilityVolunteers(), cfg, zap.NewNop(), "not-a-token")
	require.ErrorIs(t, err, ErrNotFound)
}

// TestRoundReportsGroupCover: a group answers as a unit, so a volunteer whose
// partner has already replied is covered, not missing. Without this the roster
// would send an admin chasing an answer it already has.
func TestRoundReportsGroupCover(t *testing.T) {
	store, cfg := availabilityFixture()
	volunteers := availabilityVolunteers()
	round := mintRound(t, store, volunteers, cfg)

	_, err := SubmitAvailability(context.Background(), store, volunteers, cfg, zap.NewNop(),
		tokenFor(t, round, "emma"), []string{"shift-1"})
	require.NoError(t, err)

	updated, err := GetAvailabilityRound(context.Background(), store, volunteers, cfg, zap.NewNop(), "")
	require.NoError(t, err)

	byVolunteer := map[string]AvailabilityEntry{}
	for _, e := range roundEntries(updated) {
		byVolunteer[e.VolunteerID] = e
	}

	assert.True(t, byVolunteer["emma"].Replied)
	assert.Empty(t, byVolunteer["emma"].CoveredBy, "a volunteer who replied is not covered by anyone")

	assert.False(t, byVolunteer["michael"].Replied)
	assert.Equal(t, []string{"Emma"}, byVolunteer["michael"].CoveredBy)

	assert.False(t, byVolunteer["aaliyah"].Replied)
	assert.Empty(t, byVolunteer["aaliyah"].CoveredBy, "an ungrouped volunteer is nobody's partner")
}
