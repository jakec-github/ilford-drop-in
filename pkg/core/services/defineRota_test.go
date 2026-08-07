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

// mockDB implements a test double for db.DB
type mockDB struct {
	testRoleStore

	rotations       []db.Rotation
	standing        []db.StandingPreallocation
	defaults        db.RotaDefaults
	insertedRotas   []*db.Rotation
	insertedShifts  [][]db.Shift
	insertedPins    [][]db.Preallocation
	getRotationsErr error
	defaultsErr     error
	insertErr       error
}

// definableMock is the store a rota can actually be defined against: one that
// has been told when the drop-in runs. A Shift's date is the date of its start
// (ADR 0007), so unset shift times are not a rota with unknown hours, they are
// a refusal — which the test below is the one to say. Every other test here is
// about something else and starts from settings an admin has filled in.
func definableMock() *mockDB {
	return &mockDB{defaults: db.RotaDefaults{
		ShiftStartTime: "19:30",
		ShiftEndTime:   "21:30",
		ShiftTimezone:  "Europe/London",
	}}
}

func (m *mockDB) GetRotaDefaults(ctx context.Context) (db.RotaDefaults, error) {
	if m.defaultsErr != nil {
		return db.RotaDefaults{}, m.defaultsErr
	}
	return m.defaults, nil
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

func (m *mockDB) InsertDefinedRota(ctx context.Context, rotation *db.Rotation, shifts []db.Shift, preallocations []db.Preallocation) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	m.insertedRotas = append(m.insertedRotas, rotation)
	m.insertedShifts = append(m.insertedShifts, shifts)
	m.insertedPins = append(m.insertedPins, preallocations)
	return nil
}

func TestDefineRota_NoExistingRotations(t *testing.T) {
	mock := definableMock()

	logger := zap.NewNop()
	ctx := context.Background()

	result, err := DefineRota(ctx, mock, logger, 12)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Check rotation was created
	assert.NotEmpty(t, result.Rotation.ID)
	assert.Equal(t, 12, result.Rotation.ShiftCount)

	// Check start date is next Sunday
	startDate, err := time.Parse("2006-01-02", result.Rotation.Start)
	require.NoError(t, err)
	assert.Equal(t, time.Sunday, startDate.Weekday())

	// End spans to the last shift, so the returned rotation reads the same as one
	// later fetched back from the store
	assert.Equal(t, startDate.AddDate(0, 0, 7*11).Format("2006-01-02"), result.Rotation.End)

	// Check rotation was inserted
	assert.Len(t, mock.insertedRotas, 1)
	assert.Equal(t, result.Rotation, mock.insertedRotas[0])

	// Rotation and its shifts are created together in a single store call, and
	// the result carries exactly the shifts that were inserted
	require.Len(t, mock.insertedShifts, 1)
	require.Len(t, result.Shifts, 12, "one shift minted per shift date")
	assert.Equal(t, mock.insertedShifts[0], result.Shifts)

	seenIDs := make(map[string]bool)
	for i, s := range result.Shifts {
		assert.NotEmpty(t, s.ID, "shift %d has an identity", i)
		assert.False(t, seenIDs[s.ID], "shift ids are unique")
		seenIDs[s.ID] = true

		assert.Equal(t, result.Rotation.ID, s.RotaID, "shift %d belongs to the minting rotation", i)
		expectedDate := startDate.AddDate(0, 0, 7*i)
		assert.Equal(t, time.Sunday, expectedDate.Weekday(), "shift %d should be on Sunday", i)
		assert.Equal(t, expectedDate.Format("2006-01-02"), s.Date, "shift %d is on the correct consecutive Sunday", i)
	}
}

func TestDefineRota_WithExistingRotations(t *testing.T) {
	existingStart := time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC) // Sunday, Jan 5, 2025
	mock := definableMock()
	mock.rotations = []db.Rotation{
		{
			ID:         "existing-1",
			Start:      "2024-12-15", // Older rotation
			End:        "2025-02-02",
			ShiftCount: 8,
		},
		{
			ID:         "existing-2",
			Start:      existingStart.Format("2006-01-02"), // Most recent
			End:        existingStart.AddDate(0, 0, 7*9).Format("2006-01-02"),
			ShiftCount: 10,
		},
	}

	logger := zap.NewNop()
	ctx := context.Background()

	result, err := DefineRota(ctx, mock, logger, 6)
	require.NoError(t, err)

	// Expected start: the latest rotation's last shift is Mar 9 (Jan 5 + 63 days);
	// the new rota starts the Sunday after, Mar 16
	latestEnd := existingStart.AddDate(0, 0, 7*9)
	expectedStart := nextSunday(latestEnd)

	startDate, err := time.Parse("2006-01-02", result.Rotation.Start)
	require.NoError(t, err)

	assert.Equal(t, expectedStart.Format("2006-01-02"), startDate.Format("2006-01-02"))
	assert.Equal(t, 6, result.Rotation.ShiftCount)
	assert.Len(t, result.Shifts, 6)
}

