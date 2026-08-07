package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services/utils"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// mockDB is a test double for db.DB on the defining path.
type mockDB struct {
	testRoleStore

	rotations       []db.Rotation
	standing        []db.StandingPreallocation
	insertedRotas   []*db.Rotation
	insertedShifts  [][]db.Shift
	insertedPins    [][]db.Preallocation
	insertedSeats   [][]db.ShiftRequirement
	getRotationsErr error
	insertErr       error
}

func (m *mockDB) GetRotations(ctx context.Context) ([]db.Rotation, error) {
	if m.getRotationsErr != nil {
		return nil, m.getRotationsErr
	}
	return m.rotations, nil
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

// testShape is the Shape these tests define rotas against — one Team lead and
// four Service volunteers, against the Roles testRoleStore holds. Most of them
// are not about the Shape and simply need one, since a rota whose shifts ask
// for nobody is refused.
var testShape = []SeatParams{
	{RoleID: "role-team-lead", Count: 1},
	{RoleID: "role-service-volunteer", Count: 4},
}

// statedRota is a whole stated rota, which is the only kind there is: every
// field is the request's to say, and none of them falls back to the settings
// (issue #140). Tests that care about one field set it and leave the rest.
func statedRota(shiftCount int, startDate string) DefineRotaParams {
	return DefineRotaParams{
		ShiftCount:     shiftCount,
		StartDate:      startDate,
		ShiftStartTime: "19:30",
		ShiftEndTime:   "21:30",
		Shape:          testShape,
	}
}

func TestDefineRota_MintsWeeklyFromTheStatedStart(t *testing.T) {
	mock := &mockDB{}

	result, err := DefineRota(context.Background(), mock, zap.NewNop(), statedRota(12, "2026-08-02"))
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
	mock := &mockDB{}

	result, err := DefineRota(context.Background(), mock, zap.NewNop(), statedRota(3, "2026-08-01"))
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
	mock := &mockDB{rotations: []db.Rotation{
		{ID: "existing", Start: "2026-06-07", End: "2026-06-28", ShiftCount: 4, AllocatedDatetime: "2026-05-31T09:00:00Z"},
	}}

	result, err := DefineRota(context.Background(), mock, zap.NewNop(), statedRota(4, "2026-09-06"))
	require.NoError(t, err)
	assert.Equal(t, "2026-09-06", result.Rotation.Start, "the stated date stands, gap and all")
}

// Defining a rota is where a Shift gets the hours it runs: the stated times of
// day, written onto each shift's own date (issue #133). They are local
// wall-clock, so no zone moves them and a shift in July reads the same as one
// in January.
func TestDefineRota_WritesTheStatedShiftTimes(t *testing.T) {
	mock := &mockDB{}

	stated := statedRota(3, "2026-08-02")
	stated.ShiftStartTime = "18:00"
	stated.ShiftEndTime = "20:15"

	result, err := DefineRota(context.Background(), mock, zap.NewNop(), stated)
	require.NoError(t, err)

	require.Len(t, result.Shifts, 3)
	for i, s := range result.Shifts {
		assert.Equal(t, s.Date+"T18:00:00", s.StartAt, "shift %d starts at the stated time on its own date", i)
		assert.Equal(t, s.Date+"T20:15:00", s.EndAt, "shift %d ends at the stated time on its own date", i)
	}

	// What was returned is what was written.
	require.Len(t, mock.insertedShifts, 1)
	assert.Equal(t, result.Shifts, mock.insertedShifts[0])
}

// Defining a rota is also where a Shift gets its Shape: the stated Shape,
// copied onto every Shift as Seats of its own (issue #137). From here the
// settings can be edited freely and this rota still asks for what it was minted
// asking for.
func TestDefineRota_WritesTheStatedShapeOntoEveryShift(t *testing.T) {
	mock := &mockDB{}

	result, err := DefineRota(context.Background(), mock, zap.NewNop(), statedRota(3, "2026-08-02"))
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
	mock := &mockDB{standing: []db.StandingPreallocation{
		{ID: "standing-1", RRule: "FREQ=MONTHLY;BYDAY=1SU", RoleID: "role-service-volunteer", CustomValue: "St John's team"},
	}}

	result, err := DefineRota(context.Background(), mock, zap.NewNop(), statedRota(4, "2026-08-02"))
	require.NoError(t, err)

	require.Len(t, result.Preallocations, 1)
	assert.Equal(t, result.Shifts[0].ID, result.Preallocations[0].ShiftID)
	assert.Equal(t, "St John's team", result.Preallocations[0].CustomValue)
	assert.Equal(t, "Service volunteer", result.Preallocations[0].Role)

	// One store call, so a rota can never exist with only some of the pins an
	// admin was promised.
	require.Len(t, mock.insertedPins, 1)
	assert.Equal(t, result.Preallocations, mock.insertedPins[0])
}

// No Standing Preallocations is the ordinary state of a deployment nobody has
// configured, not a reason to refuse a rota.
func TestDefineRota_NoStandingPreallocations(t *testing.T) {
	mock := &mockDB{}

	result, err := DefineRota(context.Background(), mock, zap.NewNop(), statedRota(4, "2026-08-02"))
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
			mock := &mockDB{}

			result, err := DefineRota(context.Background(), mock, zap.NewNop(), statedRota(tt.shiftCount, "2026-08-02"))
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
			mock := &mockDB{}

			result, err := DefineRota(context.Background(), mock, zap.NewNop(), statedRota(4, stated))
			assert.Nil(t, result)
			require.ErrorIs(t, err, ErrInvalidInput)
			assert.Contains(t, err.Error(), "is not a date")
			assert.Empty(t, mock.insertedRotas)
		})
	}
}

