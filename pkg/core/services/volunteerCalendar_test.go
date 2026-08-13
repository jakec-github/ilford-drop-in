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

// Where the rota lives, as the handler reads it off the request.
const calendarTestURL = "https://word-all.com"

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

	out, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), testRoles, calendarTestDefaults, calendarTestURL)
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

	out, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), testRoles, calendarTestDefaults, calendarTestURL)
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

	out, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), testRoles, calendarTestDefaults, calendarTestURL)
	require.NoError(t, err)

	assert.Contains(t, out, "DTSTART:20260112T180000Z")
	assert.Contains(t, out, "DTEND:20260112T200000Z")
	assert.NotContains(t, out, "DTSTART:20260112T193000Z", "the settings must not move a minted shift")
	// An unaltered shift's DTSTAMP falls back to its own start, so it moves
	// with the shift rather than with the settings.
	assert.Contains(t, out, "DTSTAMP:20260112T180000Z")
}

// The event names the Role the volunteer is doing the shift in, whichever Role
// that is. It used to name only a capped one, on the grounds that being on the
// shift was the uncapped Role already; there is no uncapped Role now (#185).
func TestBuildVolunteerCalendar_RoleSummary(t *testing.T) {
	shift := calendarShift(t, "2026-01-12")
	shift.Assignees = []ShiftAssignee{
		{VolunteerID: "alice", Name: "Alice", Role: "Team lead"},
		{VolunteerID: "bob", Name: "Bob", Role: "Service volunteer"},
	}
	shifts := []Shift{shift}

	out, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), testRoles, calendarTestDefaults, calendarTestURL)
	require.NoError(t, err)
	assert.Contains(t, out, "SUMMARY:Ilford Drop-In shift (Team lead)")

	// The same shift from Bob's perspective names the job he is doing
	bob := model.Volunteer{ID: "bob", DisplayName: "Bob", Roles: []string{"Service volunteer"}}
	out, err = BuildVolunteerCalendar(shifts, bob, testRoles, calendarTestDefaults, calendarTestURL)
	require.NoError(t, err)
	assert.NotContains(t, out, "(Team lead)")
	assert.Contains(t, out, "SUMMARY:Ilford Drop-In shift (Service volunteer)")
}

func TestBuildVolunteerCalendar_SequenceAndDtstamp(t *testing.T) {
	changed := time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC)
	shift := calendarShift(t, "2026-01-12")
	shift.AlterationCount, shift.LastChanged = 3, changed
	shifts := []Shift{shift}

	out, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), testRoles, calendarTestDefaults, calendarTestURL)
	require.NoError(t, err)
	assert.Contains(t, out, "SEQUENCE:3")
	assert.Contains(t, out, "DTSTAMP:20260102T103000Z")
}

func TestBuildVolunteerCalendar_StableAcrossRenders(t *testing.T) {
	altered := calendarShift(t, "2026-01-19")
	altered.AlterationCount, altered.LastChanged = 1, time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	shifts := []Shift{calendarShift(t, "2026-01-12"), altered}

	first, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), testRoles, calendarTestDefaults, calendarTestURL)
	require.NoError(t, err)
	second, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), testRoles, calendarTestDefaults, calendarTestURL)
	require.NoError(t, err)

	assert.Equal(t, first, second, "repeated renders must be byte-identical so polling clients see no phantom changes")
}

func TestBuildVolunteerCalendar_EmptyShifts(t *testing.T) {
	out, err := BuildVolunteerCalendar(nil, calendarTestVolunteer(), testRoles, calendarTestDefaults, calendarTestURL)
	require.NoError(t, err)
	assert.Contains(t, out, "BEGIN:VCALENDAR")
	assert.NotContains(t, out, "BEGIN:VEVENT")
	assert.Equal(t, 1, strings.Count(out, "BEGIN:VCALENDAR"))
}

func TestBuildVolunteerCalendar_InvalidShiftTimes(t *testing.T) {
	shifts := []Shift{{Date: "2026-01-12", StartAt: "half seven", EndAt: "half nine"}}
	_, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), testRoles, calendarTestDefaults, calendarTestURL)
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

	out, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), testRoles, model.RotaDefaults{}, calendarTestURL)
	require.NoError(t, err)

	assert.Contains(t, out, "DTSTART:20260112T193000Z")
	assert.Contains(t, out, "X-WR-TIMEZONE:Europe/London")
}

