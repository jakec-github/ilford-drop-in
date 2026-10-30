package shiftpattern

import (
	"strings"
	"testing"
	"time"
)

const zone = "Europe/London"

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

func formatted(starts []Start) []string {
	out := make([]string, len(starts))
	for i, s := range starts {
		out[i] = s.At.Format("Mon 2006-01-02T15:04")
	}
	return out
}

// entry writes one VEVENT block.
func vevent(dtstart, duration, rrule string) string {
	return "BEGIN:VEVENT\nDTSTART:" + dtstart + "\nDURATION:" + duration + "\nRRULE:" + rrule + "\nEND:VEVENT\n"
}

func mustCompile(t *testing.T, text string) *Pattern {
	t.Helper()
	p, err := Compile(text, zone)
	if err != nil {
		t.Fatalf("Compile(%q): %v", text, err)
	}
	return p
}

// problemsOf is every problem a reading has, the pattern's own and each
// entry's, in one string to search.
func problemsOf(r Reading) string {
	all := append([]string{}, r.Problems...)
	for _, e := range r.Entries {
		all = append(all, e.Problems...)
	}
	return strings.Join(all, "; ")
}

var sundaysAndTuesdays = vevent("20261101T193000", "PT3H", "FREQ=WEEKLY;BYDAY=SU") +
	"\n" + vevent("20261103T190000", "PT4H", "FREQ=WEEKLY;BYDAY=TU")

func TestNext_EachEntryRunsForItsOwnLength(t *testing.T) {
	p := mustCompile(t, sundaysAndTuesdays)

	starts := p.Next(time.Time{}, time.Time{}, 4)

	want := []string{"Sun 2026-11-01T19:30", "Tue 2026-11-03T19:00", "Sun 2026-11-08T19:30", "Tue 2026-11-10T19:00"}
	if got := formatted(starts); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
	for _, s := range starts {
		want := 3 * time.Hour
		if s.At.Weekday() == time.Tuesday {
			want = 4 * time.Hour
		}
		if s.Length != want {
			t.Errorf("%s runs %s, want %s", s.At.Format("Mon 2 Jan"), s.Length, want)
		}
	}
}

func TestNext_TheFirstRotaStartsFromTheEarliestEntry(t *testing.T) {
	// Written latest first: the Tuesday entry starts before the Sunday one.
	text := vevent("20261108T193000", "PT3H", "FREQ=WEEKLY;BYDAY=SU") +
		vevent("20261103T190000", "PT4H", "FREQ=WEEKLY;BYDAY=TU")
	p := mustCompile(t, text)

	if got := formatted(p.Next(time.Time{}, time.Time{}, 2)); strings.Join(got, ",") != "Tue 2026-11-03T19:00,Sun 2026-11-08T19:30" {
		t.Errorf("got %v", got)
	}
	if got := p.First().Format("2006-01-02T15:04"); got != "2026-11-03T19:00" {
		t.Errorf("First = %s, want the Tuesday", got)
	}
}

