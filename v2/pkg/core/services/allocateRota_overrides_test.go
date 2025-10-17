package services

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
)

func TestConvertRotaOverrides_WeeklyOverride(t *testing.T) {
	shiftSize := 3
	configOverrides := []config.RotaOverride{
		{
			RRule:                "FREQ=WEEKLY;BYDAY=SU",
			PrefilledAllocations: []string{"external_john", "external_jane"},
			ShiftSize:            &shiftSize,
		},
	}

	// Create a sample rota date range (January 2024)
	shiftDates := []time.Time{
		time.Date(2024, 1, 7, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 14, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 21, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 28, 0, 0, 0, 0, time.UTC),
	}

	logger := zap.NewNop()
	allocatorOverrides, err := convertRotaOverrides(configOverrides, shiftDates, logger)

	require.NoError(t, err)
	require.Len(t, allocatorOverrides, 1)

	override := allocatorOverrides[0]

	// Test that Sunday dates match
	assert.True(t, override.AppliesTo("2024-01-07"), "Should match Sunday 2024-01-07")
	assert.True(t, override.AppliesTo("2024-01-14"), "Should match Sunday 2024-01-14")
	assert.True(t, override.AppliesTo("2024-01-21"), "Should match Sunday 2024-01-21")

	// Test that non-Sunday dates don't match
	assert.False(t, override.AppliesTo("2024-01-08"), "Should not match Monday 2024-01-08")
	assert.False(t, override.AppliesTo("2024-01-09"), "Should not match Tuesday 2024-01-09")
	assert.False(t, override.AppliesTo("2024-01-10"), "Should not match Wednesday 2024-01-10")

	// Verify shift size
	require.NotNil(t, override.ShiftSize)
	assert.Equal(t, 3, *override.ShiftSize)

	// Verify pre-allocated volunteers
	require.Len(t, override.PreAllocatedVolunteers, 2)
	assert.Contains(t, override.PreAllocatedVolunteers, "external_john")
	assert.Contains(t, override.PreAllocatedVolunteers, "external_jane")
}

func TestConvertRotaOverrides_SpecificDate(t *testing.T) {
	// Use a specific date override (first Monday of January)
	configOverrides := []config.RotaOverride{
		{
			RRule:                "FREQ=YEARLY;BYMONTH=1;BYDAY=1MO",
			PrefilledAllocations: []string{"holiday_cover"},
			ShiftSize:            nil,
		},
	}

	// Create a sample rota date range spanning multiple years
	shiftDates := []time.Time{
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC),
	}

	logger := zap.NewNop()
	allocatorOverrides, err := convertRotaOverrides(configOverrides, shiftDates, logger)

	require.NoError(t, err)
	require.Len(t, allocatorOverrides, 1)

	override := allocatorOverrides[0]

	// 2024-01-01 is the first Monday of January 2024
	assert.True(t, override.AppliesTo("2024-01-01"), "Should match first Monday of January 2024")

	// 2025-01-06 is the first Monday of January 2025
	assert.True(t, override.AppliesTo("2025-01-06"), "Should match first Monday of January 2025")

	// Other dates should not match
	assert.False(t, override.AppliesTo("2024-01-08"), "Should not match second Monday")
	assert.False(t, override.AppliesTo("2024-02-05"), "Should not match first Monday of February")

	// Verify shift size is nil (use default)
	assert.Nil(t, override.ShiftSize)

	// Verify pre-allocated volunteer
	require.Len(t, override.PreAllocatedVolunteers, 1)
	assert.Equal(t, "holiday_cover", override.PreAllocatedVolunteers[0])
}

func TestConvertRotaOverrides_MultipleOverrides(t *testing.T) {
	shiftSize1 := 4
	shiftSize2 := 2

	configOverrides := []config.RotaOverride{
		{
			RRule:                "FREQ=WEEKLY;BYDAY=SA",
			PrefilledAllocations: []string{"weekend_team"},
			ShiftSize:            &shiftSize1,
		},
		{
			RRule:                "FREQ=MONTHLY;BYDAY=-1FR",
			PrefilledAllocations: []string{"special_event"},
			ShiftSize:            &shiftSize2,
		},
	}

	// Create a sample rota date range
	shiftDates := []time.Time{
		time.Date(2024, 1, 6, 0, 0, 0, 0, time.UTC),  // Saturday
		time.Date(2024, 1, 7, 0, 0, 0, 0, time.UTC),  // Sunday
		time.Date(2024, 1, 19, 0, 0, 0, 0, time.UTC), // Friday (not last)
		time.Date(2024, 1, 26, 0, 0, 0, 0, time.UTC), // Last Friday
	}

	logger := zap.NewNop()
	allocatorOverrides, err := convertRotaOverrides(configOverrides, shiftDates, logger)

	require.NoError(t, err)
	require.Len(t, allocatorOverrides, 2)

	// Test first override (Saturdays)
	saturday := allocatorOverrides[0]
	assert.True(t, saturday.AppliesTo("2024-01-06"), "Should match Saturday")
	assert.False(t, saturday.AppliesTo("2024-01-07"), "Should not match Sunday")
	assert.Equal(t, 4, *saturday.ShiftSize)

	// Test second override (last Friday of month)
	lastFriday := allocatorOverrides[1]
	assert.True(t, lastFriday.AppliesTo("2024-01-26"), "Should match last Friday of January 2024")
	assert.False(t, lastFriday.AppliesTo("2024-01-19"), "Should not match earlier Friday")
	assert.Equal(t, 2, *lastFriday.ShiftSize)
}