// Defining a rota spends the Rota Defaults: the Standing Preallocations become
// ordinary Preallocations on the Shifts their rules land on, written in the same
// transaction as the rota itself.
func TestDefineRota_SeedsStandingPreallocations(t *testing.T) {
	// A rota starting on the first Sunday of a month, so a "first Sunday" rule
	// lands on its opening shift and on nothing else in a four-shift run.
	existingEnd := time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC) // Sunday before 2 August
	mock := definableMock()
	mock.rotations = []db.Rotation{
		{ID: "existing", Start: "2026-07-05", End: existingEnd.Format("2006-01-02"), ShiftCount: 4},
	}
	mock.standing = []db.StandingPreallocation{
		{ID: "standing-1", RRule: "FREQ=MONTHLY;BYDAY=1SU", RoleID: "role-service-volunteer", CustomValue: "St John's team"},
	}

	result, err := DefineRota(context.Background(), mock, zap.NewNop(), 4)
	require.NoError(t, err)
	require.Equal(t, "2026-08-02", result.Rotation.Start)

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
	mock := definableMock()

	result, err := DefineRota(context.Background(), mock, zap.NewNop(), 4)
	require.NoError(t, err)
	assert.Empty(t, result.Preallocations)
	require.Len(t, mock.insertedRotas, 1)
}

func TestDefineRota_InvalidShiftCount(t *testing.T) {
	mock := &mockDB{}
	logger := zap.NewNop()
	ctx := context.Background()

	tests := []struct {
		name       string
		shiftCount int
	}{
		{"zero shifts", 0},
		{"negative shifts", -5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := DefineRota(ctx, mock, logger, tt.shiftCount)
			assert.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), "shift count must be positive")
			// Classified as bad input, so callers such as the HTTP API answer
			// 400 rather than 500
			assert.ErrorIs(t, err, ErrInvalidInput)
		})
	}
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

func TestNextSunday(t *testing.T) {
	tests := []struct {
		name     string
		from     time.Time
		expected time.Time
	}{
		{
			name:     "from Tuesday",
			from:     time.Date(2025, 10, 7, 15, 30, 0, 0, time.UTC), // Tuesday
			expected: time.Date(2025, 10, 12, 0, 0, 0, 0, time.UTC),  // Next Sunday
		},
		{
			name:     "from Sunday",
			from:     time.Date(2025, 10, 5, 10, 0, 0, 0, time.UTC), // Sunday
			expected: time.Date(2025, 10, 12, 0, 0, 0, 0, time.UTC), // Next Sunday
		},
		{
			name:     "from Saturday",
			from:     time.Date(2025, 10, 11, 0, 0, 0, 0, time.UTC), // Saturday
			expected: time.Date(2025, 10, 12, 0, 0, 0, 0, time.UTC), // Next Sunday
		},
		{
			name:     "from late Saturday night",
			from:     time.Date(2025, 10, 11, 23, 59, 59, 0, time.UTC), // Saturday 23:59:59
			expected: time.Date(2025, 10, 12, 0, 0, 0, 0, time.UTC),    // Next Sunday (not same day!)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := nextSunday(tt.from)
			assert.Equal(t, tt.expected.Format("2006-01-02"), result.Format("2006-01-02"))
			assert.Equal(t, time.Sunday, result.Weekday())
		})
	}
}

// Defining a rota is where a Shift gets the times it runs at: the drop-in's
// default times of day, written onto each shift's own date (issue #133). They
// are local wall-clock, so the stored zone does not move them and a shift in
// July reads the same as one in January.
func TestDefineRota_WritesShiftTimesFromDefaults(t *testing.T) {
	mock := definableMock()

	result, err := DefineRota(context.Background(), mock, zap.NewNop(), 3)
	require.NoError(t, err)

	require.Len(t, result.Shifts, 3)
	for i, s := range result.Shifts {
		assert.Equal(t, s.Date+"T19:30:00", s.StartAt, "shift %d starts at the default time on its own date", i)
		assert.Equal(t, s.Date+"T21:30:00", s.EndAt, "shift %d ends at the default time on its own date", i)
	}

	// What was returned is what was written.
	require.Len(t, mock.insertedShifts, 1)
	assert.Equal(t, result.Shifts, mock.insertedShifts[0])
}

// A deployment whose admin has not said when the drop-in runs cannot define a
// rota at all, and is told which settings are missing (issue #135). This is the
// second path incomplete settings block, alongside allocation, and it narrows
// ADR 0006's "allocation and nothing else": a Shift with no start is not a
// Shift with unknown hours, it is a Shift on no day at all (ADR 0007).
func TestDefineRota_UnsetDefaultsRefuse(t *testing.T) {
	mock := &mockDB{}

	result, err := DefineRota(context.Background(), mock, zap.NewNop(), 2)
	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "the default shift start time")
	assert.Empty(t, mock.insertedShifts, "nothing is written when a rota cannot be defined")
}

// Settings that cannot be read are a fault rather than an empty answer, so the
// rota is not minted half-timed on the back of one.
func TestDefineRota_SettingsReadFails(t *testing.T) {
	mock := &mockDB{defaultsErr: errors.New("boom")}

	result, err := DefineRota(context.Background(), mock, zap.NewNop(), 2)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Empty(t, mock.insertedShifts, "nothing is written when the settings cannot be read")
}
