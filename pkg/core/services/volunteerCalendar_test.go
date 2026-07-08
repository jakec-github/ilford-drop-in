package services

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

var calendarTestCfg = &config.Config{
	ShiftStartTime: "19:30",
	ShiftEndTime:   "21:30",
}

func calendarTestVolunteer() model.Volunteer {
	return model.Volunteer{ID: "alice", DisplayName: "Alice", Role: model.RoleTeamLead}
}

func TestBuildVolunteerCalendar_Basic(t *testing.T) {
	shifts := []Shift{
		{
			Date: "2026-01-12", // GMT: 19:30 London == 19:30 UTC
			Assignees: []ShiftAssignee{
				{VolunteerID: "alice", Name: "Alice", Role: string(model.RoleVolunteer)},
			},
		},
	}

	out, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), calendarTestCfg)
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

	out, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), calendarTestCfg)
	require.NoError(t, err)

	assert.Contains(t, out, "DTSTART:20260112T193000Z")
	assert.Contains(t, out, "DTSTART:20260713T183000Z")
}

func TestBuildVolunteerCalendar_TeamLeadSummary(t *testing.T) {
	shifts := []Shift{
		{
			Date: "2026-01-12",
			Assignees: []ShiftAssignee{
				{VolunteerID: "alice", Name: "Alice", Role: string(model.RoleTeamLead)},
				{VolunteerID: "bob", Name: "Bob", Role: string(model.RoleVolunteer)},
			},
		},
	}

	out, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), calendarTestCfg)
	require.NoError(t, err)
	assert.Contains(t, out, "SUMMARY:Ilford Drop-In shift (team lead)")

	// The same shift from Bob's perspective is not a team-lead event
	bob := model.Volunteer{ID: "bob", DisplayName: "Bob", Role: model.RoleVolunteer}
	out, err = BuildVolunteerCalendar(shifts, bob, calendarTestCfg)
	require.NoError(t, err)
	assert.NotContains(t, out, "(team lead)")
}

func TestBuildVolunteerCalendar_SequenceAndDtstamp(t *testing.T) {
	changed := time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC)
	shifts := []Shift{
		{Date: "2026-01-12", AlterationCount: 3, LastChanged: changed},
	}

	out, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), calendarTestCfg)
	require.NoError(t, err)
	assert.Contains(t, out, "SEQUENCE:3")
	assert.Contains(t, out, "DTSTAMP:20260102T103000Z")
}

func TestBuildVolunteerCalendar_StableAcrossRenders(t *testing.T) {
	shifts := []Shift{
		{Date: "2026-01-12"},
		{Date: "2026-01-19", AlterationCount: 1, LastChanged: time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)},
	}

	first, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), calendarTestCfg)
	require.NoError(t, err)
	second, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), calendarTestCfg)
	require.NoError(t, err)

	assert.Equal(t, first, second, "repeated renders must be byte-identical so polling clients see no phantom changes")
}

func TestBuildVolunteerCalendar_EmptyShifts(t *testing.T) {
	out, err := BuildVolunteerCalendar(nil, calendarTestVolunteer(), calendarTestCfg)
	require.NoError(t, err)
	assert.Contains(t, out, "BEGIN:VCALENDAR")
	assert.NotContains(t, out, "BEGIN:VEVENT")
	assert.Equal(t, 1, strings.Count(out, "BEGIN:VCALENDAR"))
}

func TestBuildVolunteerCalendar_InvalidShiftDate(t *testing.T) {
	shifts := []Shift{{Date: "not-a-date"}}
	_, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), calendarTestCfg)
	assert.Error(t, err)
}
