package utils

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	require.NoError(t, err)
	return d
}

func TestNewRRuleMatcher_MatchAndNoMatch(t *testing.T) {
	// Sundays 5, 12, 19 January 2025.
	shiftDates := []time.Time{
		mustDate(t, "2025-01-05"),
		mustDate(t, "2025-01-12"),
		mustDate(t, "2025-01-19"),
	}

	matcher, err := NewRRuleMatcher("FREQ=WEEKLY;BYDAY=SU", shiftDates)
	require.NoError(t, err)

	assert.True(t, matcher("2025-01-12"), "a Sunday in range should match")
	assert.False(t, matcher("2025-01-13"), "a Monday should not match")
}

func TestNewRRuleMatcher_WindowEdges(t *testing.T) {
	// Sundays 5, 12, 19 January 2025. The search window is widened by a week on
	// each side, so the Sundays one week before the first and after the last
	// shift still match, but dates beyond the buffer do not.
	shiftDates := []time.Time{
		mustDate(t, "2025-01-05"),
		mustDate(t, "2025-01-12"),
		mustDate(t, "2025-01-19"),
	}

	matcher, err := NewRRuleMatcher("FREQ=WEEKLY;BYDAY=SU", shiftDates)
	require.NoError(t, err)

	assert.True(t, matcher("2024-12-29"), "Sunday within the leading buffer should match")
	assert.True(t, matcher("2025-01-26"), "Sunday within the trailing buffer should match")
	assert.False(t, matcher("2024-12-22"), "Sunday beyond the leading buffer should not match")
	assert.False(t, matcher("2025-02-02"), "Sunday beyond the trailing buffer should not match")
}

func TestNewRRuleMatcher_ParseFailure(t *testing.T) {
	matcher, err := NewRRuleMatcher("INVALID_RRULE_SYNTAX", []time.Time{mustDate(t, "2025-01-05")})
	require.Error(t, err)
	assert.Nil(t, matcher)
}

func TestNewRRuleMatcher_EmptyShiftDatesMatchesNothing(t *testing.T) {
	matcher, err := NewRRuleMatcher("FREQ=WEEKLY;BYDAY=SU", nil)
	require.NoError(t, err)
	assert.False(t, matcher("2025-01-05"))
}

func TestNewRRuleMatcher_EmbeddedDTStartIsOverridden(t *testing.T) {
	// An rrule may carry its own DTSTART; the matcher pins the search to the
	// rota window, so occurrences are still found across the shift dates.
	shiftDates := []time.Time{
		mustDate(t, "2025-01-05"),
		mustDate(t, "2025-01-19"),
	}

	matcher, err := NewRRuleMatcher("DTSTART:20200105T000000Z\nRRULE:FREQ=WEEKLY;BYDAY=SU", shiftDates)
	require.NoError(t, err)

	assert.True(t, matcher("2025-01-12"))
}

func TestParseRRule(t *testing.T) {
	rule, err := ParseRRule("FREQ=WEEKLY;BYDAY=SU")
	require.NoError(t, err)
	assert.NotNil(t, rule)

	_, err = ParseRRule("INVALID_RRULE_SYNTAX")
	assert.Error(t, err)
}
