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
	shiftsByRota    map[string][]db.Shift
	existingShifts  []db.Shift
	defaults        db.RotaDefaults
	shape           []db.DefaultShapeSeat
	overrides       []db.Override
	insertedRotas   []*db.Rotation
	insertedShifts  [][]db.Shift
	insertedPins    [][]db.Pin
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

func (m *mockDB) GetShiftsByRotaID(ctx context.Context, rotaID string) ([]db.Shift, error) {
	return m.shiftsByRota[rotaID], nil
}

// GetShiftsOverlapping answers as the store does: every Shift already defined
// that runs at some moment between the two.
func (m *mockDB) GetShiftsOverlapping(ctx context.Context, startAt, endAt string) ([]db.Shift, error) {
	var overlapping []db.Shift
	for _, s := range m.existingShifts {
		if s.StartAt < endAt && startAt < s.EndAt {
			overlapping = append(overlapping, s)
		}
	}
	return overlapping, nil
}

func (m *mockDB) GetRotaDefaults(ctx context.Context) (db.RotaDefaults, error) {
	return m.defaults, nil
}

func (m *mockDB) GetDefaultShape(ctx context.Context) ([]db.DefaultShapeSeat, error) {
	return m.shape, nil
}

func (m *mockDB) GetOverrides(ctx context.Context) ([]db.Override, error) {
	return m.overrides, nil
}

func (m *mockDB) InsertDefinedRota(ctx context.Context, rotation *db.Rotation, shifts []db.Shift, pins []db.Pin, requirements []db.ShiftRequirement) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	m.insertedRotas = append(m.insertedRotas, rotation)
	m.insertedShifts = append(m.insertedShifts, shifts)
	m.insertedPins = append(m.insertedPins, pins)
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

// definable is a deployment a rota can be defined against: an Organiser has said
// when the Organisation runs — every Sunday at 19:30 for two hours, from Sunday
// 2 August 2026 — and what a shift asks for. Everything else about it is empty,
// and tests that care about one part set it.
func definable() *mockDB {
	return &mockDB{
		defaults: db.RotaDefaults{
			ShiftPattern:       weeklySunday,
			PatternStartFrom:   "2026-08-02",
			ShiftLengthMinutes: 120,
			ShiftTimezone:      "Europe/London",
		},
		shape: testShape,
	}
}

// withPattern points a deployment at another Shift pattern, counted from the
// same Start from.
func withPattern(m *mockDB, pattern string) *mockDB {
	m.defaults.ShiftPattern = pattern
	return m
}

// pastRota is a rota that has been and gone, stopping after the given
// Shifts. It is what the next rota carries on from.
func pastRota(m *mockDB, id string, shifts ...db.Shift) {
	last := shifts[len(shifts)-1]
	m.rotations = append(m.rotations, db.Rotation{
		ID: id, Start: shifts[0].Date, End: last.Date, ShiftCount: len(shifts),
		StoppedAfter: last.StartAt, AllocatedDatetime: "2026-01-01T09:00:00Z",
	})
	if m.shiftsByRota == nil {
		m.shiftsByRota = map[string][]db.Shift{}
	}
	m.shiftsByRota[id] = shifts
	m.existingShifts = append(m.existingShifts, shifts...)
}

// existing is a Shift already defined, starting at start and running for two hours.
func existing(id, start string) db.Shift {
	at, err := time.Parse(model.ShiftTimestampLayout, start)
	if err != nil {
		panic(err)
	}
	return db.Shift{
		ID: id, Date: at.Format("2006-01-02"), StartAt: start,
		EndAt: at.Add(2 * time.Hour).Format(model.ShiftTimestampLayout),
	}
}

