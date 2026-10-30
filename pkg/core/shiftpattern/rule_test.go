package shiftpattern

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// rule writes an Override's rule: the date it counts from and its RRULE.
func rule(from, rrule string) string {
	return "DTSTART;VALUE=DATE:" + from + "\nRRULE:" + rrule
}

func mustReadRule(t *testing.T, text string) *Rule {
	t.Helper()
	line, rule := ReadRule(text)
	if rule == nil {
		t.Fatalf("ReadRule(%q): %v", text, line.Problems)
	}
	return rule
}

func moments(ss ...string) []time.Time {
	out := make([]time.Time, len(ss))
	for i, s := range ss {
		out[i] = moment(s)
	}
	return out
}

func TestReadRule_DescribesARuleWithAndWithoutATime(t *testing.T) {
	cases := []struct{ rule, want string }{
		{rule("20261101", "FREQ=MONTHLY;BYDAY=1SU"), "Every month on the first Sunday, from 1 Nov 2026"},
		{rule("20261101", "FREQ=WEEKLY;BYDAY=SA;BYHOUR=10;BYMINUTE=0"), "Every Saturday at 10:00, from 1 Nov 2026"},
		{rule("20261101", "FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=-1"), "Every month on the last weekday, from 1 Nov 2026"},
		// The order of the two lines is not what decides, and the RRULE:
		// prefix is optional, as it always was.
		{"FREQ=WEEKLY;BYDAY=SU\nDTSTART;VALUE=DATE:20261101", "Every Sunday, from 1 Nov 2026"},
		{"DTSTART:20261101\nRRULE:FREQ=WEEKLY;BYDAY=SU", "Every Sunday, from 1 Nov 2026"},
		// What migration 038 makes of a rule stored before ADR 0014.
		{"DTSTART;VALUE=DATE:20261101\nFREQ=MONTHLY;BYDAY=1SU", "Every month on the first Sunday, from 1 Nov 2026"},
	}
	for _, c := range cases {
		line, rule := ReadRule(c.rule)
		if rule == nil {
			t.Errorf("%q: refused with %v", c.rule, line.Problems)
			continue
		}
		if line.Description != c.want {
			t.Errorf("%q:\n got %q\nwant %q", c.rule, line.Description, c.want)
		}
	}
}

func TestReadRule_RefusesHalfATimeAndWhatThePatternRefuses(t *testing.T) {
	cases := map[string]string{
		rule("20261101", "FREQ=WEEKLY;BYDAY=SU;BYHOUR=19"):                 "give BYHOUR and BYMINUTE together",
		rule("20261101", "FREQ=WEEKLY;BYDAY=SU;BYMINUTE=30"):               "give BYHOUR and BYMINUTE together",
		rule("20261101", "FREQ=YEARLY;BYYEARDAY=1"):                        "BYYEARDAY is not supported",
		rule("20261101", "BYDAY=SU"):                                       "FREQ is missing",
		"":                                                                 "the rule is empty",
		"RRULE:FREQ=WEEKLY;BYDAY=SU":                                       "DTSTART is missing",
		"DTSTART;VALUE=DATE:20261101":                                      "RRULE is missing",
		"DTSTART:20261101T193000\nRRULE:FREQ=WEEKLY":                       "an Override's DTSTART is a date",
		"DTSTART;VALUE=DATE:2026110\nRRULE:FREQ=WEEKLY":                    "is not a date",
		rule("20261101", "FREQ=DAILY") + "\nRRULE:FREQ=WEEKLY":             "RRULE appears twice",
		rule("20261101", "FREQ=DAILY") + "\nEXDATE:20261108":               "EXDATE",
		rule("20261101", "FREQ=DAILY") + "\nRDATE:20261108":                "RDATE",
		"BEGIN:VEVENT\n" + rule("20261101", "FREQ=DAILY") + "\nEND:VEVENT": "not a VEVENT",
	}
	for text, want := range cases {
		line, rule := ReadRule(text)
		if rule != nil {
			t.Errorf("%q: accepted, want refused with %q", text, want)
			continue
		}
		if !strings.Contains(strings.Join(line.Problems, "; "), want) {
			t.Errorf("%q: problems %v, want one containing %q", text, line.Problems, want)
		}
	}
}

