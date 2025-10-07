package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// mockDB implements a test double for db.DB
type mockDB struct {
	rotations      []db.Rotation
	insertedRotas  []*db.Rotation
	getRotationsErr error
	insertErr      error
}

func (m *mockDB) GetRotations(ctx context.Context) ([]db.Rotation, error) {
	if m.getRotationsErr != nil {
		return nil, m.getRotationsErr
	}
	return m.rotations, nil
}

func (m *mockDB) InsertRotation(rotation *db.Rotation) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	m.insertedRotas = append(m.insertedRotas, rotation)
	return nil
}

func TestDefineRota_NoExistingRotations(t *testing.T) {
	mock := &mockDB{
		rotations: []db.Rotation{},
	}

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

	// Check shift dates
	assert.Len(t, result.ShiftDates, 12)
	for i, shiftDate := range result.ShiftDates {
		assert.Equal(t, time.Sunday, shiftDate.Weekday(), "Shift %d should be on Sunday", i)
		expectedDate := startDate.AddDate(0, 0, 7*i)
		assert.Equal(t, expectedDate.Format("2006-01-02"), shiftDate.Format("2006-01-02"))
	}

	// Check rotation was inserted
	assert.Len(t, mock.insertedRotas, 1)
	assert.Equal(t, result.Rotation, mock.insertedRotas[0])
}

func TestDefineRota_WithExistingRotations(t *testing.T) {
	existingStart := time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC) // Sunday, Jan 5, 2025
	mock := &mockDB{
		rotations: []db.Rotation{
			{
				ID:         "existing-1",
				Start:      "2024-12-15", // Older rotation
				ShiftCount: 8,
			},
			{
				ID:         "existing-2",
				Start:      existingStart.Format("2006-01-02"), // Most recent
				ShiftCount: 10,
			},
		},
	}

	logger := zap.NewNop()
	ctx := context.Background()

	result, err := DefineRota(ctx, mock, logger, 6)
	require.NoError(t, err)

	// Expected start: Latest rotation ends after 10 weeks (Jan 5 + 70 days = Mar 16)
	// Next Sunday after Mar 16 is Mar 16 itself (it's already Sunday)
	expectedEnd := existingStart.AddDate(0, 0, 7*10)
	expectedStart := nextSundayAfter(expectedEnd)

	startDate, err := time.Parse("2006-01-02", result.Rotation.Start)
	require.NoError(t, err)

	assert.Equal(t, expectedStart.Format("2006-01-02"), startDate.Format("2006-01-02"))
	assert.Equal(t, 6, result.Rotation.ShiftCount)
	assert.Len(t, result.ShiftDates, 6)
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
		})
	}
}

func TestFindLatestRotation(t *testing.T) {
	rotations := []db.Rotation{
		{ID: "r1", Start: "2025-01-06", ShiftCount: 10},
		{ID: "r2", Start: "2025-03-17", ShiftCount: 8}, // Latest
		{ID: "r3", Start: "2024-12-16", ShiftCount: 12},
	}

	latest := findLatestRotation(rotations)
	require.NotNil(t, latest)
	assert.Equal(t, "r2", latest.ID)
	assert.Equal(t, "2025-03-17", latest.Start)
}

func TestFindLatestRotation_Empty(t *testing.T) {
	latest := findLatestRotation([]db.Rotation{})
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
			from:     time.Date(2025, 10, 5, 10, 0, 0, 0, time.UTC),  // Sunday
			expected: time.Date(2025, 10, 12, 0, 0, 0, 0, time.UTC),  // Next Sunday
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

func TestNextSundayAfter(t *testing.T) {
	tests := []struct {
		name     string
		from     time.Time
		expected time.Time
	}{
		{
			name:     "from Tuesday",
			from:     time.Date(2025, 10, 7, 0, 0, 0, 0, time.UTC),  // Tuesday
			expected: time.Date(2025, 10, 12, 0, 0, 0, 0, time.UTC), // Next Sunday
		},
		{
			name:     "from Sunday",
			from:     time.Date(2025, 10, 5, 0, 0, 0, 0, time.UTC), // Sunday
			expected: time.Date(2025, 10, 5, 0, 0, 0, 0, time.UTC), // Same Sunday
		},
		{
			name:     "from Saturday",
			from:     time.Date(2025, 10, 11, 0, 0, 0, 0, time.UTC), // Saturday
			expected: time.Date(2025, 10, 12, 0, 0, 0, 0, time.UTC), // Next Sunday
		},
		{
			name:     "from late Sunday night",
			from:     time.Date(2025, 10, 5, 23, 59, 59, 0, time.UTC), // Sunday 23:59:59
			expected: time.Date(2025, 10, 5, 0, 0, 0, 0, time.UTC),    // Same Sunday (truncated)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := nextSundayAfter(tt.from)
			assert.Equal(t, tt.expected.Format("2006-01-02"), result.Format("2006-01-02"))
			assert.Equal(t, time.Sunday, result.Weekday())
		})
	}
}
