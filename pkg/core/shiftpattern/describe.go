package shiftpattern

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/teambition/rrule-go"
)

// The plain-English reading of an entry or a rule is what makes RRULE usable by
// somebody who does not read RRULE (ADR 0013): they write one, or paste one,
// and the screen says back what it means — including the date it starts from,
// and for an entry how long its Shifts run.

var (
	dayNames = []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"}
	ordinals = map[int]string{
		1: "first", 2: "second", 3: "third", 4: "fourth", 5: "fifth",
		-1: "last", -2: "second-to-last", -3: "third-to-last",
	}
	units = map[rrule.Frequency][2]string{
		rrule.DAILY:   {"day", "days"},
		rrule.WEEKLY:  {"week", "weeks"},
		rrule.MONTHLY: {"month", "months"},
		rrule.YEARLY:  {"year", "years"},
	}
)

// describe says in plain English when a rule's Shifts start, counting from
// anchor. It is only asked about rules parseRule accepted.
//
// An entry's anchor is its DTSTART, a time: what the rule leaves out — the
// weekday, the day of the month, the time — comes from it without comment,
// since the description ends by saying the date. An Override's anchor is a
// date, and what that fills in is said in brackets, because it is not what the
// rule says. A zero length says nothing about how long.
func describe(option rrule.ROption, anchor time.Time, length time.Duration, isEntry bool) string {
	unit := units[option.Freq]
	var b strings.Builder

	var plain, positioned []string
	for _, day := range option.Byweekday {
		if day.N() == 0 {
			plain = append(plain, dayNames[day.Day()])
		} else {
			positioned = append(positioned, "the "+ordinal(day.N())+" "+dayNames[day.Day()])
		}
	}
	plainDays := joinWithAnd(plain)
	if isWorkingWeek(plain) {
		plainDays = "weekday"
	}
	anchorDay := dayNames[(int(anchor.Weekday())+6)%7]

	// "Every Sunday" for the plainest weekly rule, as a person would say it.
	weekly := option.Freq == rrule.WEEKLY && option.Interval == 1 && len(positioned) == 0 && len(option.Bysetpos) == 0
	switch {
	case weekly && len(plain) > 0:
		b.WriteString("Every " + plainDays)
		plain = nil
	case weekly:
		b.WriteString("Every " + anchorDay)
		if !isEntry {
			b.WriteString(" (the weekday it starts from)")
		}
	case option.Interval == 1:
		b.WriteString("Every " + unit[0])
	default:
		fmt.Fprintf(&b, "Every %d %s", option.Interval, unit[1])
	}

	if len(plain) > 0 {
		if len(option.Bysetpos) > 0 {
			var picks []string
			for _, n := range option.Bysetpos {
				picks = append(picks, ordinal(n))
			}
			if plainDays == "weekday" {
				b.WriteString(" on the " + joinWithAnd(picks) + " weekday")
			} else {
				b.WriteString(" on the " + joinWithAnd(picks) + " of " + plainDays)
			}
		} else {
			b.WriteString(" on " + plainDays)
		}
	}
	if len(positioned) > 0 {
		b.WriteString(" on " + joinWithAnd(positioned))
		if option.Freq == rrule.YEARLY && len(option.Bymonth) == 0 {
			b.WriteString(" of the year")
		}
	}

	if len(option.Bymonthday) > 0 {
		var days []string
		for _, n := range option.Bymonthday {
			switch {
			case n > 0:
				days = append(days, "the "+numbered(n))
			case n == -1:
				days = append(days, "the last day")
			default:
				days = append(days, "the "+numbered(-n)+"-from-last day")
			}
		}
		b.WriteString(" on " + joinWithAnd(days))
	}
	if len(option.Bysetpos) > 0 && len(option.Byweekday) == 0 {
		var picks []string
		for _, n := range option.Bysetpos {
			picks = append(picks, ordinal(n))
		}
		b.WriteString(", only the " + joinWithAnd(picks) + " of each " + unit[0])
	}

	if len(option.Bymonth) > 0 {
		var months []string
		for _, m := range option.Bymonth {
			months = append(months, time.Month(m).String())
		}
		b.WriteString(" in " + joinWithAnd(months))
	}

	// What the rule leaves out, its start fills in.
	noDay := len(option.Byweekday) == 0 && len(option.Bymonthday) == 0
	switch {
	case option.Freq == rrule.WEEKLY && len(option.Byweekday) == 0 && !weekly:
		b.WriteString(" on " + anchorDay)
		if !isEntry {
			b.WriteString(" (the weekday it starts from)")
		}
	case (option.Freq == rrule.MONTHLY || option.Freq == rrule.YEARLY && len(option.Bymonth) > 0) && noDay:
		b.WriteString(" on the " + numbered(anchor.Day()))
		if !isEntry {
			b.WriteString(" (the day of the month it starts from)")
		}
	case option.Freq == rrule.YEARLY && noDay:
		fmt.Fprintf(&b, " on %d %s", anchor.Day(), anchor.Month())
		if !isEntry {
			b.WriteString(" (the date it starts from)")
		}
	}

	// An entry's time is its DTSTART's where the rule names none. An
	// Override's rule may name no time, and then it means the whole day.
	hours, minutes := option.Byhour, option.Byminute
	if isEntry && len(hours) == 0 {
		hours = []int{anchor.Hour()}
	}
	if isEntry && len(minutes) == 0 {
		minutes = []int{anchor.Minute()}
	}
	var times []string
	for _, h := range hours {
		for _, m := range minutes {
			times = append(times, fmt.Sprintf("%02d:%02d", h, m))
		}
	}
	if len(times) > 0 {
		b.WriteString(" at " + joinWithAnd(times))
	}
	if length > 0 {
		b.WriteString(" for " + readableLength(length))
	}

	if option.Freq == rrule.MONTHLY && noDay && anchor.Day() > 28 {
		b.WriteString(", skipping months without one")
	}

	if option.Count > 0 {
		fmt.Fprintf(&b, ", %d time%s", option.Count, plural(option.Count))
	}
	if !option.Until.IsZero() {
		b.WriteString(", until " + option.Until.Format("Mon 2 Jan 2006"))
	}
	b.WriteString(", from " + anchor.Format("2 Jan 2006"))
	return b.String()
}

// isWorkingWeek is whether days are Monday to Friday, which reads as "weekday".
func isWorkingWeek(days []string) bool {
	return len(days) == 5 && strings.Join(days, ",") == strings.Join(dayNames[:5], ",")
}

// readableLength says a length as a person would: "3 hours", "1 hour 30
// minutes", "45 minutes".
func readableLength(d time.Duration) string {
	hours, minutes := int(d/time.Hour), int(d%time.Hour/time.Minute)
	var parts []string
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d hour%s", hours, plural(hours)))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%d minute%s", minutes, plural(minutes)))
	}
	return strings.Join(parts, " ")
}

// ordinal names a position: "first", "last", "6th".
func ordinal(n int) string {
	if word, ok := ordinals[n]; ok {
		return word
	}
	if n < 0 {
		return numbered(-n) + "-from-last"
	}
	return numbered(n)
}

// numbered writes a number as an ordinal: 1st, 2nd, 23rd, 11th.
func numbered(n int) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.Itoa(n) + suffix
}

func joinWithAnd(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