func TestNext_EveryOtherWeekCountsFromEachEntrysOwnDate(t *testing.T) {
	// Two fortnightly entries a week apart: between them every Sunday.
	text := vevent("20261101T193000", "PT3H", "FREQ=WEEKLY;INTERVAL=2;BYDAY=SU") +
		vevent("20261108T100000", "PT2H", "FREQ=WEEKLY;INTERVAL=2;BYDAY=SU")
	p := mustCompile(t, text)

	want := []string{"Sun 2026-11-01T19:30", "Sun 2026-11-08T10:00", "Sun 2026-11-15T19:30", "Sun 2026-11-22T10:00"}
	if got := formatted(p.Next(time.Time{}, time.Time{}, 4)); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNext_LastWeekdayOfTheMonth(t *testing.T) {
	p := mustCompile(t, vevent("20261130T100000", "PT2H", "FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=-1"))

	want := []string{"Mon 2026-11-30T10:00", "Thu 2026-12-31T10:00", "Fri 2027-01-29T10:00", "Fri 2027-02-26T10:00", "Wed 2027-03-31T10:00"}
	if got := formatted(p.Next(time.Time{}, time.Time{}, 5)); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNext_CarriesOnFromTheExactStartTheLastRotaStoppedAfter(t *testing.T) {
	text := vevent("20261101T100000", "PT2H", "FREQ=DAILY") + vevent("20261101T193000", "PT2H", "FREQ=DAILY")
	p := mustCompile(t, text)

	starts := p.Next(moment("2026-11-05T10:00"), time.Time{}, 3)

	want := []string{"Thu 2026-11-05T19:30", "Fri 2026-11-06T10:00", "Fri 2026-11-06T19:30"}
	if got := formatted(starts); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNext_NotBeforeLeavesAGap(t *testing.T) {
	p := mustCompile(t, vevent("20261101T193000", "PT3H", "FREQ=WEEKLY;BYDAY=SU"))

	starts := p.Next(moment("2026-11-01T19:30"), date("2026-12-01"), 2)

	want := []string{"Sun 2026-12-06T19:30", "Sun 2026-12-13T19:30"}
	if got := formatted(starts); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNext_NotBeforeEarlierThanTheStopPointChangesNothing(t *testing.T) {
	p := mustCompile(t, vevent("20261101T193000", "PT3H", "FREQ=WEEKLY;BYDAY=SU"))

	starts := p.Next(moment("2026-11-08T19:30"), date("2026-10-01"), 1)

	if got := formatted(starts); got[0] != "Sun 2026-11-15T19:30" {
		t.Errorf("got %v, want the Sunday after the stop point", got)
	}
}

func TestNext_StopsAtTheEndOfAPatternThatRunsOut(t *testing.T) {
	p := mustCompile(t, vevent("20261101T193000", "PT3H", "FREQ=WEEKLY;BYDAY=SU;COUNT=3"))

	if starts := p.Next(time.Time{}, time.Time{}, 5); len(starts) != 3 {
		t.Errorf("got %d starts, want the 3 the pattern has", len(starts))
	}
}

func TestNext_IsWallClockAcrossAClockChange(t *testing.T) {
	// The clocks go back in London on Sun 25 Oct 2026. 19:30 stays 19:30.
	p := mustCompile(t, vevent("20261018T193000", "PT3H", "FREQ=WEEKLY;BYDAY=SU"))

	for _, s := range p.Next(time.Time{}, time.Time{}, 3) {
		if s.At.Hour() != 19 || s.At.Minute() != 30 {
			t.Errorf("start %s is not at 19:30", s.At)
		}
	}
}

func TestBetween_IncludesFromAndExcludesTo(t *testing.T) {
	p := mustCompile(t, vevent("20261101T000000", "PT3H", "FREQ=WEEKLY;BYDAY=SU"))

	starts := p.Between(date("2026-11-01"), date("2026-11-15"))

	if got := formatted(starts); strings.Join(got, ",") != "Sun 2026-11-01T00:00,Sun 2026-11-08T00:00" {
		t.Errorf("got %v", got)
	}
}

func TestBetween_UntilADateIncludesThatDate(t *testing.T) {
	p := mustCompile(t, vevent("20261101T193000", "PT3H", "FREQ=DAILY;UNTIL=20261103"))

	if starts := p.Between(date("2026-11-01"), date("2026-12-01")); len(starts) != 3 {
		t.Errorf("got %v, want 1, 2 and 3 Nov", formatted(starts))
	}
}

func TestRead_DescribesEachEntryWithItsFirstDateAndLength(t *testing.T) {
	cases := []struct {
		dtstart, duration, rrule, want string
	}{
		{"20261101T193000", "PT3H", "FREQ=WEEKLY;BYDAY=SU", "Every Sunday at 19:30 for 3 hours, from 1 Nov 2026"},
		{"20261103T190000", "PT1H30M", "FREQ=WEEKLY", "Every Tuesday at 19:00 for 1 hour 30 minutes, from 3 Nov 2026"},
		{"20261104T180000", "PT45M", "FREQ=WEEKLY;INTERVAL=2;BYDAY=WE", "Every 2 weeks on Wednesday at 18:00 for 45 minutes, from 4 Nov 2026"},
		{"20261107T100000", "PT2H", "FREQ=MONTHLY;BYDAY=1SA,-1SU", "Every month on the first Saturday and the last Sunday at 10:00 for 2 hours, from 7 Nov 2026"},
		{"20261031T100000", "PT2H", "FREQ=MONTHLY", "Every month on the 31st at 10:00 for 2 hours, skipping months without one, from 31 Oct 2026"},
		{"20261130T100000", "PT2H", "FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=-1", "Every month on the last weekday at 10:00 for 2 hours, from 30 Nov 2026"},
		{"20261101T100000", "PT1H", "FREQ=DAILY;COUNT=4", "Every day at 10:00 for 1 hour, 4 times, from 1 Nov 2026"},
		{"20261231T210000", "P1D", "FREQ=YEARLY;BYMONTH=12;BYMONTHDAY=-1;UNTIL=20301231", "Every year on the last day in December at 21:00 for 24 hours, until Tue 31 Dec 2030, from 31 Dec 2026"},
	}
	for _, c := range cases {
		reading, pattern := Read(vevent(c.dtstart, c.duration, c.rrule), zone)
		if pattern == nil {
			t.Errorf("%q: refused with %s", c.rrule, problemsOf(reading))
			continue
		}
		if got := reading.Entries[0].Description; got != c.want {
			t.Errorf("%q:\n got %q\nwant %q", c.rrule, got, c.want)
		}
	}
}

func TestRead_RefusesWhatAnEntryCannotSay(t *testing.T) {
	cases := map[string]string{
		// DTSTART must be a start its own RRULE generates.
		vevent("20261102T193000", "PT3H", "FREQ=WEEKLY;BYDAY=SU"):                                                     "which its RRULE does not generate",
		vevent("20261101T193000Z", "PT3H", "FREQ=WEEKLY;BYDAY=SU"):                                                    "DTSTART ends in Z",
		"BEGIN:VEVENT\nDTSTART;TZID=America/New_York:20261101T193000\nDURATION:PT3H\nRRULE:FREQ=WEEKLY\nEND:VEVENT":   "America/New_York",
		"BEGIN:VEVENT\nDTSTART;VALUE=DATE:20261101\nDURATION:PT3H\nRRULE:FREQ=WEEKLY\nEND:VEVENT":                     "DTSTART needs a time",
		"BEGIN:VEVENT\nDTSTART:20261101T193000\nDURATION:PT3H\nRRULE:FREQ=WEEKLY\nEXDATE:20261108T193000\nEND:VEVENT": "EXDATE",
		"BEGIN:VEVENT\nDTSTART:20261101T193000\nDURATION:PT3H\nRRULE:FREQ=WEEKLY\nRDATE:20261109T193000\nEND:VEVENT":  "RDATE adds a one-off shift",
		"BEGIN:VEVENT\nDTSTART:20261101T193000\nDURATION:PT3H\nRRULE:FREQ=WEEKLY\nEXRULE:FREQ=MONTHLY\nEND:VEVENT":    "EXRULE",
		"BEGIN:VEVENT\nDTSTART:20261101T193000\nDURATION:PT3H\nEND:VEVENT":                                            "RRULE is missing",
		"BEGIN:VEVENT\nDURATION:PT3H\nRRULE:FREQ=WEEKLY\nEND:VEVENT":                                                  "DTSTART is missing",
		"BEGIN:VEVENT\nDTSTART:20261101T193000\nRRULE:FREQ=WEEKLY\nEND:VEVENT":                                        "DURATION is missing",
		"BEGIN:VEVENT\nDTSTART:20261101T193000\nDTEND:20261101T223000\nRRULE:FREQ=WEEKLY\nEND:VEVENT":                 "DTEND",
		"BEGIN:VEVENT\nDTSTART:20261101T193000\nDURATION:PT3H\nRRULE:FREQ=WEEKLY\nRRULE:FREQ=DAILY\nEND:VEVENT":       "RRULE appears twice",
		"BEGIN:VEVENT\nDTSTART:20261101T193000\nDURATION:PT3H\nRRULE:FREQ=WEEKLY\n":                                   "END:VEVENT",
		vevent("20261101T193000", "PT25H", "FREQ=WEEKLY"):                                                             "from 1 minute to 24 hours",
		vevent("20261101T193000", "-PT3H", "FREQ=WEEKLY"):                                                             "from 1 minute to 24 hours",
		vevent("20261101T193000", "P1W", "FREQ=WEEKLY"):                                                               "from 1 minute to 24 hours",
		vevent("20261101T193000", "PT30S", "FREQ=WEEKLY"):                                                             "whole minutes",
		vevent("20261101T193000", "3 hours", "FREQ=WEEKLY"):                                                           "is not a length",
		vevent("20261101T193000", "PT3H", "FREQ=HOURLY"):                                                              "FREQ=HOURLY is refused",
		vevent("20261101T193000", "PT3H", "FREQ=DAILY;COUNT=2;UNTIL=20270101"):                                        "COUNT and UNTIL cannot both be given",
		vevent("20261101T193000", "PT3H", "FREQ=YEARLY;BYWEEKNO=1"):                                                   "BYWEEKNO is not supported",
		vevent("20261101T193000", "PT3H", "FREQ=WEEKLY;BYDAY=1SU"):                                                    "a position like 1SU only means something",
		vevent("20261101T193000", "PT3H", "BYDAY=SU"):                                                                 "FREQ is missing",
		vevent("20261101T193000", "PT3H", "FREQ=MONTHLY;BYDAY=SU;BYSETPOS=0"):                                         `BYSETPOS: "0" is out of range`,
		"RRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30":                                                            "outside a BEGIN:VEVENT",
		"":                               "no entries",
		"BEGIN:VCALENDAR\nEND:VCALENDAR": "no entries",
	}
	for text, want := range cases {
		reading, pattern := Read(text, zone)
		if pattern != nil {
			t.Errorf("%q: accepted, want refused with %q", text, want)
			continue
		}
		if got := problemsOf(reading); !strings.Contains(got, want) {
			t.Errorf("%q:\n got %q\nwant it to contain %q", text, got, want)
		}
	}
}

func TestRead_ATZIDMatchingTheZoneIsAcceptedAndDropped(t *testing.T) {
	text := "BEGIN:VEVENT\nDTSTART;TZID=Europe/London:20261101T193000\nDURATION:PT3H\nRRULE:FREQ=WEEKLY\nEND:VEVENT"
	reading, pattern := Read(text, zone)
	if pattern == nil {
		t.Fatalf("refused: %s", problemsOf(reading))
	}
	if len(reading.Entries[0].Warnings) != 0 {
		t.Errorf("warned %v about a TZID that is the organisation's own", reading.Entries[0].Warnings)
	}
	if got := formatted(pattern.Next(time.Time{}, time.Time{}, 1)); got[0] != "Sun 2026-11-01T19:30" {
		t.Errorf("got %v, want the wall-clock time as written", got)
	}
}

func TestRead_RefusesTwoEntriesGeneratingTheSameStartNamingBoth(t *testing.T) {
	text := vevent("20261101T193000", "PT3H", "FREQ=WEEKLY;BYDAY=SU") + vevent("20261102T193000", "PT2H", "FREQ=DAILY")

	reading, pattern := Read(text, zone)

	if pattern != nil {
		t.Fatal("two entries making one start were accepted")
	}
	for _, e := range reading.Entries {
		if !strings.Contains(strings.Join(e.Problems, "; "), "Sun 8 Nov 2026 at 19:30") {
			t.Errorf("entry %d: problems %v, want the start they share", e.Number, e.Problems)
		}
	}
	if !strings.Contains(strings.Join(reading.Entries[0].Problems, "; "), "entry 2") ||
		!strings.Contains(strings.Join(reading.Entries[1].Problems, "; "), "entry 1") {
		t.Errorf("each entry should name the other: %v / %v", reading.Entries[0].Problems, reading.Entries[1].Problems)
	}
}

func TestRead_APastedCalendarEventReadsWithWarnings(t *testing.T) {
	// As a calendar exports it: CRLF, wrapped in VCALENDAR, with properties
	// this app has no use for, a time zone definition, and a folded line.
	text := strings.Join([]string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//Example//Calendar//EN",
		"BEGIN:VTIMEZONE",
		"TZID:Europe/London",
		"END:VTIMEZONE",
		"BEGIN:VEVENT",
		"UID:abc123@example.com",
		"DTSTAMP:20261020T120000Z",
		"SUMMARY:Sunday drop-in",
		"DTSTART;TZID=Europe/London:20261101T193000",
		"DURATION:PT3H",
		"RRULE:FREQ=WEEKLY;",
		" BYDAY=SU",
		"END:VEVENT",
		"END:VCALENDAR",
	}, "\r\n")

	reading, pattern := Read(text, zone)

	if pattern == nil {
		t.Fatalf("refused: %s", problemsOf(reading))
	}
	if len(reading.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(reading.Entries))
	}
	warnings := strings.Join(append(append([]string{}, reading.Warnings...), reading.Entries[0].Warnings...), "; ")
	for _, want := range []string{"VCALENDAR", "VTIMEZONE", "UID", "DTSTAMP", "SUMMARY"} {
		if !strings.Contains(warnings, want) {
			t.Errorf("warnings %q do not mention %s", warnings, want)
		}
	}
	if got := reading.Entries[0].Description; got != "Every Sunday at 19:30 for 3 hours, from 1 Nov 2026" {
		t.Errorf("description %q: the folded RRULE was not unfolded", got)
	}
}

func TestRead_OneBadEntryMakesNoPatternButEveryEntryIsRead(t *testing.T) {
	text := vevent("20261101T193000", "PT3H", "FREQ=WEEKLY;BYDAY=SU") + "\n" + vevent("20261101T193000", "PT3H", "FREQ=HOURLY")
	reading, pattern := Read(text, zone)

	if pattern != nil {
		t.Fatal("a pattern with a bad entry was usable")
	}
	if len(reading.Entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(reading.Entries))
	}
	if reading.Entries[0].Description == "" || len(reading.Entries[0].Problems) != 0 {
		t.Errorf("the good entry was not described: %+v", reading.Entries[0])
	}
	if second := reading.Entries[1]; second.Number != 2 || second.Line != 7 || len(second.Problems) == 0 {
		t.Errorf("the bad entry is not entry 2 on line 7 with its problems: %+v", second)
	}
}

func TestCompile_NamesEachEntryThatIsWrong(t *testing.T) {
	_, err := Compile(vevent("20261101T193000", "PT3H", "FREQ=WEEKLY;BYDAY=SU")+vevent("20261101T193000", "PT3H", "FREQ=HOURLY"), zone)

	if err == nil || !strings.HasPrefix(err.Error(), "entry 2: ") {
		t.Errorf("got %v, want the problems on entry 2", err)
	}
}
