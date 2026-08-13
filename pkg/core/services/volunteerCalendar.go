package services

import (
	"fmt"
	"strings"
	"time"

	ics "github.com/arran4/golang-ical"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

const calendarRefreshInterval = "PT6H"

// The reminders every shift event carries, as offsets from its start. Two,
// because they answer different questions: the evening before is "am I doing
// anything tomorrow", and the two hours is "I am doing it now".
//
// Whether a subscriber ever sees one is their calendar's decision, not ours.
// Apple Calendar honours the alarms on a subscribed calendar (unless the
// subscription is set to strip them); Google Calendar ignores alarms on a
// calendar added by URL and notifies for nothing in it. Shipping them is still
// right — they cost two lines per event, they are what the standard says, and
// the clients that do honour them are the ones a volunteer is most likely to be
// reading this on.
var calendarReminders = []struct {
	trigger     string
	description string
}{
	{trigger: "-P1D", description: "Ilford Drop-In shift tomorrow"},
	{trigger: "-PT2H", description: "Ilford Drop-In shift in two hours"},
}

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
//
// rotaURL is where the rota lives, for the link on each event. Empty leaves the
// link out rather than writing a broken one.
func BuildVolunteerCalendar(shifts []Shift, volunteer model.Volunteer, roles model.Roles, defaults model.RotaDefaults, rotaURL string) (string, error) {
	cal := ics.NewCalendar()
	cal.SetProductId("-//ilford-drop-in//EN")
	cal.SetCalscale("GREGORIAN")
	cal.SetMethod(ics.MethodPublish)
	cal.SetXWRCalName("Ilford Drop-In — " + volunteer.DisplayName)
	cal.SetXWRTimezone(defaults.Timezone())
	cal.SetRefreshInterval(calendarRefreshInterval)
	cal.SetXPublishedTTL(calendarRefreshInterval)

	for _, shift := range shifts {
		// The summary names the Role the volunteer is doing the shift in.
		// It used to name only a capped one, on the grounds that being on the
		// shift *was* the uncapped Role and saying so would be noise; with no
		// uncapped Role to be (issue #185) every job is worth naming, and an
		// allocation whose Role the app does not recognise still says nothing.
		summary := "Ilford Drop-In shift"
		if role, named := ownRole(shift, volunteer, roles); named {
			summary += " (" + role + ")"
		}

		event := cal.AddEvent(fmt.Sprintf("%s-%s@ilford-drop-in", volunteer.ID, shift.Date))
		stamp, err := setEventDates(event, shift, defaults)
		if err != nil {
			return "", err
		}
		event.SetSummary(summary)
		// Newlines and commas go in as themselves: DESCRIPTION is a TEXT
		// property, and the library escapes those on the way out.
		event.SetDescription(shiftDescription(shift, volunteer, roles, rotaURL))
		if rotaURL != "" {
			event.SetURL(rotaURL)
		}
		addReminders(event)
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

// ownRole is the Role this volunteer is doing the shift in, and whether it is
// one the app can name. A Role the roster no longer holds — renamed since the
// rota was allocated — is reported as unnamed rather than as itself: the event
// would otherwise say a job nothing in the app answers to.
func ownRole(shift Shift, volunteer model.Volunteer, roles model.Roles) (string, bool) {
	for _, a := range shift.Assignees {
		if a.VolunteerID != volunteer.ID {
			continue
		}
		if role, ok := roles.ByName(a.Role); ok {
			return role.Name, true
		}
		return "", false
	}
	return "", false
}

// shiftDescription is the body of one event: what this volunteer is doing, who
// else is on with them, and where to go to read the rota.
//
// Who else is on is the thing a volunteer actually wants from a calendar entry
// and the one thing the event could not say before. It is worth carrying even
// though it dates: a subscription re-reads every few hours, so a swap made on
// Thursday is in everybody's calendar by Friday, and the SEQUENCE bump that
// comes with an alteration is what makes clients take the new copy.
//
// Custom entries are listed like anybody else. They are a real body on the
// shift — a visiting group, somebody off the roster — and leaving them out
// would make a full shift read as a short one.
func shiftDescription(shift Shift, volunteer model.Volunteer, roles model.Roles, rotaURL string) string {
	var lines []string

	if role, named := ownRole(shift, volunteer, roles); named {
		// The Role as an admin spelled it, rather than lower-cased to fit the
		// sentence: a Role can be an acronym, and renaming somebody's job to
		// make the grammar tidy is the wrong trade.
		lines = append(lines, "You are on as "+role+".")
	} else {
		lines = append(lines, "You are on this shift.")
	}

	others := make([]string, 0, len(shift.Assignees))
	for _, a := range shift.Assignees {
		if a.VolunteerID != "" && a.VolunteerID == volunteer.ID {
			continue
		}
		if _, ok := roles.ByName(a.Role); ok {
			others = append(others, a.Name+" — "+a.Role)
		} else {
			others = append(others, a.Name)
		}
	}

	if len(others) > 0 {
		lines = append(lines, "", "On with you:")
		lines = append(lines, others...)
	} else {
		// Said out loud, because an event listing nobody could equally mean the
		// feed forgot to. A shift with one person on it is also worth an admin
		// hearing about, and this is the copy that prompts it.
		lines = append(lines, "", "Nobody else is on this shift yet.")
	}

	if rotaURL != "" {
		lines = append(lines, "", "The whole rota: "+rotaURL)
	}

	return strings.Join(lines, "\n")
}

// addReminders hangs the standard reminders off one event.
func addReminders(event *ics.VEvent) {
	for _, reminder := range calendarReminders {
		alarm := event.AddAlarm()
		alarm.SetAction(ics.ActionDisplay)
		alarm.SetTrigger(reminder.trigger)
		alarm.SetDescription(reminder.description)
	}
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