func TestRule_ExactRulePicksOnlyTheShiftAtItsTime(t *testing.T) {
	rule := mustReadRule(t, rule("20261101", "FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30"))
	if !rule.Exact() {
		t.Fatal("a rule naming a time should be exact")
	}
	starts := moments("2026-11-01T10:00", "2026-11-01T19:30", "2026-11-02T19:30")
	if got, want := rule.Picks(starts), []bool{false, true, false}; !slices.Equal(got, want) {
		t.Errorf("Picks = %v, want %v", got, want)
	}
}

func TestRule_DateRulePicksEveryShiftOnItsDates(t *testing.T) {
	rule := mustReadRule(t, rule("20261101", "FREQ=WEEKLY;BYDAY=SU"))
	if rule.Exact() {
		t.Fatal("a rule naming no time should match whole dates")
	}
	starts := moments("2026-11-01T10:00", "2026-11-01T19:30", "2026-11-02T19:30")
	if got, want := rule.Picks(starts), []bool{true, true, false}; !slices.Equal(got, want) {
		t.Errorf("Picks = %v, want %v", got, want)
	}
}

func TestRule_FirstSundayOfTheMonth(t *testing.T) {
	rule := mustReadRule(t, rule("20261101", "FREQ=MONTHLY;BYDAY=1SU"))
	starts := moments(
		"2026-11-01T19:30", "2026-11-08T19:30", "2026-11-29T19:30",
		"2026-12-06T19:30", "2026-12-13T19:30",
	)
	if got, want := rule.Picks(starts), []bool{true, false, false, true, false}; !slices.Equal(got, want) {
		t.Errorf("Picks = %v, want %v", got, want)
	}
}

func TestRule_LastWeekdayOfTheMonth(t *testing.T) {
	rule := mustReadRule(t, rule("20261101", "FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=-1"))
	starts := moments("2026-11-27T10:00", "2026-11-30T10:00", "2026-12-31T10:00")
	if got, want := rule.Picks(starts), []bool{false, true, true}; !slices.Equal(got, want) {
		t.Errorf("Picks = %v, want %v", got, want)
	}
}

func TestRule_NothingBeforeItsDTSTART(t *testing.T) {
	rule := mustReadRule(t, rule("20261108", "FREQ=WEEKLY;BYDAY=SU"))
	starts := moments("2026-11-01T19:30", "2026-11-08T19:30")
	if got, want := rule.Picks(starts), []bool{false, true}; !slices.Equal(got, want) {
		t.Errorf("Picks = %v, want %v", got, want)
	}
}

func TestRule_OnDateFindsTheDayAnExactRuleMissedByTime(t *testing.T) {
	rule := mustReadRule(t, rule("20261101", "FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=0"))
	starts := moments("2026-11-01T19:30", "2026-11-02T19:30")
	if got, want := rule.Picks(starts), []bool{false, false}; !slices.Equal(got, want) {
		t.Errorf("Picks = %v, want %v", got, want)
	}
	if got, want := rule.OnDate(starts), []bool{true, false}; !slices.Equal(got, want) {
		t.Errorf("OnDate = %v, want %v", got, want)
	}
}

// A rule migration 038 rewrote — its Start from folded in as DTSTART, its
// RRULE as it was stored, with or without the prefix — matches what it
// matched before: here every other Sunday counted from 8 November.
func TestRule_AMigratedRuleMatchesWhatItDidBefore(t *testing.T) {
	for _, text := range []string{
		"DTSTART;VALUE=DATE:20261108\nRRULE:FREQ=WEEKLY;INTERVAL=2;BYDAY=SU;BYHOUR=19;BYMINUTE=30",
		"DTSTART;VALUE=DATE:20261108\nFREQ=WEEKLY;INTERVAL=2;BYDAY=SU;BYHOUR=19;BYMINUTE=30",
	} {
		rule := mustReadRule(t, text)
		starts := moments("2026-11-01T19:30", "2026-11-08T19:30", "2026-11-15T19:30", "2026-11-22T19:30")
		if got, want := rule.Picks(starts), []bool{false, true, false, true}; !slices.Equal(got, want) {
			t.Errorf("%q: Picks = %v, want %v", text, got, want)
		}
	}
}
