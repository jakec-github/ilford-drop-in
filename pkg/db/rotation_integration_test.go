package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
	"github.com/jakechorley/ilford-drop-in/pkg/db/dbtest"
)

// TestGetRotaInFlight covers the read behind the one-rota-in-flight rule: the
// unallocated Rotation, or nothing at all (issue #139).
func TestGetRotaInFlight(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	// Nothing defined is nothing in flight, and not an error: it is the
	// ordinary state between one rota going out and the next being defined.
	inFlight, err := database.GetRotaInFlight(ctx)
	require.NoError(t, err)
	assert.Nil(t, inFlight)

	allocated := &db.Rotation{ID: uuid.New().String()}
	allocatedShift := dbtest.Shift(allocated.ID, "2026-07-05")
	require.NoError(t, database.InsertDefinedRota(ctx, allocated, []db.Shift{allocatedShift}, nil, nil))

	live := &db.Rotation{ID: uuid.New().String()}
	liveShifts := []db.Shift{
		dbtest.Shift(live.ID, "2026-08-02"),
		dbtest.Shift(live.ID, "2026-08-09"),
	}
	require.NoError(t, database.InsertDefinedRota(ctx, live, liveShifts, nil, nil))

	// Both are unallocated so far, and the earlier one is the one in flight.
	inFlight, err = database.GetRotaInFlight(ctx)
	require.NoError(t, err)
	require.NotNil(t, inFlight)
	assert.Equal(t, allocated.ID, inFlight.ID)

	require.NoError(t, database.InsertAllocationsAndSetAllocated(ctx,
		[]db.Allocation{{ID: uuid.New().String(), ShiftID: allocatedShift.ID, Role: "Service volunteer", VolunteerID: "alice"}},
		allocated.ID, time.Now()))

	// Now only the later rota is unallocated, and its span and size are derived
	// from its shifts exactly as GetRotations derives them (ADR 0001).
	inFlight, err = database.GetRotaInFlight(ctx)
	require.NoError(t, err)
	require.NotNil(t, inFlight)
	assert.Equal(t, live.ID, inFlight.ID)
	assert.Equal(t, "2026-08-02", inFlight.Start)
	assert.Equal(t, "2026-08-09", inFlight.End)
	assert.Equal(t, 2, inFlight.ShiftCount)
	assert.Equal(t, 0, inFlight.Asked, "nobody has been asked yet")
	assert.Equal(t, 0, inFlight.Sent)
	assert.Equal(t, 0, inFlight.Replied)
}

// The round counts are in volunteers, not emails or submissions: a resubmission
// is the same person answering twice, and the number a discard confirmation
// quotes is how many people's answers it destroys.
func TestGetRotaInFlight_CountsTheRoundInVolunteers(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	rota := &db.Rotation{ID: uuid.New().String()}
	shift := dbtest.Shift(rota.ID, "2026-08-02")
	require.NoError(t, database.InsertDefinedRota(ctx, rota, []db.Shift{shift}, nil, nil))

	requests := []db.AvailabilityRequest{
		{ID: uuid.New().String(), RotaID: rota.ID, VolunteerID: "alice", Token: "token-alice"},
		{ID: uuid.New().String(), RotaID: rota.ID, VolunteerID: "bob", Token: "token-bob"},
		{ID: uuid.New().String(), RotaID: rota.ID, VolunteerID: "carol", Token: "token-carol"},
	}
	minted, err := database.MintAvailabilityRequests(ctx, requests)
	require.NoError(t, err)
	require.Equal(t, 3, minted)

	require.NoError(t, database.MarkAvailabilityRequestSent(ctx, requests[0].ID))
	require.NoError(t, database.MarkAvailabilityRequestSent(ctx, requests[1].ID))

	// Alice answers twice; Bob once. Carol has her link and has said nothing.
	answers := []db.ShiftAnswer{{ShiftID: shift.ID, Answer: "YES"}}
	_, err = database.InsertAvailabilityResponse(ctx, requests[0].ID, answers)
	require.NoError(t, err)
	_, err = database.InsertAvailabilityResponse(ctx, requests[0].ID, answers)
	require.NoError(t, err)
	_, err = database.InsertAvailabilityResponse(ctx, requests[1].ID, nil)
	require.NoError(t, err)

	inFlight, err := database.GetRotaInFlight(ctx)
	require.NoError(t, err)
	require.NotNil(t, inFlight)
	assert.Equal(t, 3, inFlight.Asked)
	assert.Equal(t, 2, inFlight.Sent)
	assert.Equal(t, 2, inFlight.Replied, "Alice's two submissions are one volunteer, and answering nothing is answering")
}

