package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services/utils"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// mockDB is a test double for db.DB on the defining path.
type mockDB struct {
	testRoleStore

	rotations       []db.Rotation
	defaults        db.RotaDefaults
	shape           []db.DefaultShapeSeat
	standing        []db.StandingPreallocation
	insertedRotas   []*db.Rotation
	insertedShifts  [][]db.Shift
	insertedPins    [][]db.Preallocation
	insertedSeats   [][]db.ShiftRequirement
	mintedRequests  []db.AvailabilityRequest
	getRotationsErr error
	insertErr       error
	mintErr         error
}

func (m *mockDB) MintAvailabilityRequests(ctx context.Context, requests []db.AvailabilityRequest) (int, error) {
	if m.mintErr != nil {
		return 0, m.mintErr
	}
	m.mintedRequests = append(m.mintedRequests, requests...)
	return len(requests), nil
}

func (m *mockDB) GetRotations(ctx context.Context) ([]db.Rotation, error) {
	if m.getRotationsErr != nil {
		return nil, m.getRotationsErr
	}
	return m.rotations, nil
}

func (m *mockDB) GetRotaDefaults(ctx context.Context) (db.RotaDefaults, error) {
	return m.defaults, nil
}

func (m *mockDB) GetDefaultShape(ctx context.Context) ([]db.DefaultShapeSeat, error) {
	return m.shape, nil
}

func (m *mockDB) GetStandingPreallocations(ctx context.Context) ([]db.StandingPreallocation, error) {
	return m.standing, nil
}

func (m *mockDB) InsertDefinedRota(ctx context.Context, rotation *db.Rotation, shifts []db.Shift, preallocations []db.Preallocation, requirements []db.ShiftRequirement) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	m.insertedRotas = append(m.insertedRotas, rotation)
	m.insertedShifts = append(m.insertedShifts, shifts)
	m.insertedPins = append(m.insertedPins, preallocations)
	m.insertedSeats = append(m.insertedSeats, requirements)
	return nil
}

// testShape is the default Shape these tests define rotas against — one Team
// lead and four Service volunteers, against the Roles testRoleStore holds. Most
// of them are not about the Shape and simply need one, since a rota whose
// shifts would ask for nobody is refused.
var testShape = []db.DefaultShapeSeat{
	{RoleID: "role-team-lead", Seats: 1},
	{RoleID: "role-service-volunteer", Seats: 4},
}

// definable is a deployment a rota can be defined against: an admin has said
// when the drop-in runs and what a shift asks for. Everything else about it is
// empty, and tests that care about one part set it.
func definable() *mockDB {
	return &mockDB{
		defaults: db.RotaDefaults{ShiftStartTime: "19:30", ShiftEndTime: "21:30", ShiftTimezone: "Europe/London"},
		shape:    testShape,
	}
}

// definableRoster is the roster defining reads to open the new rota's round:
// two active volunteers and one who has stopped. Most of these tests are not
// about the round and simply need somewhere for it to come from.
func definableRoster() *mockVolunteerClient {
	return &mockVolunteerClient{volunteers: []model.Volunteer{
		{ID: "v1", FirstName: "Alice", LastName: "Smith", Status: "Active"},
		{ID: "v2", FirstName: "Bob", LastName: "Jones", Status: "Active"},
		{ID: "v3", FirstName: "Carol", LastName: "White", Status: "Inactive"},
	}}
}

// statedRota is the whole of what defining takes: how many shifts, and from
// when. The hours and the Shape are the Rota Defaults' to say and are not here
// (issue #176).
func statedRota(shiftCount int, startDate string) DefineRotaParams {
	return DefineRotaParams{ShiftCount: shiftCount, StartDate: startDate}
}