func TestConvertRotaOverrides_InvalidRRule(t *testing.T) {
	configOverrides := []config.RotaOverride{
		{
			RRule:                "INVALID_RRULE_SYNTAX",
			PrefilledAllocations: []string{"test"},
		},
	}

	shiftDates := []time.Time{
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	logger := zap.NewNop()
	_, err := convertRotaOverrides(configOverrides, shiftDates, logger)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse rrule")
}

func TestConvertRotaOverrides_EmptyList(t *testing.T) {
	configOverrides := []config.RotaOverride{}

	shiftDates := []time.Time{
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	logger := zap.NewNop()
	allocatorOverrides, err := convertRotaOverrides(configOverrides, shiftDates, logger)

	require.NoError(t, err)
	assert.Empty(t, allocatorOverrides)
}

func TestConvertRotaOverrides_NoPreallocations(t *testing.T) {
	shiftSize := 5
	configOverrides := []config.RotaOverride{
		{
			RRule:                "FREQ=WEEKLY;BYDAY=MO",
			PrefilledAllocations: []string{},
			ShiftSize:            &shiftSize,
		},
	}

	shiftDates := []time.Time{
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC),
	}

	logger := zap.NewNop()
	allocatorOverrides, err := convertRotaOverrides(configOverrides, shiftDates, logger)

	require.NoError(t, err)
	require.Len(t, allocatorOverrides, 1)

	override := allocatorOverrides[0]
	assert.Empty(t, override.PreAllocatedVolunteers)
	assert.NotNil(t, override.ShiftSize)
	assert.Equal(t, 5, *override.ShiftSize)
}

func TestConvertRotaOverrides_YearSpanningRota(t *testing.T) {
	shiftSize := 4
	configOverrides := []config.RotaOverride{
		{
			// Every Sunday
			RRule:                "FREQ=WEEKLY;BYDAY=SU",
			PrefilledAllocations: []string{"weekend_volunteer"},
			ShiftSize:            &shiftSize,
		},
	}

	// Create a rota that spans from December 2024 to February 2025
	shiftDates := []time.Time{
		time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC),  // Sunday, Dec 1
		time.Date(2024, 12, 8, 0, 0, 0, 0, time.UTC),  // Sunday, Dec 8
		time.Date(2024, 12, 15, 0, 0, 0, 0, time.UTC), // Sunday, Dec 15
		time.Date(2024, 12, 22, 0, 0, 0, 0, time.UTC), // Sunday, Dec 22
		time.Date(2024, 12, 29, 0, 0, 0, 0, time.UTC), // Sunday, Dec 29
		time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC),   // Sunday, Jan 5
		time.Date(2025, 1, 12, 0, 0, 0, 0, time.UTC),  // Sunday, Jan 12
		time.Date(2025, 1, 19, 0, 0, 0, 0, time.UTC),  // Sunday, Jan 19
		time.Date(2025, 2, 2, 0, 0, 0, 0, time.UTC),   // Sunday, Feb 2
		time.Date(2025, 2, 9, 0, 0, 0, 0, time.UTC),   // Sunday, Feb 9
	}

	logger := zap.NewNop()
	allocatorOverrides, err := convertRotaOverrides(configOverrides, shiftDates, logger)

	require.NoError(t, err)
	require.Len(t, allocatorOverrides, 1)

	override := allocatorOverrides[0]

	// Test all Sundays across the year boundary
	assert.True(t, override.AppliesTo("2024-12-01"), "Should match Sunday in Dec 2024")
	assert.True(t, override.AppliesTo("2024-12-08"), "Should match Sunday in Dec 2024")
	assert.True(t, override.AppliesTo("2024-12-15"), "Should match Sunday in Dec 2024")
	assert.True(t, override.AppliesTo("2024-12-22"), "Should match Sunday in Dec 2024")
	assert.True(t, override.AppliesTo("2024-12-29"), "Should match Sunday in Dec 2024")
	assert.True(t, override.AppliesTo("2025-01-05"), "Should match Sunday in Jan 2025")
	assert.True(t, override.AppliesTo("2025-01-12"), "Should match Sunday in Jan 2025")
	assert.True(t, override.AppliesTo("2025-01-19"), "Should match Sunday in Jan 2025")
	assert.True(t, override.AppliesTo("2025-02-02"), "Should match Sunday in Feb 2025")
	assert.True(t, override.AppliesTo("2025-02-09"), "Should match Sunday in Feb 2025")

	// Test non-Sundays don't match
	assert.False(t, override.AppliesTo("2024-12-02"), "Should not match Monday")
	assert.False(t, override.AppliesTo("2025-01-06"), "Should not match Monday")
	assert.False(t, override.AppliesTo("2025-02-03"), "Should not match Monday")

	// Verify override properties
	assert.NotNil(t, override.ShiftSize)
	assert.Equal(t, 4, *override.ShiftSize)
	require.Len(t, override.PreAllocatedVolunteers, 1)
	assert.Equal(t, "weekend_volunteer", override.PreAllocatedVolunteers[0])
}