// TestDiscardRota is the release valve the one-rota-in-flight rule requires:
// everything hanging off an unallocated Rotation goes with it, in one
// transaction, and nothing belonging to any other rota is touched.
func TestDiscardRota(t *testing.T) {
	database, _ := dbtest.New(t)
	dbtest.SeedRoles(t, database)
	ctx := context.Background()

	roles, err := database.ListRoles(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, roles)

	// A neighbour rota, so the deletes can be shown to be bounded by rota id
	// rather than emptying the tables.
	keeper := &db.Rotation{ID: uuid.New().String()}
	keeperShift := dbtest.Shift(keeper.ID, "2026-07-05")
	keeperPin := db.Preallocation{ID: uuid.New().String(), ShiftID: keeperShift.ID, Role: roles[0].Name, VolunteerID: "alice"}
	keeperSeats := []db.ShiftRequirement{{ShiftID: keeperShift.ID, RoleID: roles[0].ID, Seats: 2}}
	require.NoError(t, database.InsertDefinedRota(ctx, keeper, []db.Shift{keeperShift}, []db.Preallocation{keeperPin}, keeperSeats))
	keeperRequests := []db.AvailabilityRequest{{ID: uuid.New().String(), RotaID: keeper.ID, VolunteerID: "alice", Token: "keeper-token"}}
	_, err = database.MintAvailabilityRequests(ctx, keeperRequests)
	require.NoError(t, err)
	_, err = database.InsertAvailabilityResponse(ctx, keeperRequests[0].ID, []db.ShiftAnswer{{ShiftID: keeperShift.ID, Answer: "YES"}})
	require.NoError(t, err)

	doomed := &db.Rotation{ID: uuid.New().String()}
	doomedShifts := []db.Shift{
		dbtest.Shift(doomed.ID, "2026-08-02"),
		dbtest.Shift(doomed.ID, "2026-08-09"),
	}
	doomedPins := []db.Preallocation{
		{ID: uuid.New().String(), ShiftID: doomedShifts[0].ID, Role: roles[0].Name, VolunteerID: "bob"},
		{ID: uuid.New().String(), ShiftID: doomedShifts[1].ID, Role: roles[0].Name, CustomValue: "St John's team"},
	}
	var doomedSeats []db.ShiftRequirement
	for _, s := range doomedShifts {
		doomedSeats = append(doomedSeats, db.ShiftRequirement{ShiftID: s.ID, RoleID: roles[0].ID, Seats: 3})
	}
	require.NoError(t, database.InsertDefinedRota(ctx, doomed, doomedShifts, doomedPins, doomedSeats))

	doomedRequests := []db.AvailabilityRequest{
		{ID: uuid.New().String(), RotaID: doomed.ID, VolunteerID: "alice", Token: "doomed-alice"},
		{ID: uuid.New().String(), RotaID: doomed.ID, VolunteerID: "bob", Token: "doomed-bob"},
	}
	_, err = database.MintAvailabilityRequests(ctx, doomedRequests)
	require.NoError(t, err)
	_, err = database.InsertAvailabilityResponse(ctx, doomedRequests[0].ID,
		[]db.ShiftAnswer{{ShiftID: doomedShifts[0].ID, Answer: "YES"}, {ShiftID: doomedShifts[1].ID, Answer: "YES"}})
	require.NoError(t, err)

	// A Draft Rota Allocation on each. Both cascade rather than being deleted
	// here — the draft from its Rotation, its Seats from their Shifts (issue
	// #141) — so this is what proves the cascade is really wired up and a
	// discard does not leave a draft naming a rota that is not there.
	require.NoError(t, database.ReplaceDraftRotaAllocation(ctx,
		db.DraftRotaAllocation{RotaID: keeper.ID, SolvedAt: time.Now(), Success: true, SolverStatus: "OPTIMAL", Diagnostics: []byte(`{}`)},
		[]db.DraftAllocation{{ID: uuid.New().String(), ShiftID: keeperShift.ID, Role: roles[0].Name, VolunteerID: "alice"}}))
	require.NoError(t, database.ReplaceDraftRotaAllocation(ctx,
		db.DraftRotaAllocation{RotaID: doomed.ID, SolvedAt: time.Now(), Success: true, SolverStatus: "OPTIMAL", Diagnostics: []byte(`{}`)},
		[]db.DraftAllocation{
			{ID: uuid.New().String(), ShiftID: doomedShifts[0].ID, Role: roles[0].Name, VolunteerID: "bob"},
			{ID: uuid.New().String(), ShiftID: doomedShifts[1].ID, Role: roles[0].Name, VolunteerID: "alice"},
		}))

	discarded, err := database.DiscardRota(ctx, doomed.ID)
	require.NoError(t, err)
	assert.True(t, discarded)

	// The rota is gone, and with it its shifts, their Shapes, its pins and its
	// whole round.
	rotations, err := database.GetRotations(ctx)
	require.NoError(t, err)
	require.Len(t, rotations, 1)
	assert.Equal(t, keeper.ID, rotations[0].ID)

	shifts, err := database.GetShiftsByRotaID(ctx, doomed.ID)
	require.NoError(t, err)
	assert.Empty(t, shifts)

	shapes, err := database.GetShiftShapes(ctx, []string{doomedShifts[0].ID, doomedShifts[1].ID})
	require.NoError(t, err)
	assert.Empty(t, shapes, "a Shape is part of its Shift and cascades with it")

	pins, err := database.GetPreallocationsByShiftIDs(ctx, []string{doomedShifts[0].ID, doomedShifts[1].ID})
	require.NoError(t, err)
	assert.Empty(t, pins)

	requests, err := database.GetAvailabilityRequestsByRotaID(ctx, doomed.ID)
	require.NoError(t, err)
	assert.Empty(t, requests)

	stale, err := database.GetAvailabilityRequestByToken(ctx, "doomed-alice")
	require.NoError(t, err)
	assert.Nil(t, stale, "a link to a discarded rota resolves to nothing")

	draft, err := database.GetDraftRotaAllocation(ctx, doomed.ID)
	require.NoError(t, err)
	assert.Nil(t, draft, "a Draft Rota Allocation does not outlive the Rotation it drafts")
	draftSeats, err := database.GetDraftAllocationsByShiftIDs(ctx, []string{doomedShifts[0].ID, doomedShifts[1].ID})
	require.NoError(t, err)
	assert.Empty(t, draftSeats)

	// The neighbour is untouched, top to bottom.
	keptShifts, err := database.GetShiftsByRotaID(ctx, keeper.ID)
	require.NoError(t, err)
	require.Len(t, keptShifts, 1)
	keptShapes, err := database.GetShiftShapes(ctx, []string{keeperShift.ID})
	require.NoError(t, err)
	assert.Len(t, keptShapes[keeperShift.ID], 1)
	keptPins, err := database.GetPreallocationsByShiftIDs(ctx, []string{keeperShift.ID})
	require.NoError(t, err)
	assert.Len(t, keptPins, 1)
	keptRequests, err := database.GetAvailabilityRequestsByRotaID(ctx, keeper.ID)
	require.NoError(t, err)
	require.Len(t, keptRequests, 1)
	keptAnswers, err := database.GetLatestAvailability(ctx, []string{keptRequests[0].ID}, nil)
	require.NoError(t, err)
	assert.Len(t, keptAnswers, 1, "the neighbour's answers survive")
	keptDraft, err := database.GetDraftRotaAllocation(ctx, keeper.ID)
	require.NoError(t, err)
	assert.NotNil(t, keptDraft, "and so does its draft")
	keptDraftSeats, err := database.GetDraftAllocationsByShiftIDs(ctx, []string{keeperShift.ID})
	require.NoError(t, err)
	assert.Len(t, keptDraftSeats, 1)

	// Nothing is in flight but the neighbour now.
	inFlight, err := database.GetRotaInFlight(ctx)
	require.NoError(t, err)
	require.NotNil(t, inFlight)
	assert.Equal(t, keeper.ID, inFlight.ID)
}

