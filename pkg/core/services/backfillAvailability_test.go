package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/clients/formsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// writtenGeneration is one call the service made to the backfill writer, which
// is the only thing these tests care about: what history it decided to record.
type writtenGeneration struct {
	requestID   string
	submittedAt time.Time
	shiftIDs    []string
}

type mockBackfillStore struct {
	rotations []db.Rotation
	legacy    []db.AvailabilityRequest
	shifts    map[string][]db.Shift
	// v2 requests that already exist, keyed by rota id. Minting appends here, so
	// the read-back after a mint sees what the mint created.
	v2 map[string][]db.AvailabilityRequestV2

	minted  []db.AvailabilityRequestV2
	written []writtenGeneration
	// submitted_at values the store already holds, so a second run can report
	// them as duplicates the way Postgres does.
	existing map[string]bool
}

func (m *mockBackfillStore) GetRotations(context.Context) ([]db.Rotation, error) {
	return m.rotations, nil
}

func (m *mockBackfillStore) GetAvailabilityRequests(context.Context) ([]db.AvailabilityRequest, error) {
	return m.legacy, nil
}

func (m *mockBackfillStore) GetShiftsByRotaID(_ context.Context, rotaID string) ([]db.Shift, error) {
	return m.shifts[rotaID], nil
}

func (m *mockBackfillStore) MintAvailabilityRequests(_ context.Context, requests []db.AvailabilityRequestV2) (int, error) {
	inserted := 0
	for _, req := range requests {
		held := false
		for _, existing := range m.v2[req.RotaID] {
			if existing.VolunteerID == req.VolunteerID {
				held = true
				break
			}
		}
		if held {
			continue
		}
		m.v2[req.RotaID] = append(m.v2[req.RotaID], req)
		m.minted = append(m.minted, req)
		inserted++
	}
	return inserted, nil
}

func (m *mockBackfillStore) GetAvailabilityRequestsV2ByRotaID(_ context.Context, rotaID string) ([]db.AvailabilityRequestV2, error) {
	return m.v2[rotaID], nil
}

func (m *mockBackfillStore) InsertBackfilledAvailabilityResponse(_ context.Context, requestID string, submittedAt time.Time, answers []db.ShiftAnswer) (bool, error) {
	key := requestID + "@" + submittedAt.Format(time.RFC3339Nano)
	if m.existing[key] {
		return false, nil
	}
	m.existing[key] = true

	shiftIDs := make([]string, 0, len(answers))
	for _, a := range answers {
		shiftIDs = append(shiftIDs, a.ShiftID)
	}
	m.written = append(m.written, writtenGeneration{requestID: requestID, submittedAt: submittedAt, shiftIDs: shiftIDs})
	return true, nil
}

type mockFormResponseLister struct {
	byFormID map[string][]formsclient.SubmittedFormResponse
	calls    []string
}

func (m *mockFormResponseLister) ListFormResponses(formID string, _ string, _ []time.Time) ([]formsclient.SubmittedFormResponse, error) {
	m.calls = append(m.calls, formID)
	return m.byFormID[formID], nil
}

// backfillFixture is one allocated rota with three weekly shifts and two legacy
// requests: alice's was emailed, bob's was created but never sent.
func backfillFixture() *mockBackfillStore {
	return &mockBackfillStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2025-03-03", ShiftCount: 3, AllocatedDatetime: "2025-03-01T00:00:00Z"},
		},
		legacy: []db.AvailabilityRequest{
			{ID: "legacy-alice", RotaID: "rota-1", VolunteerID: "alice", FormID: "form-alice", FormSent: true},
			{ID: "legacy-bob", RotaID: "rota-1", VolunteerID: "bob", FormID: "form-bob", FormSent: false},
		},
		shifts: map[string][]db.Shift{
			"rota-1": {
				{ID: "shift-1", RotaID: "rota-1", Date: "2025-03-03"},
				{ID: "shift-2", RotaID: "rota-1", Date: "2025-03-10"},
				{ID: "shift-3", RotaID: "rota-1", Date: "2025-03-17"},
			},
		},
		v2:       map[string][]db.AvailabilityRequestV2{},
		existing: map[string]bool{},
	}
}

func submitted(at time.Time, availableDates ...string) formsclient.SubmittedFormResponse {
	return formsclient.SubmittedFormResponse{
		SubmittedAt: at,
		Response: &formsclient.FormResponse{
			HasResponded:   true,
			AvailableDates: availableDates,
		},
	}
}

// TestBackfillWritesEachResponseAtItsOwnTime is the fidelity criterion: two
// submissions against one form become two generations at their original times,
// so a later read bounded by the rota's allocated_datetime picks the same answer
// Forms would have returned.
func TestBackfillWritesEachResponseAtItsOwnTime(t *testing.T) {
	store := backfillFixture()
	first := time.Date(2025, 2, 20, 10, 0, 0, 0, time.UTC)
	second := time.Date(2025, 2, 25, 18, 30, 0, 0, time.UTC)
	forms := &mockFormResponseLister{byFormID: map[string][]formsclient.SubmittedFormResponse{
		"form-alice": {
			submitted(first, "Mon Mar 3 2025", "Mon Mar 10 2025"),
			submitted(second, "Mon Mar 17 2025"),
		},
	}}

	result, err := BackfillAvailabilityFromForms(context.Background(), store, forms, zap.NewNop(), false)
	require.NoError(t, err)

	require.Len(t, store.written, 2)
	assert.Equal(t, first, store.written[0].submittedAt)
	assert.Equal(t, []string{"shift-1", "shift-2"}, store.written[0].shiftIDs)
	assert.Equal(t, second, store.written[1].submittedAt)
	assert.Equal(t, []string{"shift-3"}, store.written[1].shiftIDs)
	assert.Equal(t, 2, result.GenerationsWritten)

	require.Len(t, store.minted, 1)
	assert.Equal(t, "alice", store.minted[0].VolunteerID)
	assert.NotEmpty(t, store.minted[0].Token, "a backfilled request still needs a token to satisfy the schema")
	assert.Equal(t, store.minted[0].ID, store.written[0].requestID)
}