func TestDefineRota_MintsWeeklyFromTheStatedStart(t *testing.T) {
	mock := definable()

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(12, "2026-08-02"))
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.NotEmpty(t, result.Rotation.ID)
	assert.Equal(t, 12, result.Rotation.ShiftCount)
	assert.Equal(t, "2026-08-02", result.Rotation.Start)

	// End spans to the last shift, so the returned rotation reads the same as one
	// later fetched back from the store.
	startDate, err := time.Parse("2006-01-02", result.Rotation.Start)
	require.NoError(t, err)
	assert.Equal(t, startDate.AddDate(0, 0, 7*11).Format("2006-01-02"), result.Rotation.End)

	assert.Len(t, mock.insertedRotas, 1)
	assert.Equal(t, result.Rotation, mock.insertedRotas[0])

	// Rotation and its shifts are created together in a single store call, and
	// the result carries exactly the shifts that were inserted.
	require.Len(t, mock.insertedShifts, 1)
	require.Len(t, result.Shifts, 12, "one shift minted per shift date")
	assert.Equal(t, mock.insertedShifts[0], result.Shifts)

	seenIDs := make(map[string]bool)
	for i, s := range result.Shifts {
		assert.NotEmpty(t, s.ID, "shift %d has an identity", i)
		assert.False(t, seenIDs[s.ID], "shift ids are unique")
		seenIDs[s.ID] = true

		assert.Equal(t, result.Rotation.ID, s.RotaID, "shift %d belongs to the minting rotation", i)
		expected := startDate.AddDate(0, 0, 7*i)
		assert.Equal(t, expected.Format("2006-01-02"), s.Date, "shift %d is a week after the one before", i)
	}
}

// The cadence is the start date's own weekday, not Sunday: a rota an admin
// began on a Saturday runs on Saturdays. Nothing offers a cadence beyond
// weekly, which is deliberate (issue #140).
func TestDefineRota_KeepsTheStartDatesWeekday(t *testing.T) {
	mock := definable()

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(3, "2026-08-01"))
	require.NoError(t, err)

	for i, s := range result.Shifts {
		date, err := time.Parse("2006-01-02", s.Date)
		require.NoError(t, err)
		assert.Equal(t, time.Saturday, date.Weekday(), "shift %d falls on the weekday the rota started on", i)
	}
	assert.Equal(t, []string{"2026-08-01", "2026-08-08", "2026-08-15"},
		[]string{result.Shifts[0].Date, result.Shifts[1].Date, result.Shifts[2].Date})
}

// A rota may start whenever an admin says, including after a break — which is
// the reason the date is stated rather than derived. Nothing here consults the
// rota that came before beyond the one-rota-in-flight rule.
func TestDefineRota_StartsAfterABreak(t *testing.T) {
	mock := definable()
	mock.rotations = []db.Rotation{
		{ID: "existing", Start: "2026-06-07", End: "2026-06-28", ShiftCount: 4, AllocatedDatetime: "2026-05-31T09:00:00Z"},
	}

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4, "2026-09-06"))
	require.NoError(t, err)
	assert.Equal(t, "2026-09-06", result.Rotation.Start, "the stated date stands, gap and all")
}

// Defining a rota is where a Shift gets the hours it runs: the default times of
// day, written onto each shift's own date (issue #133). They are local
// wall-clock, so no zone moves them and a shift in July reads the same as one
// in January.
func TestDefineRota_WritesTheDefaultShiftTimes(t *testing.T) {
	mock := definable()
	mock.defaults = db.RotaDefaults{ShiftStartTime: "18:00", ShiftEndTime: "20:15", ShiftTimezone: "Europe/London"}

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(3, "2026-08-02"))
	require.NoError(t, err)

	require.Len(t, result.Shifts, 3)
	for i, s := range result.Shifts {
		assert.Equal(t, s.Date+"T18:00:00", s.StartAt, "shift %d starts at the default time on its own date", i)
		assert.Equal(t, s.Date+"T20:15:00", s.EndAt, "shift %d ends at the default time on its own date", i)
	}

	// What was returned is what was written.
	require.Len(t, mock.insertedShifts, 1)
	assert.Equal(t, result.Shifts, mock.insertedShifts[0])
}

