package services

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

// The settings a configured drop-in has: the evening session the real one runs.
var calendarTestDefaults = model.RotaDefaults{
	ShiftStartTime: "19:30",
	ShiftEndTime:   "21:30",
	ShiftTimezone:  "Europe/London",
}

func calendarTestVolunteer() model.Volunteer {
	return model.Volunteer{ID: "alice", DisplayName: "Alice", Roles: []string{"Team lead", "Service volunteer"}}
}

// calendarShift is a shift as the listing hands one over: minted with the
// drop-in's default times written onto its date, which is what defineRota does.
func calendarShift(t *testing.T, date string) Shift {
	t.Helper()
	start, end, err := model.ShiftTimestamps(date, calendarTestDefaults.ShiftStartTime, calendarTestDefaults.ShiftEndTime)
	require.NoError(t, err)
	return Shift{Date: date, StartAt: start, EndAt: end}
}

func TestBuildVolunteerCalendar_Basic(t *testing.T) {
	shift := calendarShift(t, "2026-01-12") // GMT: 19:30 London == 19:30 UTC
	shift.Assignees = []ShiftAssignee{
		{VolunteerID: "alice", Name: "Alice", Role: "Service volunteer"},
	}
	shifts := []Shift{shift}

	out, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), testRoles, calendarTestDefaults)
	require.NoError(t, err)

	assert.Contains(t, out, "BEGIN:VCALENDAR")
	assert.Contains(t, out, "END:VCALENDAR")
	assert.Contains(t, out, "VERSION:2.0")
	assert.Contains(t, out, "PRODID:-//ilford-drop-in//EN")
	assert.Contains(t, out, "METHOD:PUBLISH")
	assert.Contains(t, out, "X-WR-TIMEZONE:Europe/London")
	assert.Contains(t, out, "REFRESH-INTERVAL;VALUE=DURATION:PT6H")
	assert.Contains(t, out, "X-PUBLISHED-TTL:PT6H")

	assert.Contains(t, out, "UID:alice-2026-01-12@ilford-drop-in")
	assert.Contains(t, out, "DTSTART:20260112T193000Z")
	assert.Contains(t, out, "DTEND:20260112T213000Z")
	assert.Contains(t, out, "SUMMARY:Ilford Drop-In shift")
	assert.Contains(t, out, "SEQUENCE:0")
	// Unaltered shift: DTSTAMP falls back to the shift start
	assert.Contains(t, out, "DTSTAMP:20260112T193000Z")

	// Calendar name is folded across lines by the em dash, so check the prefix
	assert.Contains(t, out, "X-WR-CALNAME:Ilford Drop-In")

	// RFC 5545 requires CRLF line endings
	assert.Contains(t, out, "\r\n")
}

// A shift's times are wall-clock in the drop-in's zone, so the same stored
// 19:30 is a different instant either side of the DST boundary. This is what a
// subscriber's client has to be told, and the reason the zone is a setting.
func TestBuildVolunteerCalendar_DSTBoundary(t *testing.T) {
	shifts := []Shift{
		calendarShift(t, "2026-01-12"), // GMT (UTC+0)
		calendarShift(t, "2026-07-13"), // BST (UTC+1)
	}

	out, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), testRoles, calendarTestDefaults)
	require.NoError(t, err)

	assert.Contains(t, out, "DTSTART:20260112T193000Z")
	assert.Contains(t, out, "DTSTART:20260713T183000Z")
}

// The event runs between the shift's own times, not between the times the
// settings currently hold. A shift minted before an admin moved the drop-in an
// hour later keeps the hour it was minted with (ADR 0007), and a subscriber
// sees the evening that was actually planned.
func TestBuildVolunteerCalendar_ReadsTheShiftsOwnTimes(t *testing.T) {
	shifts := []Shift{{
		Date:    "2026-01-12",
		StartAt: "2026-01-12T18:00:00",
		EndAt:   "2026-01-12T20:00:00",
	}}

	out, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), testRoles, calendarTestDefaults)
	require.NoError(t, err)

	assert.Contains(t, out, "DTSTART:20260112T180000Z")
	assert.Contains(t, out, "DTEND:20260112T200000Z")
	assert.NotContains(t, out, "DTSTART:20260112T193000Z", "the settings must not move a minted shift")
	// An unaltered shift's DTSTAMP falls back to its own start, so it moves
	// with the shift rather than with the settings.
	assert.Contains(t, out, "DTSTAMP:20260112T180000Z")
}