func starts(shifts []db.Shift) []string {
	var out []string
	for _, s := range shifts {
		out = append(out, s.StartAt)
	}
	return out
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

// statedRota is the whole of what defining takes: how many shifts, and no gap.
// Which Shifts, and their hours, are the Shift pattern's to say (ADR 0013).
func statedRota(shiftCount int) DefineRotaParams {
	return DefineRotaParams{ShiftCount: shiftCount}
}

// Weekly with a count of 12 gives 12 Shifts, a week apart, from Start from when
// there is no rota before it to carry on from.
func TestDefineRota_MintsTheNextShiftsThePatternGenerates(t *testing.T) {
	mock := definable()

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(12))
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.NotEmpty(t, result.Rotation.ID)
	assert.Equal(t, 12, result.Rotation.ShiftCount)
	assert.Equal(t, "2026-08-02", result.Rotation.Start)
	// End spans to the last shift, so the returned rotation reads the same as one
	// later fetched back from the store.
	assert.Equal(t, "2026-10-18", result.Rotation.End)
	assert.Equal(t, "2026-10-18T19:30:00", result.Rotation.StoppedAfter, "where it stopped is its last Shift's start")

	assert.Len(t, mock.insertedRotas, 1)
	assert.Equal(t, result.Rotation, mock.insertedRotas[0])

	// Rotation and its shifts are created together in a single store call, and
	// the result carries exactly the shifts that were inserted.
	require.Len(t, mock.insertedShifts, 1)
	require.Len(t, result.Shifts, 12)
	assert.Equal(t, mock.insertedShifts[0], result.Shifts)

	first, err := time.Parse("2006-01-02", "2026-08-02")
	require.NoError(t, err)
	seenIDs := make(map[string]bool)
	for i, s := range result.Shifts {
		assert.NotEmpty(t, s.ID, "shift %d has an identity", i)
		assert.False(t, seenIDs[s.ID], "shift ids are unique")
		seenIDs[s.ID] = true
		assert.Equal(t, result.Rotation.ID, s.RotaID, "shift %d belongs to the minting rotation", i)

		date := first.AddDate(0, 0, 7*i).Format("2006-01-02")
		assert.Equal(t, date, s.Date, "shift %d is a week after the one before", i)
		assert.Equal(t, date+"T19:30:00", s.StartAt, "shift %d starts when the pattern says", i)
		assert.Equal(t, date+"T21:30:00", s.EndAt, "and runs for the shift length")
	}
}

// A rota carries on from the exact Shift the last one stopped after, not from
// the day after it: on a day with two Shifts, a rota that stopped after the
// morning one is followed by the evening one that same day.
func TestDefineRota_CarriesOnFromTheShiftTheLastRotaStoppedAfter(t *testing.T) {
	mock := withPattern(definable(),
		"RRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=10;BYMINUTE=0\nRRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30")
	pastRota(mock, "previous",
		existing("a", "2026-08-02T10:00:00"),
		existing("b", "2026-08-02T19:30:00"),
		existing("c", "2026-08-09T10:00:00"),
	)

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(3))
	require.NoError(t, err)
	assert.Equal(t, []string{"2026-08-09T19:30:00", "2026-08-16T10:00:00", "2026-08-16T19:30:00"}, starts(result.Shifts))
}

// Not before leaves a gap: the rota carries on with the first Shift the
// pattern generates on or after it, and none before.
func TestDefineRota_LeavesAGapWithNotBefore(t *testing.T) {
	mock := definable()
	pastRota(mock, "previous", existing("a", "2026-08-02T19:30:00"))

	params := statedRota(2)
	params.NotBefore = "2026-09-01"
	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), params)
	require.NoError(t, err)
	assert.Equal(t, []string{"2026-09-06T19:30:00", "2026-09-13T19:30:00"}, starts(result.Shifts))
}

// "Every other week" counts from Start from rather than from where the last
// rota stopped, so a rota that stopped on the wrong week does not move the
// fortnight.
func TestDefineRota_EveryOtherWeekCountsFromStartFrom(t *testing.T) {
	mock := withPattern(definable(), "RRULE:FREQ=WEEKLY;INTERVAL=2;BYDAY=SU;BYHOUR=19;BYMINUTE=30")
	// Stopped after the 9th, which is not one of the pattern's weeks.
	pastRota(mock, "previous", existing("a", "2026-08-09T19:30:00"))

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(2))
	require.NoError(t, err)
	assert.Equal(t, []string{"2026-08-16T19:30:00", "2026-08-30T19:30:00"}, starts(result.Shifts))
}

// The previous rota is the one that stopped latest, which is the one the next
// carries on from, whatever order they were defined in.
func TestDefineRota_CarriesOnFromTheRotaThatStoppedLatest(t *testing.T) {
	mock := definable()
	pastRota(mock, "later", existing("b", "2026-09-06T19:30:00"))
	pastRota(mock, "earlier", existing("a", "2026-08-02T19:30:00"))

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(1))
	require.NoError(t, err)
	assert.Equal(t, []string{"2026-09-13T19:30:00"}, starts(result.Shifts))
}

// Defining a rota is also where a Shift gets its Shape: the default Shape,
// copied onto every Shift as Seats of its own (issue #137). From here the
// settings can be edited freely and this rota still asks for what it was minted
// asking for.
func TestDefineRota_WritesTheDefaultShapeOntoEveryShift(t *testing.T) {
	mock := definable()

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(3))
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