// Defining a rota is also where a Shift gets its Shape: the default Shape,
// copied onto every Shift as Seats of its own (issue #137). From here the
// settings can be edited freely and this rota still asks for what it was minted
// asking for.
func TestDefineRota_WritesTheDefaultShapeOntoEveryShift(t *testing.T) {
	mock := definable()

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(3, "2026-08-02"))
	require.NoError(t, err)
	require.Len(t, result.Shifts, 3)

	require.Len(t, mock.insertedSeats, 1)
	var expected []db.ShiftRequirement
	for _, s := range result.Shifts {
		expected = append(expected,
			db.ShiftRequirement{ShiftID: s.ID, RoleID: "role-team-lead", Seats: 1},
			db.ShiftRequirement{ShiftID: s.ID, RoleID: "role-service-volunteer", Seats: 4},
		)
	}
	assert.Equal(t, expected, mock.insertedSeats[0])
}

// Defining a rota spends the Standing Preallocations: they become ordinary
// Preallocations on the Shifts their rules land on, written in the same
// transaction as the rota itself.
func TestDefineRota_SeedsStandingPreallocations(t *testing.T) {
	// A rota starting on the first Sunday of a month, so a "first Sunday" rule
	// lands on its opening shift and on nothing else in a four-shift run.
	mock := definable()
	mock.standing = []db.StandingPreallocation{
		{ID: "standing-1", RRule: "FREQ=MONTHLY;BYDAY=1SU", RoleID: "role-service-volunteer", CustomValue: "St John's team"},
	}

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4, "2026-08-02"))
	require.NoError(t, err)

	require.Len(t, result.Preallocations, 1)
	assert.Equal(t, result.Shifts[0].ID, result.Preallocations[0].ShiftID)
	assert.Equal(t, "St John's team", result.Preallocations[0].CustomValue)
	assert.Equal(t, "role-service-volunteer", result.Preallocations[0].RoleID)

	// One store call, so a rota can never exist with only some of the pins an
	// admin was promised.
	require.Len(t, mock.insertedPins, 1)
	assert.Equal(t, result.Preallocations, mock.insertedPins[0])
}

// No Standing Preallocations is the ordinary state of a deployment nobody has
// configured, not a reason to refuse a rota.
func TestDefineRota_NoStandingPreallocations(t *testing.T) {
	mock := definable()

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4, "2026-08-02"))
	require.NoError(t, err)
	assert.Empty(t, result.Preallocations)
	require.Len(t, mock.insertedRotas, 1)
}

func TestDefineRota_InvalidShiftCount(t *testing.T) {
	for _, tt := range []struct {
		name       string
		shiftCount int
	}{
		{"zero shifts", 0},
		{"negative shifts", -5},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mock := definable()

			result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(tt.shiftCount, "2026-08-02"))
			assert.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), "shift count must be positive")
			// Classified as bad input, so callers such as the HTTP API answer
			// 400 rather than 500.
			assert.ErrorIs(t, err, ErrInvalidInput)
			assert.Empty(t, mock.insertedRotas)
		})
	}
}

// A start date is the one field with no sensible default at this level: a rota
// on no day is not a rota, so a missing or unreadable one is refused rather
// than quietly becoming today (ADR 0007).
func TestDefineRota_RefusesAnUnreadableStartDate(t *testing.T) {
	for _, stated := range []string{"", "2 August 2026", "02/08/2026", "2026-02-30"} {
		t.Run(stated, func(t *testing.T) {
			mock := definable()

			result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4, stated))
			assert.Nil(t, result)
			require.ErrorIs(t, err, ErrInvalidInput)
			assert.Contains(t, err.Error(), "is not a date")
			assert.Empty(t, mock.insertedRotas)
		})
	}
}

// Hours nobody has stated stop a rota being defined, and the refusal says where
// to go and state them. A Shift's date is the date of its start (ADR 0007), so
// a rota minted without them would be a rota on no days at all — which is why
// defining is the second path that refuses over incomplete settings, alongside
// allocation (a narrowing of ADR 0006).
func TestDefineRota_RefusesShiftTimesNobodyHasSet(t *testing.T) {
	for _, tt := range []struct {
		name       string
		start, end string
		says       string
	}{
		{"neither", "", "", "the default shift start time and the default shift end time have not been set"},
		{"no start", "", "21:30", "the default shift start time has not been set"},
		{"no end", "19:30", "", "the default shift end time has not been set"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mock := definable()
			mock.defaults = db.RotaDefaults{ShiftStartTime: tt.start, ShiftEndTime: tt.end, ShiftTimezone: "Europe/London"}

			result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4, "2026-08-02"))
			assert.Nil(t, result)
			require.ErrorIs(t, err, ErrInvalidInput)
			assert.Contains(t, err.Error(), tt.says)
			assert.Contains(t, err.Error(), "rota defaults", "and where to go and say so")
			assert.Empty(t, mock.insertedRotas, "nothing is minted on hours nobody has stated")
		})
	}
}

