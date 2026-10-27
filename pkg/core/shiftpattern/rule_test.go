package shiftpattern

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func mustReadRule(t *testing.T, text, startFrom string) *Rule {
	t.Helper()
	line, rule := ReadRule(text, date(startFrom))
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
		{"FREQ=MONTHLY;BYDAY=1SU", "Every month on the first Sunday"},
		{"RRULE:FREQ=WEEKLY;BYDAY=SA;BYHOUR=10;BYMINUTE=0", "Every week on Saturday at 10:00"},
	}
	for _, c := range cases {
		line, rule := ReadRule(c.rule, date("2026-11-01"))
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
		"FREQ=WEEKLY;BYDAY=SU;BYHOUR=19":   "give BYHOUR and BYMINUTE together",
		"FREQ=WEEKLY;BYDAY=SU;BYMINUTE=30": "give BYHOUR and BYMINUTE together",
		"FREQ=MONTHLY;BYSETPOS=1":          "BYSETPOS is not supported",
		"BYDAY=SU":                         "FREQ is missing",
		"":                                 "the rule is empty",
		"FREQ=DAILY\nFREQ=WEEKLY":          "one rule, on one line",
	}
	for text, want := range cases {
		line, rule := ReadRule(text, date("2026-11-01"))
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
	rule := mustReadRule(t, "FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30", "2026-11-01")
	if !rule.Exact() {
		t.Fatal("a rule naming a time should be exact")
	}
	starts := moments("2026-11-01T10:00", "2026-11-01T19:30", "2026-11-02T19:30")
	if got, want := rule.Picks(starts), []bool{false, true, false}; !slices.Equal(got, want) {
		t.Errorf("Picks = %v, want %v", got, want)
	}
}

func TestRule_DateRulePicksEveryShiftOnItsDates(t *testing.T) {
	rule := mustReadRule(t, "FREQ=WEEKLY;BYDAY=SU", "2026-11-01")
	if rule.Exact() {
		t.Fatal("a rule naming no time should match whole dates")
	}
	starts := moments("2026-11-01T10:00", "2026-11-01T19:30", "2026-11-02T19:30")
	if got, want := rule.Picks(starts), []bool{true, true, false}; !slices.Equal(got, want) {
		t.Errorf("Picks = %v, want %v", got, want)
	}
}

func TestRule_FirstSundayOfTheMonth(t *testing.T) {
	rule := mustReadRule(t, "FREQ=MONTHLY;BYDAY=1SU", "2026-11-01")
	starts := moments(
		"2026-11-01T19:30", "2026-11-08T19:30", "2026-11-29T19:30",
		"2026-12-06T19:30", "2026-12-13T19:30",
	)
	if got, want := rule.Picks(starts), []bool{true, false, false, true, false}; !slices.Equal(got, want) {
		t.Errorf("Picks = %v, want %v", got, want)
	}
}

func TestRule_NothingBeforeItsStartFrom(t *testing.T) {
	rule := mustReadRule(t, "FREQ=WEEKLY;BYDAY=SU", "2026-11-08")
	starts := moments("2026-11-01T19:30", "2026-11-08T19:30")
	if got, want := rule.Picks(starts), []bool{false, true}; !slices.Equal(got, want) {
		t.Errorf("Picks = %v, want %v", got, want)
	}
}

func TestRule_OnDateFindsTheDayAnExactRuleMissedByTime(t *testing.T) {
	rule := mustReadRule(t, "FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=0", "2026-11-01")
	starts := moments("2026-11-01T19:30", "2026-11-02T19:30")
	if got, want := rule.Picks(starts), []bool{false, false}; !slices.Equal(got, want) {
		t.Errorf("Picks = %v, want %v", got, want)
	}
	if got, want := rule.OnDate(starts), []bool{true, false}; !slices.Equal(got, want) {
		t.Errorf("OnDate = %v, want %v", got, want)
	}
}