// An allocated rota is never discarded. The check runs inside the transaction
// holding the rota's row lock — the same lock allocation takes — so it cannot be
// overtaken by an allocation landing a moment later.
func TestDiscardRota_RefusesAnAllocatedRota(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	rota := &db.Rotation{ID: uuid.New().String()}
	shift := dbtest.Shift(rota.ID, "2026-08-02")
	require.NoError(t, database.InsertDefinedRota(ctx, rota, []db.Shift{shift}, nil, nil))
	require.NoError(t, database.InsertAllocationsAndSetAllocated(ctx,
		[]db.Allocation{{ID: uuid.New().String(), ShiftID: shift.ID, Role: "Service volunteer", VolunteerID: "alice"}},
		rota.ID, time.Now()))

	discarded, err := database.DiscardRota(ctx, rota.ID)
	assert.False(t, discarded)
	require.ErrorIs(t, err, db.ErrRotaAllocated)

	// Nothing was taken on the way to the refusal.
	rotations, err := database.GetRotations(ctx)
	require.NoError(t, err)
	assert.Len(t, rotations, 1)
	allocations, err := database.GetAllocationsByShiftIDs(ctx, []string{shift.ID})
	require.NoError(t, err)
	assert.Len(t, allocations, 1)
}

// An unknown id is a miss rather than an error: the caller answers 404, and a
// second discard of the same rota is the ordinary way to reach this.
func TestDiscardRota_UnknownRota(t *testing.T) {
	database, _ := dbtest.New(t)

	discarded, err := database.DiscardRota(context.Background(), uuid.New().String())
	require.NoError(t, err)
	assert.False(t, discarded)
}