func TestBuildVolunteerCalendar_TeamLeadSummary(t *testing.T) {
	shift := calendarShift(t, "2026-01-12")
	shift.Assignees = []ShiftAssignee{
		{VolunteerID: "alice", Name: "Alice", Role: "Team lead"},
		{VolunteerID: "bob", Name: "Bob", Role: "Service volunteer"},
	}
	shifts := []Shift{shift}

	out, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), testRoles, calendarTestDefaults)
	require.NoError(t, err)
	assert.Contains(t, out, "SUMMARY:Ilford Drop-In shift (Team lead)")

	// The same shift from Bob's perspective is not a team-lead event
	bob := model.Volunteer{ID: "bob", DisplayName: "Bob", Roles: []string{"Service volunteer"}}
	out, err = BuildVolunteerCalendar(shifts, bob, testRoles, calendarTestDefaults)
	require.NoError(t, err)
	assert.NotContains(t, out, "(Team lead)")
	assert.NotContains(t, out, "(Service volunteer)",
		"the uncapped Role is what being on the shift already means")
}

func TestBuildVolunteerCalendar_SequenceAndDtstamp(t *testing.T) {
	changed := time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC)
	shift := calendarShift(t, "2026-01-12")
	shift.AlterationCount, shift.LastChanged = 3, changed
	shifts := []Shift{shift}

	out, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), testRoles, calendarTestDefaults)
	require.NoError(t, err)
	assert.Contains(t, out, "SEQUENCE:3")
	assert.Contains(t, out, "DTSTAMP:20260102T103000Z")
}

func TestBuildVolunteerCalendar_StableAcrossRenders(t *testing.T) {
	altered := calendarShift(t, "2026-01-19")
	altered.AlterationCount, altered.LastChanged = 1, time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	shifts := []Shift{calendarShift(t, "2026-01-12"), altered}

	first, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), testRoles, calendarTestDefaults)
	require.NoError(t, err)
	second, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), testRoles, calendarTestDefaults)
	require.NoError(t, err)

	assert.Equal(t, first, second, "repeated renders must be byte-identical so polling clients see no phantom changes")
}

func TestBuildVolunteerCalendar_EmptyShifts(t *testing.T) {
	out, err := BuildVolunteerCalendar(nil, calendarTestVolunteer(), testRoles, calendarTestDefaults)
	require.NoError(t, err)
	assert.Contains(t, out, "BEGIN:VCALENDAR")
	assert.NotContains(t, out, "BEGIN:VEVENT")
	assert.Equal(t, 1, strings.Count(out, "BEGIN:VCALENDAR"))
}

func TestBuildVolunteerCalendar_InvalidShiftTimes(t *testing.T) {
	shifts := []Shift{{Date: "2026-01-12", StartAt: "half seven", EndAt: "half nine"}}
	_, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), testRoles, calendarTestDefaults)
	assert.Error(t, err)
}

// Settings nobody has filled in are the ordinary first state of a deployment,
// and the feed still answers: the zone falls back rather than blocking, so the
// hours the shifts themselves carry are still readable and a client is not left
// guessing what the calendar's own zone is.
func TestBuildVolunteerCalendar_SettingsNotSet(t *testing.T) {
	shifts := []Shift{{
		Date:    "2026-01-12",
		StartAt: "2026-01-12T19:30:00",
		EndAt:   "2026-01-12T21:30:00",
	}}

	out, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), testRoles, model.RotaDefaults{})
	require.NoError(t, err)

	assert.Contains(t, out, "DTSTART:20260112T193000Z")
	assert.Contains(t, out, "X-WR-TIMEZONE:Europe/London")
}