// The times a rota is defined with are checked by the same rules the settings
// screen states them under, because it is the same question asked twice.
func TestDefineRota_RefusesUnusableShiftTimes(t *testing.T) {
	for _, tt := range []struct {
		name       string
		start, end string
		says       string
	}{
		{"no start", "", "21:30", "the start is missing"},
		{"no end", "19:30", "", "the end is missing"},
		{"not a time", "half seven", "21:30", "is not a time of day"},
		{"ends before it starts", "21:30", "19:30", "has to end after it starts"},
		{"ends when it starts", "19:30", "19:30", "has to end after it starts"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockDB{}

			stated := statedRota(4, "2026-08-02")
			stated.ShiftStartTime, stated.ShiftEndTime = tt.start, tt.end

			result, err := DefineRota(context.Background(), mock, zap.NewNop(), stated)
			assert.Nil(t, result)
			require.ErrorIs(t, err, ErrInvalidInput)
			assert.Contains(t, err.Error(), tt.says)
			assert.Empty(t, mock.insertedRotas, "nothing is minted on hours nobody could work")
		})
	}
}

// A rota whose shifts ask for nobody solves perfectly and staffs nobody, so it
// is refused at the one moment it could still be said differently. One Shift
// can be emptied afterwards, deliberately (issue #138); a whole rota of them is
// a mistake.
func TestDefineRota_RefusesAShapeAskingForNobody(t *testing.T) {
	mock := &mockDB{}

	stated := statedRota(4, "2026-08-02")
	stated.Shape = nil

	result, err := DefineRota(context.Background(), mock, zap.NewNop(), stated)
	assert.Nil(t, result)
	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "ask for somebody")
	assert.Empty(t, mock.insertedRotas, "nothing is minted when there is no shape to mint against")
}

// A Shape naming a Role nobody has heard of is the settings screen's refusal,
// reached through the define form: both state a Shape, and both are checked by
// statedSeats.
func TestDefineRota_RefusesAShapeNamingAnUnknownRole(t *testing.T) {
	mock := &mockDB{}

	stated := statedRota(4, "2026-08-02")
	stated.Shape = []SeatParams{{RoleID: "role-nobody-has", Count: 2}}

	_, err := DefineRota(context.Background(), mock, zap.NewNop(), stated)
	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "is not a known role")
	assert.Empty(t, mock.insertedRotas)
}

// One rota is in flight at a time (issue #139). An unallocated rota is refused
// as a conflict rather than as bad input: nothing about the request is wrong,
// and the fix is an act on the other rota — which is why the message names it.
func TestDefineRota_RefusedWhileARotaIsInFlight(t *testing.T) {
	mock := &mockDB{rotations: []db.Rotation{
		{ID: "allocated", Start: "2026-06-07", End: "2026-06-28", ShiftCount: 4, AllocatedDatetime: "2026-05-31T09:00:00Z"},
		{ID: "in-flight", Start: "2026-07-05", End: "2026-07-26", ShiftCount: 4},
	}}

	result, err := DefineRota(context.Background(), mock, zap.NewNop(), statedRota(4, "2026-08-02"))
	assert.Nil(t, result)
	require.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "2026-07-05 to 2026-07-26", "the message points at the rota in the way")
	assert.Empty(t, mock.insertedRotas, "nothing is minted behind a rota that is still in flight")
}