// Defining a rota spends the Overrides: their Pins become ordinary Pins on the
// Shifts their rules match, written in the same transaction as the rota itself.
func TestDefineRota_PinsTheShiftsAnOverrideMatches(t *testing.T) {
	// A rota starting on the first Sunday of a month, so a "first Sunday" rule
	// lands on its opening shift and on nothing else in a four-shift run.
	mock := definable()
	mock.overrides = []db.Override{firstSundayOverride(
		db.OverridePin{ID: "op-1", RoleID: "role-service-volunteer", CustomValue: "St John's team"},
		db.OverridePin{ID: "op-2", RoleID: "role-team-lead", VolunteerID: "v1"},
	)}

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4))
	require.NoError(t, err)

	require.Len(t, result.Pins, 2)
	for _, pin := range result.Pins {
		assert.Equal(t, result.Shifts[0].ID, pin.ShiftID)
		assert.NotEqual(t, "op-1", pin.ID, "a minted Pin is a Pin of its own, not the Override's")
	}
	assert.Equal(t, "St John's team", result.Pins[0].CustomValue)
	assert.Equal(t, "role-service-volunteer", result.Pins[0].RoleID)
	assert.Equal(t, "v1", result.Pins[1].VolunteerID)
	assert.Equal(t, "role-team-lead", result.Pins[1].RoleID)

	// One store call, so a rota can never exist with only some of the pins an
	// Organiser was promised.
	require.Len(t, mock.insertedPins, 1)
	assert.Equal(t, result.Pins, mock.insertedPins[0])
}

// An Override's Shape replaces the default Shape whole on the Shifts it
// matches, and only on those.
func TestDefineRota_AnOverridesShapeReplacesTheDefault(t *testing.T) {
	mock := definable()
	quiet := firstSundayOverride()
	quiet.SetsShape = true
	quiet.Seats = []db.OverrideSeat{{RoleID: "role-service-volunteer", Seats: 2}}
	mock.overrides = []db.Override{quiet}

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(2))
	require.NoError(t, err)

	first, second := result.Shifts[0].ID, result.Shifts[1].ID
	assert.Equal(t, []db.ShiftRequirement{
		{ShiftID: first, RoleID: "role-service-volunteer", Seats: 2},
		{ShiftID: second, RoleID: "role-team-lead", Seats: 1},
		{ShiftID: second, RoleID: "role-service-volunteer", Seats: 4},
	}, mock.insertedSeats[0])
}

// No Overrides is the ordinary state of a deployment nobody has configured,
// not a reason to refuse a rota.
func TestDefineRota_NoOverrides(t *testing.T) {
	mock := definable()

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4))
	require.NoError(t, err)
	assert.Empty(t, result.Pins)
	require.Len(t, mock.insertedRotas, 1)
}

func TestDefineRota_InvalidShiftCount(t *testing.T) {
	for name, count := range map[string]int{"zero shifts": 0, "negative shifts": -5, "too many": maxShiftCount + 1} {
		t.Run(name, func(t *testing.T) {
			mock := definable()

			result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(count))
			assert.Nil(t, result)
			// Classified as bad input, so callers such as the HTTP API answer
			// 400 rather than 500.
			require.ErrorIs(t, err, ErrInvalidInput)
			assert.Contains(t, err.Error(), "a rota has from 1 to")
			assert.Empty(t, mock.insertedRotas)
		})
	}
}

func TestDefineRota_RefusesAnUnreadableNotBefore(t *testing.T) {
	for _, stated := range []string{"2 August 2026", "02/08/2026", "2026-02-30"} {
		t.Run(stated, func(t *testing.T) {
			mock := definable()

			result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), DefineRotaParams{ShiftCount: 4, NotBefore: stated})
			assert.Nil(t, result)
			require.ErrorIs(t, err, ErrInvalidInput)
			assert.Contains(t, err.Error(), "is not a date")
			assert.Empty(t, mock.insertedRotas)
		})
	}
}

// With no Shift pattern there is nothing to say when Shifts run, so nothing is
// minted, and the refusal says where to go and write one.
func TestDefineRota_RefusesWithoutAShiftPattern(t *testing.T) {
	mock := definable()
	mock.defaults = db.RotaDefaults{ShiftTimezone: "Europe/London"}

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4))
	assert.Nil(t, result)
	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "no shift pattern has been set")
	assert.Contains(t, err.Error(), "rota defaults", "and where to go and say so")
	assert.Empty(t, mock.insertedRotas)
}

// A pattern that has run out — a COUNT or an UNTIL spent — cannot fill the
// rota, and the refusal says how many it could.
func TestDefineRota_RefusesWhenThePatternRunsOut(t *testing.T) {
	mock := withPattern(definable(), "RRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30;COUNT=3")

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4))
	assert.Nil(t, result)
	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "generates only 3 more shifts, not the 4 asked for")
	assert.Empty(t, mock.insertedRotas)
}