// A rota whose shifts ask for nobody solves perfectly and staffs nobody, so a
// Shape nobody has stated is refused at the one moment it could still be said.
// One Shift can be emptied afterwards, deliberately (issue #138); a whole rota
// of them is a mistake.
func TestDefineRota_RefusesADefaultShapeAskingForNobody(t *testing.T) {
	mock := definable()
	mock.shape = nil

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4, "2026-08-02"))
	assert.Nil(t, result)
	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "the default shape has not been set")
	assert.Empty(t, mock.insertedRotas, "nothing is minted when there is no shape to mint against")
}

// One rota is in flight at a time (issue #139). An unallocated rota is refused
// as a conflict rather than as bad input: nothing about the request is wrong,
// and the fix is an act on the other rota — which is why the message names it.
func TestDefineRota_RefusedWhileARotaIsInFlight(t *testing.T) {
	mock := definable()
	mock.rotations = []db.Rotation{
		{ID: "allocated", Start: "2026-06-07", End: "2026-06-28", ShiftCount: 4, AllocatedDatetime: "2026-05-31T09:00:00Z"},
		{ID: "in-flight", Start: "2026-07-05", End: "2026-07-26", ShiftCount: 4},
	}

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4, "2026-08-02"))
	assert.Nil(t, result)
	require.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "2026-07-05 to 2026-07-26", "the message points at the rota in the way")
	assert.Empty(t, mock.insertedRotas, "nothing is minted behind a rota that is still in flight")
}

// The rule is about unallocated rotas only: a history of allocated ones is the
// ordinary state of a deployment that has been running for years.
func TestDefineRota_AllowedWhenEveryRotaIsAllocated(t *testing.T) {
	mock := definable()
	mock.rotations = []db.Rotation{
		{ID: "older", Start: "2026-06-07", End: "2026-06-28", ShiftCount: 4, AllocatedDatetime: "2026-05-31T09:00:00Z"},
		{ID: "latest", Start: "2026-07-05", End: "2026-07-26", ShiftCount: 4, AllocatedDatetime: "2026-06-28T09:00:00Z"},
	}

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4, "2026-08-02"))
	require.NoError(t, err)
	assert.Equal(t, "2026-08-02", result.Rotation.Start)
	assert.Len(t, mock.insertedRotas, 1)
}

// Where more than one rota is somehow unallocated — a deployment predating the
// rule, or rows edited by hand — the admin is pointed at the earliest, which is
// the one that has to be dealt with first.
func TestDefineRota_NamesTheEarliestRotaInFlight(t *testing.T) {
	mock := definable()
	mock.rotations = []db.Rotation{
		{ID: "later", Start: "2026-09-06", End: "2026-09-27", ShiftCount: 4},
		{ID: "earlier", Start: "2026-07-05", End: "2026-07-26", ShiftCount: 4},
	}

	_, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4, "2026-10-04"))
	require.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "2026-07-05")
	assert.NotContains(t, err.Error(), "2026-09-06")
}

// A start date an admin chose can land on a day the drop-in already runs, which
// the one-Shift-per-date index refuses. It is an ordinary mistake, so it is
// answered as one: the day, the rota covering it, and where to start instead.
func TestDefineRota_RefusesADateAlreadyTaken(t *testing.T) {
	mock := definable()
	mock.rotations = []db.Rotation{
		{ID: "allocated", Start: "2026-07-05", End: "2026-07-26", ShiftCount: 4, AllocatedDatetime: "2026-06-28T09:00:00Z"},
	}
	mock.insertErr = db.ErrShiftDateTaken

	// Starts a fortnight before the last rota ends, so its first two shifts land
	// on days that already have one.
	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4, "2026-07-19"))
	assert.Nil(t, result)
	require.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "19 July 2026", "the first day that clashes is named")
	assert.Contains(t, err.Error(), "26 July 2026", "so is the rota in the way, and where to start after")
}