// The rule is about unallocated rotas only: a history of allocated ones is the
// ordinary state of a deployment that has been running for years.
func TestDefineRota_AllowedWhenEveryRotaIsAllocated(t *testing.T) {
	mock := &mockDB{rotations: []db.Rotation{
		{ID: "older", Start: "2026-06-07", End: "2026-06-28", ShiftCount: 4, AllocatedDatetime: "2026-05-31T09:00:00Z"},
		{ID: "latest", Start: "2026-07-05", End: "2026-07-26", ShiftCount: 4, AllocatedDatetime: "2026-06-28T09:00:00Z"},
	}}

	result, err := DefineRota(context.Background(), mock, zap.NewNop(), statedRota(4, "2026-08-02"))
	require.NoError(t, err)
	assert.Equal(t, "2026-08-02", result.Rotation.Start)
	assert.Len(t, mock.insertedRotas, 1)
}

// Where more than one rota is somehow unallocated — a deployment predating the
// rule, or rows edited by hand — the admin is pointed at the earliest, which is
// the one that has to be dealt with first.
func TestDefineRota_NamesTheEarliestRotaInFlight(t *testing.T) {
	mock := &mockDB{rotations: []db.Rotation{
		{ID: "later", Start: "2026-09-06", End: "2026-09-27", ShiftCount: 4},
		{ID: "earlier", Start: "2026-07-05", End: "2026-07-26", ShiftCount: 4},
	}}

	_, err := DefineRota(context.Background(), mock, zap.NewNop(), statedRota(4, "2026-10-04"))
	require.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "2026-07-05")
	assert.NotContains(t, err.Error(), "2026-09-06")
}

// A start date an admin chose can land on a day the drop-in already runs, which
// the one-Shift-per-date index refuses. It is an ordinary mistake, so it is
// answered as one: the day, the rota covering it, and where to start instead.
func TestDefineRota_RefusesADateAlreadyTaken(t *testing.T) {
	mock := &mockDB{
		rotations: []db.Rotation{
			{ID: "allocated", Start: "2026-07-05", End: "2026-07-26", ShiftCount: 4, AllocatedDatetime: "2026-06-28T09:00:00Z"},
		},
		insertErr: db.ErrShiftDateTaken,
	}

	// Starts a fortnight before the last rota ends, so its first two shifts land
	// on days that already have one.
	result, err := DefineRota(context.Background(), mock, zap.NewNop(), statedRota(4, "2026-07-19"))
	assert.Nil(t, result)
	require.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "19 July 2026", "the first day that clashes is named")
	assert.Contains(t, err.Error(), "26 July 2026", "so is the rota in the way, and where to start after")
}

// Where nothing overlaps and the index still refuses — a concurrent define that
// committed first — the refusal says only what is certain rather than naming a
// day it would be guessing at.
func TestDefineRota_ADateTakenByNothingItCanSee(t *testing.T) {
	mock := &mockDB{insertErr: db.ErrShiftDateTaken}

	_, err := DefineRota(context.Background(), mock, zap.NewNop(), statedRota(4, "2026-08-02"))
	require.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "already has a shift on it")
}

// Anything else the insert reports is a fault rather than a mistake, so it
// reaches the caller as one and answers 500.
func TestDefineRota_InsertFailure(t *testing.T) {
	mock := &mockDB{insertErr: errors.New("boom")}

	_, err := DefineRota(context.Background(), mock, zap.NewNop(), statedRota(4, "2026-08-02"))
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrConflict)
	assert.NotErrorIs(t, err, ErrInvalidInput)
}

// Rotas that cannot be read are a fault too: the one-rota-in-flight rule cannot
// be checked without them, so nothing is minted on the back of the failure.
func TestDefineRota_RotationsReadFails(t *testing.T) {
	mock := &mockDB{getRotationsErr: errors.New("boom")}

	result, err := DefineRota(context.Background(), mock, zap.NewNop(), statedRota(2, "2026-08-02"))
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Empty(t, mock.insertedShifts, "nothing is written when the rotas cannot be read")
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
