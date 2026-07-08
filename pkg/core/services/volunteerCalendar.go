package services

import (
	"fmt"

	ics "github.com/arran4/golang-ical"

	"github.com/jakechorley/ilford-drop-in/internal/config"
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
func BuildVolunteerCalendar(shifts []Shift, volunteer model.Volunteer, cfg *config.Config) (string, error) {
	cal := ics.NewCalendar()
	cal.SetProductId("-//ilford-drop-in//EN")
	cal.SetCalscale("GREGORIAN")
	cal.SetMethod(ics.MethodPublish)
	cal.SetXWRCalName("Ilford Drop-In — " + volunteer.DisplayName)
	cal.SetXWRTimezone(config.DefaultShiftTimezone)
	cal.SetRefreshInterval(calendarRefreshInterval)
	cal.SetXPublishedTTL(calendarRefreshInterval)

	for _, shift := range shifts {
		start, end, err := cfg.ShiftTimes(shift.Date)
		if err != nil {
			return "", fmt.Errorf("failed to compute times for shift %s: %w", shift.Date, err)
		}

		summary := "Ilford Drop-In shift"
		for _, a := range shift.Assignees {
			if a.VolunteerID == volunteer.ID && a.Role == string(model.RoleTeamLead) {
				summary += " (team lead)"
				break
			}
		}

		event := cal.AddEvent(fmt.Sprintf("%s-%s@ilford-drop-in", volunteer.ID, shift.Date))
		event.SetStartAt(start)
		event.SetEndAt(end)
		event.SetSummary(summary)
		event.SetSequence(shift.AlterationCount)
		// DTSTAMP must only churn when the shift actually changes; unaltered
		// shifts fall back to their own start time
		if shift.LastChanged.IsZero() {
			event.SetDtStampTime(start)
		} else {
			event.SetDtStampTime(shift.LastChanged)
		}
	}

	// RFC 5545 requires CRLF line endings regardless of platform
	return cal.Serialize(ics.WithNewLineWindows), nil
}
