package services

import (
	"fmt"
	"time"

	ics "github.com/arran4/golang-ical"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

const calendarRefreshInterval = "PT6H"

// BuildVolunteerCalendar renders the volunteer's shifts as a subscribable
// iCal feed. Pure (no I/O); callers should pass the volunteer's open shifts,
// e.g. FilterShiftsByVolunteer(shifts, volunteer.ID).
//
// Stability matters to polling calendar clients: UIDs are derived from
// volunteer and date so clients update events in place rather than
// duplicating them, SEQUENCE increases with each alteration to the shift, and
// DTSTAMP only changes when the shift changes.
//
// The times come from each shift, which carries the local hours it runs
// between (ADR 0007). The settings supply the zone those hours are read in, so
// a subscriber in another country sees the evening at their own reckoning of
// it, and a shift already minted keeps the hours it was planned for even after
// an admin moves the drop-in's default times.
func BuildVolunteerCalendar(shifts []Shift, volunteer model.Volunteer, roles model.Roles, defaults model.RotaDefaults) (string, error) {
	cal := ics.NewCalendar()
	cal.SetProductId("-//ilford-drop-in//EN")
	cal.SetCalscale("GREGORIAN")
	cal.SetMethod(ics.MethodPublish)
	cal.SetXWRCalName("Ilford Drop-In — " + volunteer.DisplayName)
	cal.SetXWRTimezone(defaults.Timezone())
	cal.SetRefreshInterval(calendarRefreshInterval)
	cal.SetXPublishedTTL(calendarRefreshInterval)

	for _, shift := range shifts {
		// The Role is worth naming in the summary only when it says something
		// the event does not already: being on the shift *is* the uncapped
		// Role, so "(Service volunteer)" on every entry would be noise.
		summary := "Ilford Drop-In shift"
		for _, a := range shift.Assignees {
			if a.VolunteerID != volunteer.ID {
				continue
			}
			if role, ok := roles.ByName(a.Role); ok && role.Capped() {
				summary += " (" + role.Name + ")"
			}
			break
		}

		event := cal.AddEvent(fmt.Sprintf("%s-%s@ilford-drop-in", volunteer.ID, shift.Date))
		stamp, err := setEventDates(event, shift, defaults)
		if err != nil {
			return "", err
		}
		event.SetSummary(summary)
		event.SetSequence(shift.AlterationCount)
		// DTSTAMP must only churn when the shift actually changes; unaltered
		// shifts fall back to their own start.
		if shift.LastChanged.IsZero() {
			event.SetDtStampTime(stamp)
		} else {
			event.SetDtStampTime(shift.LastChanged)
		}
	}

	// RFC 5545 requires CRLF line endings regardless of platform
	return cal.Serialize(ics.WithNewLineWindows), nil
}

// setEventDates gives one event its span and reports the moment DTSTAMP falls
// back to for an unaltered shift.
//
// This is the one place in the app that turns a Shift's wall-clock times into
// instants, which is exactly what ADR 0007 said would happen: the drop-in
// happens in one place, so a Shift's start is a fact about Ilford, and the zone
// is applied where somebody outside Ilford is going to read it.
func setEventDates(event *ics.VEvent, shift Shift, defaults model.RotaDefaults) (time.Time, error) {
	start, end, err := defaults.ShiftInstants(shift.StartAt, shift.EndAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to read the times of shift %s: %w", shift.Date, err)
	}
	event.SetStartAt(start)
	event.SetEndAt(end)
	return start, nil
}