// Where nothing overlaps and the index still refuses — a concurrent define that
// committed first — the refusal says only what is certain rather than naming a
// day it would be guessing at.
func TestDefineRota_ADateTakenByNothingItCanSee(t *testing.T) {
	mock := definable()
	mock.insertErr = db.ErrShiftDateTaken

	_, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4, "2026-08-02"))
	require.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "already has a shift on it")
}

// Anything else the insert reports is a fault rather than a mistake, so it
// reaches the caller as one and answers 500.
func TestDefineRota_InsertFailure(t *testing.T) {
	mock := definable()
	mock.insertErr = errors.New("boom")

	_, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4, "2026-08-02"))
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrConflict)
	assert.NotErrorIs(t, err, ErrInvalidInput)
}

// Rotas that cannot be read are a fault too: the one-rota-in-flight rule cannot
// be checked without them, so nothing is minted on the back of the failure.
func TestDefineRota_RotationsReadFails(t *testing.T) {
	mock := definable()
	mock.getRotationsErr = errors.New("boom")

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(2, "2026-08-02"))
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Empty(t, mock.insertedShifts, "nothing is written when the rotas cannot be read")
}

// Defining a rota asks everybody about it: every active volunteer holds a link
// for it before the request is over, so the draft has a round to read and an
// admin's next act is sending the links rather than making them (issue #188).
func TestDefineRota_OpensTheRound(t *testing.T) {
	mock := definable()

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4, "2026-08-02"))
	require.NoError(t, err)

	assert.Equal(t, 2, result.Asked, "the two active volunteers, not the one who has stopped")
	require.Len(t, mock.mintedRequests, 2)

	seenTokens := make(map[string]bool)
	for _, request := range mock.mintedRequests {
		assert.Equal(t, result.Rotation.ID, request.RotaID, "the round is for the rota just defined")
		assert.NotEmpty(t, request.Token)
		assert.False(t, seenTokens[request.Token], "every volunteer gets their own link")
		seenTokens[request.Token] = true
	}
}

// The roster is a Google Sheet, and defining a rota is a database act. A read
// of the sheet that fails costs the round, never the rota: what is on the
// database stays, and the Allocation tab offers to start the round.
func TestDefineRota_RosterFailureDoesNotCostTheRota(t *testing.T) {
	mock := definable()
	roster := &mockVolunteerClient{err: errors.New("sheets is down")}

	result, err := DefineRota(context.Background(), mock, roster, nil, zap.NewNop(), statedRota(4, "2026-08-02"))
	require.NoError(t, err)

	assert.Equal(t, 0, result.Asked)
	assert.Empty(t, mock.mintedRequests)
	assert.Len(t, mock.insertedRotas, 1, "the rota is defined either way")
}

// Same again for the write the links go in: it is the last thing defining does,
// and failing it leaves a rota that is complete in every other respect.
func TestDefineRota_MintFailureDoesNotCostTheRota(t *testing.T) {
	mock := definable()
	mock.mintErr = errors.New("boom")

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4, "2026-08-02"))
	require.NoError(t, err)

	assert.Equal(t, 0, result.Asked)
	assert.Len(t, mock.insertedRotas, 1)
	assert.Len(t, result.Shifts, 4)
}

func TestFindLatestRotation(t *testing.T) {
	rotations := []db.Rotation{
		{ID: "r1", Start: "2025-01-06", ShiftCount: 10},
		{ID: "r2", Start: "2025-03-17", ShiftCount: 8}, // Latest
		{ID: "r3", Start: "2024-12-16", ShiftCount: 12},
	}

	latest := utils.FindLatestRotation(rotations)
	require.NotNil(t, latest)
	assert.Equal(t, "r2", latest.ID)
	assert.Equal(t, "2025-03-17", latest.Start)
}

func TestFindLatestRotation_Empty(t *testing.T) {
	latest := utils.FindLatestRotation([]db.Rotation{})
	assert.Nil(t, latest)
}