// TestBackfillSkipsUnsentLegacyRequests: a form that was created but never
// emailed read as "not asked" in the historical report, and minting a request
// for it would silently promote that to "asked and never replied".
func TestBackfillSkipsUnsentLegacyRequests(t *testing.T) {
	store := backfillFixture()
	forms := &mockFormResponseLister{byFormID: map[string][]formsclient.SubmittedFormResponse{
		"form-alice": {submitted(time.Date(2025, 2, 20, 10, 0, 0, 0, time.UTC), "Mon Mar 3 2025")},
		"form-bob":   {submitted(time.Date(2025, 2, 21, 10, 0, 0, 0, time.UTC), "Mon Mar 10 2025")},
	}}

	_, err := BackfillAvailabilityFromForms(context.Background(), store, forms, zap.NewNop(), false)
	require.NoError(t, err)

	require.Len(t, store.minted, 1)
	assert.Equal(t, "alice", store.minted[0].VolunteerID)
	assert.NotContains(t, forms.calls, "form-bob", "an unsent form is not read at all")
}

// TestBackfillMintsARequestForASilentVolunteer: someone who was emailed and
// never answered must still get a request row, or the report cannot tell them
// apart from someone nobody asked.
func TestBackfillMintsARequestForASilentVolunteer(t *testing.T) {
	store := backfillFixture()
	forms := &mockFormResponseLister{byFormID: map[string][]formsclient.SubmittedFormResponse{
		"form-alice": nil,
	}}

	result, err := BackfillAvailabilityFromForms(context.Background(), store, forms, zap.NewNop(), false)
	require.NoError(t, err)

	assert.Len(t, store.minted, 1)
	assert.Empty(t, store.written)
	assert.Equal(t, 1, result.RequestsMinted)
	assert.Equal(t, 0, result.GenerationsWritten)
}

// TestBackfillIsIdempotent is the ticket's headline criterion. The second run
// re-reads the same forms and offers the same generations; nothing is written
// twice and no volunteer is re-tokened.
func TestBackfillIsIdempotent(t *testing.T) {
	store := backfillFixture()
	at := time.Date(2025, 2, 20, 10, 0, 0, 0, time.UTC)
	forms := &mockFormResponseLister{byFormID: map[string][]formsclient.SubmittedFormResponse{
		"form-alice": {submitted(at, "Mon Mar 3 2025")},
	}}
	ctx := context.Background()

	first, err := BackfillAvailabilityFromForms(ctx, store, forms, zap.NewNop(), false)
	require.NoError(t, err)
	require.Equal(t, 1, first.GenerationsWritten)
	firstToken := store.minted[0].Token

	second, err := BackfillAvailabilityFromForms(ctx, store, forms, zap.NewNop(), false)
	require.NoError(t, err)

	assert.Equal(t, 0, second.GenerationsWritten)
	assert.Equal(t, 1, second.GenerationsSkipped)
	assert.Equal(t, 0, second.RequestsMinted)
	assert.Len(t, store.written, 1)
	require.Len(t, store.v2["rota-1"], 1)
	assert.Equal(t, firstToken, store.v2["rota-1"][0].Token, "an existing link must survive a re-run")
}

// TestBackfillDryRunWritesNothing: the command runs once against production
// where the source data is about to be deleted, so it has to be inspectable
// before it is trusted.
func TestBackfillDryRunWritesNothing(t *testing.T) {
	store := backfillFixture()
	forms := &mockFormResponseLister{byFormID: map[string][]formsclient.SubmittedFormResponse{
		"form-alice": {submitted(time.Date(2025, 2, 20, 10, 0, 0, 0, time.UTC), "Mon Mar 3 2025")},
	}}

	result, err := BackfillAvailabilityFromForms(context.Background(), store, forms, zap.NewNop(), true)
	require.NoError(t, err)

	assert.Empty(t, store.minted)
	assert.Empty(t, store.written)
	assert.Equal(t, 1, result.RequestsMinted, "a dry run reports what it would have written")
	assert.Equal(t, 1, result.GenerationsWritten)
}

// TestBackfillRejectsADateWithNoShift: the answers are being frozen into
// permanent rows, so a date that does not resolve is a silent loss of
// availability. It aborts instead, which the dry run surfaces before any write.
func TestBackfillRejectsADateWithNoShift(t *testing.T) {
	store := backfillFixture()
	forms := &mockFormResponseLister{byFormID: map[string][]formsclient.SubmittedFormResponse{
		"form-alice": {submitted(time.Date(2025, 2, 20, 10, 0, 0, 0, time.UTC), "Mon Apr 7 2025")},
	}}

	_, err := BackfillAvailabilityFromForms(context.Background(), store, forms, zap.NewNop(), false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Mon Apr 7 2025")
}

// TestBackfillSkipsAnUndatedResponse: a response Forms cannot date cannot be
// placed against a cut-off. It is reported rather than guessed at.
func TestBackfillSkipsAnUndatedResponse(t *testing.T) {
	store := backfillFixture()
	forms := &mockFormResponseLister{byFormID: map[string][]formsclient.SubmittedFormResponse{
		"form-alice": {submitted(time.Time{}, "Mon Mar 3 2025")},
	}}

	result, err := BackfillAvailabilityFromForms(context.Background(), store, forms, zap.NewNop(), false)
	require.NoError(t, err)

	assert.Empty(t, store.written)
	assert.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "alice")
}
