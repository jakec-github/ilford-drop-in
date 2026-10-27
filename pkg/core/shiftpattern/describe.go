package shiftpattern

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/teambition/rrule-go"
)

// The plain-English reading of a line is what makes RRULE usable by somebody
// who does not read RRULE (ADR 0013): they write a line, or paste one, and the
// screen says back what it means. It says what the line leaves to Start from as
// well, because that is the part nobody can see in the text.

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

// describe says in plain English when a line's Shifts start, counting from
// anchor. It is only asked about lines parseLine accepted.
func describe(option rrule.ROption, anchor time.Time) string {
	unit := units[option.Freq]
	var b strings.Builder
	if option.Interval == 1 {
		b.WriteString("Every " + unit[0])
	} else {
		fmt.Fprintf(&b, "Every %d %s", option.Interval, unit[1])
	}

	var plain, positioned []string
	for _, day := range option.Byweekday {
		if day.N() == 0 {
			plain = append(plain, dayNames[day.Day()])
		} else {
			positioned = append(positioned, "the "+ordinal(day.N())+" "+dayNames[day.Day()])
		}
	}
	if len(plain) > 0 {
		b.WriteString(" on " + joinWithAnd(plain))
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

	if len(option.Bymonth) > 0 {
		var months []string
		for _, m := range option.Bymonth {
			months = append(months, time.Month(m).String())
		}
		b.WriteString(" in " + joinWithAnd(months))
	}

	// What the line leaves out, Start from fills in, and the reading says so.
	noDay := len(option.Byweekday) == 0 && len(option.Bymonthday) == 0
	switch {
	case option.Freq == rrule.WEEKLY && len(option.Byweekday) == 0:
		b.WriteString(" on " + dayNames[(int(anchor.Weekday())+6)%7] + " (the weekday it starts from)")
	case option.Freq == rrule.MONTHLY && noDay:
		b.WriteString(" on the " + numbered(anchor.Day()) + " (the day of the month it starts from)")
	case option.Freq == rrule.YEARLY && noDay && len(option.Bymonth) > 0:
		b.WriteString(" on the " + numbered(anchor.Day()) + " (the day of the month it starts from)")
	case option.Freq == rrule.YEARLY && noDay:
		fmt.Fprintf(&b, " on %d %s (the date it starts from)", anchor.Day(), anchor.Month())
	}

	// An Override's rule may name no time, and then it means the whole day.
	var times []string
	for _, h := range option.Byhour {
		for _, m := range option.Byminute {
			times = append(times, fmt.Sprintf("%02d:%02d", h, m))
		}
	}
	if len(times) > 0 {
		b.WriteString(" at " + joinWithAnd(times))
	}

	if option.Freq == rrule.MONTHLY && noDay && anchor.Day() > 28 {
		b.WriteString(", skipping months without one")
	}

	if option.Count > 0 {
		fmt.Fprintf(&b, ", %d time%s from its start", option.Count, plural(option.Count))
	}
	if !option.Until.IsZero() {
		b.WriteString(", until " + option.Until.Format("Mon 2 Jan 2006"))
	}
	if option.Interval > 1 {
		b.WriteString(", counting from " + anchor.Format("Mon 2 Jan 2006"))
	}
	return b.String()
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