// A Shift the pattern generates can land on hours a Shift already runs — one
// added by hand, or a pattern changed under a rota. Two Shifts cannot run at
// once, so the rota is refused as a conflict naming both.
func TestDefineRota_RefusesAShiftThatOverlapsOneAlreadyDefined(t *testing.T) {
	mock := definable()
	mock.existingShifts = []db.Shift{existing("by-hand", "2026-08-09T20:00:00")}

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4))
	assert.Nil(t, result)
	require.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "already defined")
	assert.Contains(t, err.Error(), "9 August 2026", "the day of the clash is named")
	assert.Empty(t, mock.insertedRotas)
}

// A length longer than the gap between two generated starts makes a rota
// overlap itself, which is refused for the same reason.
func TestDefineRota_RefusesShiftsThatOverlapEachOther(t *testing.T) {
	mock := withPattern(definable(), "RRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=10,11;BYMINUTE=0")

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4))
	assert.Nil(t, result)
	require.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "would still be running")
	assert.Empty(t, mock.insertedRotas)
}

// A rota whose shifts ask for nobody solves perfectly and staffs nobody, so a
// Shape nobody has stated is refused at the one moment it could still be said.
// One Shift can be emptied afterwards, deliberately (issue #138); a whole rota
// of them is a mistake.
func TestDefineRota_RefusesADefaultShapeAskingForNobody(t *testing.T) {
	mock := definable()
	mock.shape = nil

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4))
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
		{ID: "allocated", Start: "2026-06-07", End: "2026-06-28", ShiftCount: 4, StoppedAfter: "2026-06-28T19:30:00", AllocatedDatetime: "2026-05-31T09:00:00Z"},
		{ID: "in-flight", Start: "2026-07-05", End: "2026-07-26", ShiftCount: 4, StoppedAfter: "2026-07-26T19:30:00"},
	}

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4))
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
		{ID: "older", Start: "2026-06-07", End: "2026-06-28", ShiftCount: 4, StoppedAfter: "2026-06-28T19:30:00", AllocatedDatetime: "2026-05-31T09:00:00Z"},
		{ID: "latest", Start: "2026-07-05", End: "2026-07-26", ShiftCount: 4, StoppedAfter: "2026-07-26T19:30:00", AllocatedDatetime: "2026-06-28T09:00:00Z"},
	}

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4))
	require.NoError(t, err)
	assert.Equal(t, "2026-08-02", result.Rotation.Start, "carrying on from neither, since both stopped before Start from")
	assert.Len(t, mock.insertedRotas, 1)
}

// Where more than one rota is somehow unallocated — a deployment predating the
// rule, or rows edited by hand — the Organiser is pointed at the earliest, which is
// the one that has to be dealt with first.
func TestDefineRota_NamesTheEarliestRotaInFlight(t *testing.T) {
	mock := definable()
	mock.rotations = []db.Rotation{
		{ID: "later", Start: "2026-09-06", End: "2026-09-27", ShiftCount: 4, StoppedAfter: "2026-09-27T19:30:00"},
		{ID: "earlier", Start: "2026-07-05", End: "2026-07-26", ShiftCount: 4, StoppedAfter: "2026-07-26T19:30:00"},
	}

	_, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4))
	require.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "2026-07-05")
	assert.NotContains(t, err.Error(), "2026-09-06")
}

// Where PlanRota saw no clash and the no-overlap constraint still refuses — a
// concurrent define that committed first — the refusal says only that.
func TestDefineRota_AShiftDefinedMeanwhile(t *testing.T) {
	mock := definable()
	mock.insertErr = db.ErrShiftOverlap

	_, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4))
	require.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "reload and try again")
}

// Anything else the insert reports is a fault rather than a mistake, so it
// reaches the caller as one and answers 500.
func TestDefineRota_InsertFailure(t *testing.T) {
	mock := definable()
	mock.insertErr = errors.New("boom")

	_, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4))
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrConflict)
	assert.NotErrorIs(t, err, ErrInvalidInput)
}

// Rotas that cannot be read are a fault too: the one-rota-in-flight rule cannot
// be checked without them, so nothing is minted on the back of the failure.
func TestDefineRota_RotationsReadFails(t *testing.T) {
	mock := definable()
	mock.getRotationsErr = errors.New("boom")

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(2))
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Empty(t, mock.insertedShifts, "nothing is written when the rotas cannot be read")
}

// Defining a rota asks everybody about it: every active volunteer holds a link
// for it before the request is over, so the draft has a round to read and an
// Organiser's next act is sending the links rather than making them (issue #188).
func TestDefineRota_OpensTheRound(t *testing.T) {
	mock := definable()

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4))
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

	result, err := DefineRota(context.Background(), mock, roster, nil, zap.NewNop(), statedRota(4))
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

	result, err := DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4))
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
