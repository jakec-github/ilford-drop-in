package shiftpattern

import (
	"strings"
	"testing"
	"time"
)

func date(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func moment(s string) time.Time {
	t, err := time.Parse("2006-01-02T15:04", s)
	if err != nil {
		panic(err)
	}
	return t
}

func formatted(starts []time.Time) []string {
	out := make([]string, len(starts))
	for i, s := range starts {
		out[i] = s.Format("Mon 2006-01-02T15:04")
	}
	return out
}

func mustCompile(t *testing.T, text, startFrom string) *Pattern {
	t.Helper()
	p, err := Compile(text, date(startFrom))
	if err != nil {
		t.Fatalf("Compile(%q): %v", text, err)
	}
	return p
}

func TestNext_WeeklyGivesTheCountAskedFor(t *testing.T) {
	p := mustCompile(t, "RRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30", "2026-10-27")

	starts := p.Next(time.Time{}, time.Time{}, 12)

	if len(starts) != 12 {
		t.Fatalf("got %d starts, want 12", len(starts))
	}
	if got := starts[0].Format("2006-01-02T15:04"); got != "2026-11-01T19:30" {
		t.Errorf("first start = %s, want the Sunday after Start from at 19:30", got)
	}
	if got := starts[11].Format("2006-01-02T15:04"); got != "2027-01-17T19:30" {
		t.Errorf("last start = %s, want eleven weeks later", got)
	}
}

func TestNext_CarriesOnFromTheExactStartTheLastRotaStoppedAfter(t *testing.T) {
	p := mustCompile(t, "RRULE:FREQ=DAILY;BYHOUR=10;BYMINUTE=0\nRRULE:FREQ=DAILY;BYHOUR=19;BYMINUTE=30", "2026-11-01")

	starts := p.Next(moment("2026-11-05T10:00"), time.Time{}, 3)

	want := []string{"Thu 2026-11-05T19:30", "Fri 2026-11-06T10:00", "Fri 2026-11-06T19:30"}
	if got := formatted(starts); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNext_NotBeforeLeavesAGap(t *testing.T) {
	p := mustCompile(t, "RRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30", "2026-11-01")

	starts := p.Next(moment("2026-11-01T19:30"), date("2026-12-01"), 2)

	want := []string{"Sun 2026-12-06T19:30", "Sun 2026-12-13T19:30"}
	if got := formatted(starts); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNext_NotBeforeEarlierThanTheStopPointChangesNothing(t *testing.T) {
	p := mustCompile(t, "RRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30", "2026-11-01")

	starts := p.Next(moment("2026-11-08T19:30"), date("2026-10-01"), 1)

	if got := formatted(starts); got[0] != "Sun 2026-11-15T19:30" {
		t.Errorf("got %v, want the Sunday after the stop point", got)
	}
}

func TestNext_EveryOtherWeekCountsFromStartFrom(t *testing.T) {
	text := "RRULE:FREQ=WEEKLY;INTERVAL=2;BYDAY=SU;BYHOUR=19;BYMINUTE=30"

	// The week of Mon 2 Nov holds Sun 8 Nov; the week of Mon 9 Nov holds
	// Sun 15 Nov. Which of the two a fortnight starts on is Start from's
	// week, wherever the search begins.
	fromFirst := mustCompile(t, text, "2026-11-02").Next(time.Time{}, date("2026-11-20"), 2)
	fromSecond := mustCompile(t, text, "2026-11-09").Next(time.Time{}, date("2026-11-20"), 2)

	if got := formatted(fromFirst); strings.Join(got, ",") != "Sun 2026-11-22T19:30,Sun 2026-12-06T19:30" {
		t.Errorf("from 2 Nov: got %v", got)
	}
	if got := formatted(fromSecond); strings.Join(got, ",") != "Sun 2026-11-29T19:30,Sun 2026-12-13T19:30" {
		t.Errorf("from 9 Nov: got %v", got)
	}
}

func TestNext_StopsAtTheEndOfAPatternThatRunsOut(t *testing.T) {
	p := mustCompile(t, "RRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30;COUNT=3", "2026-11-01")

	if starts := p.Next(time.Time{}, time.Time{}, 5); len(starts) != 3 {
		t.Errorf("got %d starts, want the 3 the pattern has", len(starts))
	}
}

func TestNext_IsWallClockAcrossAClockChange(t *testing.T) {
	// The clocks go back in London on Sun 25 Oct 2026. 19:30 stays 19:30.
	p := mustCompile(t, "RRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30", "2026-10-18")

	for _, s := range p.Next(time.Time{}, time.Time{}, 3) {
		if s.Hour() != 19 || s.Minute() != 30 {
			t.Errorf("start %s is not at 19:30", s)
		}
	}
}

func TestBetween_IncludesFromAndExcludesTo(t *testing.T) {
	p := mustCompile(t, "RRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=0;BYMINUTE=0", "2026-11-01")

	starts := p.Between(date("2026-11-01"), date("2026-11-15"))

	if got := formatted(starts); strings.Join(got, ",") != "Sun 2026-11-01T00:00,Sun 2026-11-08T00:00" {
		t.Errorf("got %v", got)
	}
}

func TestBetween_OneShiftWhenTwoLinesGenerateTheSameStart(t *testing.T) {
	p := mustCompile(t, "FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30\nFREQ=DAILY;BYHOUR=19;BYMINUTE=30", "2026-11-01")

	if starts := p.Between(date("2026-11-01"), date("2026-11-08")); len(starts) != 7 {
		t.Errorf("got %d starts, want 7", len(starts))
	}
}

func TestBetween_UntilADateIncludesThatDate(t *testing.T) {
	p := mustCompile(t, "FREQ=DAILY;BYHOUR=19;BYMINUTE=30;UNTIL=20261103", "2026-11-01")

	if starts := p.Between(date("2026-11-01"), date("2026-12-01")); len(starts) != 3 {
		t.Errorf("got %v, want 1, 2 and 3 Nov", formatted(starts))
	}
}

func TestRead_DescribesEachLine(t *testing.T) {
	cases := []struct {
		line, startFrom, want string
	}{
		{"RRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30", "2026-11-01", "Every week on Sunday at 19:30"},
		{"FREQ=WEEKLY;BYHOUR=19;BYMINUTE=30", "2026-10-27", "Every week on Tuesday (the weekday it starts from) at 19:30"},
		{"FREQ=WEEKLY;INTERVAL=2;BYDAY=WE;BYHOUR=18;BYMINUTE=0", "2026-11-01", "Every 2 weeks on Wednesday at 18:00, counting from Sun 1 Nov 2026"},
		{"FREQ=MONTHLY;BYDAY=1SA,-1SU;BYHOUR=10;BYMINUTE=0", "2026-11-01", "Every month on the first Saturday and the last Sunday at 10:00"},
		{"FREQ=MONTHLY;BYHOUR=10;BYMINUTE=0", "2026-10-31", "Every month on the 31st (the day of the month it starts from) at 10:00, skipping months without one"},
		{"FREQ=DAILY;BYHOUR=10,19;BYMINUTE=0,30;COUNT=4", "2026-11-01", "Every day at 10:00, 10:30, 19:00 and 19:30, 4 times from its start"},
		{"FREQ=YEARLY;BYMONTH=12;BYMONTHDAY=-1;BYHOUR=21;BYMINUTE=0;UNTIL=20301231", "2026-11-01", "Every year on the last day in December at 21:00, until Tue 31 Dec 2030"},
	}
	for _, c := range cases {
		lines, pattern := Read(c.line, date(c.startFrom))
		if pattern == nil {
			t.Errorf("%q: refused with %v", c.line, lines[0].Problems)
			continue
		}
		if lines[0].Description != c.want {
			t.Errorf("%q:\n got %q\nwant %q", c.line, lines[0].Description, c.want)
		}
	}
}

func TestRead_RefusesWhatTheADRRefuses(t *testing.T) {
	cases := map[string]string{
		"DTSTART:20261101T000000":                                 "DTSTART is not written",
		"RDATE:20261101T193000":                                   "RDATE adds a one-off shift",
		"EXDATE:20261101T193000":                                  "EXDATE and EXRULE are not supported",
		"FREQ=HOURLY;BYHOUR=19;BYMINUTE=30":                       "FREQ=HOURLY is refused",
		"FREQ=WEEKLY;BYDAY=SU":                                    "every line needs BYHOUR and BYMINUTE",
		"FREQ=WEEKLY;BYHOUR=19":                                   "every line needs BYHOUR and BYMINUTE",
		"FREQ=DAILY;BYHOUR=19;BYMINUTE=30;COUNT=2;UNTIL=20270101": "COUNT and UNTIL cannot both be given",
		"FREQ=MONTHLY;BYSETPOS=1;BYHOUR=19;BYMINUTE=30":           "BYSETPOS is not supported",
		"FREQ=YEARLY;BYWEEKNO=1;BYHOUR=19;BYMINUTE=30":            "BYWEEKNO is not supported",
		"FREQ=YEARLY;BYYEARDAY=1;BYHOUR=19;BYMINUTE=30":           "BYYEARDAY is not supported",
		"FREQ=DAILY;BYSECOND=1;BYHOUR=19;BYMINUTE=30":             "BYSECOND is not supported",
		"FREQ=DAILY;DTSTART=20261101;BYHOUR=19;BYMINUTE=30":       "DTSTART is not written",
		"FREQ=WEEKLY;BYDAY=1SU;BYHOUR=19;BYMINUTE=30":             "a position like 1SU only means something",
		"BYHOUR=19;BYMINUTE=30":                                   "FREQ is missing",
		"FREQ=DAILY;BYHOUR=24;BYMINUTE=30":                        `BYHOUR: "24" is out of range`,
		"FREQ=DAILY;FREQ=WEEKLY;BYHOUR=19;BYMINUTE=30":            "FREQ appears twice",
		"FREQ=DAILY;BYHOUR19;BYMINUTE=30":                         `"BYHOUR19" is not KEY=VALUE`,
	}
	for line, want := range cases {
		lines, pattern := Read(line, date("2026-11-01"))
		if pattern != nil {
			t.Errorf("%q: accepted, want refused with %q", line, want)
			continue
		}
		if got := strings.Join(lines[0].Problems, "; "); !strings.Contains(got, want) {
			t.Errorf("%q:\n got %q\nwant it to contain %q", line, got, want)
		}
	}
}

func TestRead_OneBadLineMakesNoPatternButEveryLineIsRead(t *testing.T) {
	lines, pattern := Read("FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30\n\nFREQ=WEEKLY", date("2026-11-01"))

	if pattern != nil {
		t.Fatal("a pattern with a bad line was usable")
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want the two that are not blank", len(lines))
	}
	if lines[0].Description == "" || len(lines[0].Problems) != 0 {
		t.Errorf("the good line was not described: %+v", lines[0])
	}
	if lines[1].Number != 3 || len(lines[1].Problems) == 0 {
		t.Errorf("the bad line is not line 3 with its problems: %+v", lines[1])
	}
}

func TestCompile_NamesEachLineThatIsWrong(t *testing.T) {
	_, err := Compile("FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30\nFREQ=WEEKLY", date("2026-11-01"))

	if err == nil || !strings.HasPrefix(err.Error(), "line 2: ") {
		t.Errorf("got %v, want the problems on line 2", err)
	}
}

func TestCompile_RefusesAnEmptyPattern(t *testing.T) {
	if _, err := Compile(" \n ", date("2026-11-01")); err == nil {
		t.Error("an empty pattern compiled")
	}
}