// unfolded undoes RFC 5545 line folding — CRLF and one space of continuation —
// so a test can assert about a property longer than 75 octets without knowing
// where the wrap landed.
func unfolded(ics string) string {
	return strings.ReplaceAll(ics, "\r\n ", "")
}

// The event says who else is on it. That is the question a volunteer opens
// their calendar to answer, and until now the entry could not answer it.
func TestBuildVolunteerCalendar_DescribesWhoIsOn(t *testing.T) {
	shift := calendarShift(t, "2026-01-12")
	shift.Assignees = []ShiftAssignee{
		{VolunteerID: "alice", Name: "Alice", Role: "Team lead"},
		{VolunteerID: "bob", Name: "Bob", Role: "Service volunteer"},
		{CustomEntry: "Redbridge youth group", Name: "Redbridge youth group", Role: "Service volunteer"},
	}

	out, err := BuildVolunteerCalendar([]Shift{shift}, calendarTestVolunteer(), testRoles, calendarTestDefaults, calendarTestURL)
	require.NoError(t, err)
	body := unfolded(out)

	assert.Contains(t, body, "You are on as Team lead.")
	assert.Contains(t, body, "On with you:")
	assert.Contains(t, body, "Bob — Service volunteer")
	// A custom entry is a body on the shift like any other, so it is listed.
	assert.Contains(t, body, "Redbridge youth group — Service volunteer")
	// The reader is not listed among the people they are on with.
	assert.NotContains(t, body, "Alice — Team lead")
	assert.Contains(t, body, "The whole rota: "+calendarTestURL)
	assert.Contains(t, body, "URL:"+calendarTestURL)
}

// A shift with nobody else on it says so, rather than carrying a heading with
// nothing under it — which would read as a feed that had lost the rest.
func TestBuildVolunteerCalendar_DescribesALoneShift(t *testing.T) {
	shift := calendarShift(t, "2026-01-12")
	shift.Assignees = []ShiftAssignee{{VolunteerID: "alice", Name: "Alice", Role: "Team lead"}}

	out, err := BuildVolunteerCalendar([]Shift{shift}, calendarTestVolunteer(), testRoles, calendarTestDefaults, "")
	require.NoError(t, err)
	body := unfolded(out)

	assert.Contains(t, body, "Nobody else is on this shift yet.")
	assert.NotContains(t, body, "On with you:")
	// No URL to link to, so no half-written link and no URL property.
	assert.NotContains(t, body, "The whole rota:")
	assert.NotContains(t, body, "URL:")
}

// Commas and newlines are escaped by the serialiser, not written raw: a name
// with a comma in it would otherwise turn one description into two values.
func TestBuildVolunteerCalendar_EscapesDescriptionText(t *testing.T) {
	shift := calendarShift(t, "2026-01-12")
	shift.Assignees = []ShiftAssignee{
		{VolunteerID: "alice", Name: "Alice", Role: "Team lead"},
		{VolunteerID: "bob", Name: "Bob, jr", Role: "Service volunteer"},
	}

	out, err := BuildVolunteerCalendar([]Shift{shift}, calendarTestVolunteer(), testRoles, calendarTestDefaults, calendarTestURL)
	require.NoError(t, err)
	body := unfolded(out)

	assert.Contains(t, body, `Bob\, jr`)
	assert.Contains(t, body, `You are on as Team lead.\n`)
	assert.NotContains(t, body, `\\n`, "escaped once, not twice")
}

// Every event carries its reminders. Whether a client acts on them is the
// client's business — Google ignores alarms on a subscribed calendar — but a
// feed that ships none can never remind anybody.
func TestBuildVolunteerCalendar_ShipsReminders(t *testing.T) {
	shifts := []Shift{calendarShift(t, "2026-01-12")}

	out, err := BuildVolunteerCalendar(shifts, calendarTestVolunteer(), testRoles, calendarTestDefaults, calendarTestURL)
	require.NoError(t, err)

	assert.Equal(t, 2, strings.Count(out, "BEGIN:VALARM"))
	assert.Equal(t, 2, strings.Count(out, "END:VALARM"))
	assert.Contains(t, out, "ACTION:DISPLAY")
	assert.Contains(t, out, "TRIGGER:-P1D")
	assert.Contains(t, out, "TRIGGER:-PT2H")
	// The alarms sit inside the event they belong to, not beside it.
	event := out[strings.Index(out, "BEGIN:VEVENT"):strings.Index(out, "END:VEVENT")]
	assert.Equal(t, 2, strings.Count(event, "BEGIN:VALARM"))
}
