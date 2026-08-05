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

func TestBuildVolunteerCalendar_Basic(t *testing.T) {
	shifts := []Shift{
		{
			Date: "2026-01-12", // GMT: 19:30 London == 19:30 UTC
			Assignees: []ShiftAssignee{
				{VolunteerID: "alice", Name: "Alice", Role: "Service volunteer"},
			},
		},
	}

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

func TestBuildVolunteerCalendar_DSTBoundary(t *testing.T) {
	shifts := []Shift{
		{Date: "2026-01-12"}, // GMT (UTC+0)
		{Date: "2026-07-13"}, // BST (UTC+1)
	}

	out, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), testRoles, calendarTestDefaults)
	require.NoError(t, err)

	assert.Contains(t, out, "DTSTART:20260112T193000Z")
	assert.Contains(t, out, "DTSTART:20260713T183000Z")
}

func TestBuildVolunteerCalendar_TeamLeadSummary(t *testing.T) {
	shifts := []Shift{
		{
			Date: "2026-01-12",
			Assignees: []ShiftAssignee{
				{VolunteerID: "alice", Name: "Alice", Role: "Team lead"},
				{VolunteerID: "bob", Name: "Bob", Role: "Service volunteer"},
			},
		},
	}

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
	shifts := []Shift{
		{Date: "2026-01-12", AlterationCount: 3, LastChanged: changed},
	}

	out, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), testRoles, calendarTestDefaults)
	require.NoError(t, err)
	assert.Contains(t, out, "SEQUENCE:3")
	assert.Contains(t, out, "DTSTAMP:20260102T103000Z")
}

func TestBuildVolunteerCalendar_StableAcrossRenders(t *testing.T) {
	shifts := []Shift{
		{Date: "2026-01-12"},
		{Date: "2026-01-19", AlterationCount: 1, LastChanged: time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)},
	}

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

func TestBuildVolunteerCalendar_InvalidShiftDate(t *testing.T) {
	shifts := []Shift{{Date: "not-a-date"}}
	_, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), testRoles, calendarTestDefaults)
	assert.Error(t, err)
}

// A drop-in whose shift times an admin has not set yet still has a calendar.
// The day is known and the hours are not, so each shift is an all-day event —
// incomplete settings block allocation and nothing else (ADR 0006), and a
// subscription that has already been added to somebody's phone is not something
// to break while an admin fills a form in.
func TestBuildVolunteerCalendar_ShiftTimesNotSet(t *testing.T) {
	shifts := []Shift{{Date: "2026-01-12"}}

	out, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), testRoles, model.RotaDefaults{})
	require.NoError(t, err)

	assert.Contains(t, out, "UID:alice-2026-01-12@ilford-drop-in")
	assert.Contains(t, out, "DTSTART;VALUE=DATE:20260112")
	// DTEND is exclusive for an all-day event, so the day after is what makes
	// it one day long.
	assert.Contains(t, out, "DTEND;VALUE=DATE:20260113")
	assert.NotContains(t, out, "DTSTART:2026")
	// The zone still falls back, so a client is not left guessing.
	assert.Contains(t, out, "X-WR-TIMEZONE:Europe/London")
}

// Half-filled settings are as unusable as empty ones: a start with no end
// describes nothing, so the shift is drawn as a day rather than as a shift
// running until midnight.
func TestBuildVolunteerCalendar_HalfSetShiftTimes(t *testing.T) {
	shifts := []Shift{{Date: "2026-01-12"}}

	out, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), testRoles,
		model.RotaDefaults{ShiftStartTime: "19:30"})
	require.NoError(t, err)
	assert.Contains(t, out, "DTSTART;VALUE=DATE:20260112")
}
