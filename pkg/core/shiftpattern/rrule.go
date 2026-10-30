package shiftpattern

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/teambition/rrule-go"
)

var (
	frequencies = map[string]rrule.Frequency{
		"DAILY":   rrule.DAILY,
		"WEEKLY":  rrule.WEEKLY,
		"MONTHLY": rrule.MONTHLY,
		"YEARLY":  rrule.YEARLY,
	}
	weekdays = map[string]rrule.Weekday{
		"MO": rrule.MO, "TU": rrule.TU, "WE": rrule.WE, "TH": rrule.TH,
		"FR": rrule.FR, "SA": rrule.SA, "SU": rrule.SU,
	}
	bydayPattern = regexp.MustCompile(`^([+-]?\d{1,2})?(MO|TU|WE|TH|FR|SA|SU)$`)
	untilPattern = regexp.MustCompile(`^(\d{8})(?:T(\d{6})Z?)?$`)
)

// parseRule reads an RRULE's value into the options rrule-go evaluates, or
// lists everything wrong with it.
//
// It is its own reader rather than rrule-go's StrToROption, for two reasons.
// That one stops at the first problem and words it for a programmer, where an
// Organiser fixing a rule wants every problem in words they can act on. And
// the ADRs narrow RRULE: some parts are refused outright.
//
// What a rule must say about time differs between a pattern entry, whose time
// is its DTSTART's, and an Override, whose BYHOUR and BYMINUTE decide what it
// matches, so each caller checks that for itself.
func parseRule(value string) (rrule.ROption, []string) {
	s := strings.ToUpper(strings.TrimSpace(value))

	option := rrule.ROption{Interval: 1, Wkst: rrule.MO}
	var problems []string
	freqSet, freqProblem := false, false
	untilSet, positional := false, false
	seen := map[string]bool{}

	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		pair := strings.Split(part, "=")
		if len(pair) != 2 || pair[0] == "" || pair[1] == "" {
			problems = append(problems, fmt.Sprintf("%q is not KEY=VALUE", part))
			continue
		}
		key, value := pair[0], pair[1]
		if seen[key] {
			problems = append(problems, key+" appears twice")
			continue
		}
		seen[key] = true

		switch key {
		case "FREQ":
			if freq, ok := frequencies[value]; ok {
				option.Freq = freq
				freqSet = true
			} else if value == "HOURLY" || value == "MINUTELY" || value == "SECONDLY" {
				problems = append(problems, fmt.Sprintf("FREQ=%s is refused - the finest is DAILY", value))
				freqProblem = true
			} else {
				problems = append(problems, fmt.Sprintf("FREQ=%s is not a frequency", value))
				freqProblem = true
			}
		case "INTERVAL":
			if n, err := strconv.Atoi(value); err == nil && n >= 1 {
				option.Interval = n
			} else {
				problems = append(problems, "INTERVAL must be a whole number of at least 1")
			}
		case "COUNT":
			if n, err := strconv.Atoi(value); err == nil && n >= 1 {
				option.Count = n
			} else {
				problems = append(problems, "COUNT must be a whole number of at least 1")
			}
		case "UNTIL":
			until, ok := parseUntil(value)
			if !ok {
				problems = append(problems, "UNTIL must look like 20270301 or 20270301T193000")
				continue
			}
			option.Until = until
			untilSet = true
		case "WKST":
			if day, ok := weekdays[value]; ok {
				option.Wkst = day
			} else {
				problems = append(problems, fmt.Sprintf("WKST=%s is not a weekday", value))
			}
		case "BYDAY":
			for _, item := range strings.Split(value, ",") {
				match := bydayPattern.FindStringSubmatch(item)
				if match == nil {
					problems = append(problems, fmt.Sprintf("BYDAY: %q is not a weekday like SU, 1SU or -1SU", item))
					continue
				}
				day := weekdays[match[2]]
				if match[1] != "" {
					n, _ := strconv.Atoi(match[1])
					if n == 0 || n > 53 || n < -53 {
						problems = append(problems, fmt.Sprintf("BYDAY: %q has an impossible position", item))
						continue
					}
					day = day.Nth(n)
					positional = true
				}
				option.Byweekday = append(option.Byweekday, day)
			}
		case "BYMONTHDAY":
			option.Bymonthday = intList(value, -31, 31, true, key, &problems)
		case "BYMONTH":
			option.Bymonth = intList(value, 1, 12, false, key, &problems)
		case "BYHOUR":
			option.Byhour = intList(value, 0, 23, false, key, &problems)
		case "BYMINUTE":
			option.Byminute = intList(value, 0, 59, false, key, &problems)
		case "BYSETPOS":
			option.Bysetpos = intList(value, -366, 366, true, key, &problems)
		case "BYWEEKNO", "BYYEARDAY", "BYSECOND":
			problems = append(problems, key+" is not supported")
		case "DTSTART":
			problems = append(problems, "DTSTART is a line of its own, not part of the RRULE")
		default:
			problems = append(problems, key+" is not an RRULE part")
		}
	}

	if !freqSet && !freqProblem {
		problems = append(problems, "FREQ is missing")
	}
	if option.Count > 0 && untilSet {
		problems = append(problems, "COUNT and UNTIL cannot both be given")
	}
	if positional && (option.Freq == rrule.DAILY || option.Freq == rrule.WEEKLY) {
		problems = append(problems, "a position like 1SU only means something with FREQ=MONTHLY or YEARLY")
	}
	return option, problems
}

// parseUntil reads UNTIL as a wall-clock moment. A bare date means the end of
// that day, so a rule running until a date includes that date's Shifts: read
// as its midnight, "until 1 March" would stop before a Shift on 1 March.
func parseUntil(value string) (time.Time, bool) {
	match := untilPattern.FindStringSubmatch(value)
	if match == nil {
		return time.Time{}, false
	}
	if match[2] == "" {
		day, err := time.Parse("20060102", match[1])
		if err != nil {
			return time.Time{}, false
		}
		return day.Add(24*time.Hour - time.Second), true
	}
	moment, err := time.Parse("20060102150405", match[1]+match[2])
	if err != nil {
		return time.Time{}, false
	}
	return moment, true
}

// intList reads a comma-separated list of whole numbers within [lo, hi],
// adding a problem for each that is not one. With nonZero, zero is refused
// too: the 0th day of a month is not a day.
func intList(value string, lo, hi int, nonZero bool, key string, problems *[]string) []int {
	var out []int
	for _, item := range strings.Split(value, ",") {
		n, err := strconv.Atoi(item)
		if err != nil || n < lo || n > hi || (nonZero && n == 0) {
			*problems = append(*problems, fmt.Sprintf("%s: %q is out of range", key, item))
			continue
		}
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}
