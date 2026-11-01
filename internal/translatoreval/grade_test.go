package translatoreval

import (
	"strings"
	"testing"

	"github.com/jakechorley/ilford-drop-in/pkg/core/patterntranslator"
)

func patternCase(want string) Case {
	return Case{Name: "c", Kind: Patterns, Zone: "Europe/London", Today: date(2026, 11, 1), Expect: Expect{Pattern: want}}
}

func ruleCase(want string) Case {
	return Case{Name: "c", Kind: Rules, Zone: "Europe/London", Today: date(2026, 11, 1), Pattern: twoSundays, Expect: Expect{Rule: want}}
}

func issuesCase(kind string, want ...ExpectedIssue) Case {
	c := Case{Name: "c", Kind: kind, Zone: "Europe/London", Today: date(2026, 11, 1), Expect: Expect{Issues: want}}
	if kind == Rules {
		c.Pattern = twoSundays
	}
	return c
}

func TestGrade_APatternWrittenAnotherWayButGivingTheSameShiftsPasses(t *testing.T) {
	// DAILY with BYDAY, from a Sunday a month before today: the same Shifts
	// from today on.
	same := "BEGIN:VEVENT\nDTSTART:20261004T193000\nDURATION:PT180M\nRRULE:FREQ=DAILY;BYDAY=SU\nEND:VEVENT"
	g := grade(patternCase(sundays), Answer{Outcome: Translated, Answer: same})
	if !g.Pass {
		t.Errorf("failed: %v", g.Failures)
	}
}

func TestGrade_APatternGivingOtherShiftsFailsAndSaysWhich(t *testing.T) {
	later := "BEGIN:VEVENT\nDTSTART:20261101T200000\nDURATION:PT3H\nRRULE:FREQ=WEEKLY;BYDAY=SU\nEND:VEVENT"
	g := grade(patternCase(sundays), Answer{Outcome: Translated, Answer: later})
	if g.Pass {
		t.Fatal("passed")
	}
	if g.Shifts == nil || g.Shifts.MissingCount != 53 || g.Shifts.ExtraCount != 53 {
		t.Fatalf("shifts: %+v", g.Shifts)
	}
	if g.Shifts.Missing[0] != "Sun 1 Nov 2026 19:30 for 3h" || g.Shifts.Extra[0] != "Sun 1 Nov 2026 20:00 for 3h" {
		t.Errorf("first missing and extra: %q, %q", g.Shifts.Missing[0], g.Shifts.Extra[0])
	}
	if len(g.Shifts.Missing) != shownShifts {
		t.Errorf("listed %d missing, want the first %d", len(g.Shifts.Missing), shownShifts)
	}
}

func TestGrade_AShiftOfAnotherLengthIsADifferentShift(t *testing.T) {
	longer := strings.Replace(sundays, "PT3H", "PT4H", 1)
	if g := grade(patternCase(sundays), Answer{Outcome: Translated, Answer: longer}); g.Pass {
		t.Error("passed")
	}
}

func TestGrade_ARuleIsGradedByTheShiftsItPicksOut(t *testing.T) {
	evening := "DTSTART;VALUE=DATE:20261101\nRRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30"

	same := "DTSTART;VALUE=DATE:20260906\nRRULE:FREQ=DAILY;BYDAY=SU;BYMINUTE=30;BYHOUR=19"
	if g := grade(ruleCase(evening), Answer{Outcome: Translated, Answer: same}); !g.Pass {
		t.Errorf("the same Shifts failed: %v", g.Failures)
	}

	// The whole date picks out the 10:00 Shift as well.
	wholeDate := "DTSTART;VALUE=DATE:20261101\nRRULE:FREQ=WEEKLY;BYDAY=SU"
	g := grade(ruleCase(evening), Answer{Outcome: Translated, Answer: wholeDate})
	if g.Pass {
		t.Fatal("the whole date passed")
	}
	if g.Shifts.MissingCount != 0 || g.Shifts.ExtraCount != 53 || g.Shifts.Extra[0] != "Sun 1 Nov 2026 10:00 for 2h" {
		t.Errorf("shifts: %+v", g.Shifts)
	}
}

func TestGrade_ATranslationWhereIssuesWereWantedFails(t *testing.T) {
	g := grade(issuesCase(Patterns, ExpectedIssue{Kind: patterntranslator.Missing, About: "length"}),
		Answer{Outcome: Translated, Answer: sundays})
	if g.Pass || !strings.Contains(strings.Join(g.Failures, "\n"), "translated, but issues were wanted") {
		t.Errorf("failures: %v", g.Failures)
	}
}

func TestGrade_IssuesWhereATranslationWasWantedFail(t *testing.T) {
	g := grade(patternCase(sundays), Answer{Outcome: Raised, Issues: []patterntranslator.Issue{
		{Kind: patterntranslator.Missing, Message: "Add how long the shifts run"},
	}})
	if g.Pass || !strings.Contains(strings.Join(g.Failures, "\n"), "raised issues, but a translation was wanted") {
		t.Errorf("failures: %v", g.Failures)
	}
}

func TestGrade_IssuesPassWhenTheirKindsAndMessagesAreRight(t *testing.T) {
	c := issuesCase(Patterns,
		ExpectedIssue{Kind: patterntranslator.Missing, About: "length"},
		ExpectedIssue{Kind: patterntranslator.Ambiguous, About: "evenings"})
	g := grade(c, Answer{Outcome: Raised, Issues: []patterntranslator.Issue{
		{Kind: patterntranslator.Ambiguous, Message: `"Evenings" — at what time?`},
		{Kind: patterntranslator.Missing, Message: "Add how long the Sunday shifts run"},
	}})
	if !g.Pass {
		t.Errorf("failed: %v", g.Failures)
	}
}

func TestGrade_IssuesOfOtherKindsFail(t *testing.T) {
	c := issuesCase(Patterns,
		ExpectedIssue{Kind: patterntranslator.Missing, About: "length"},
		ExpectedIssue{Kind: patterntranslator.Ambiguous, About: "evenings"})
	g := grade(c, Answer{Outcome: Raised, Issues: []patterntranslator.Issue{
		{Kind: patterntranslator.Missing, Message: "Add how long the Sunday shifts run"},
		{Kind: patterntranslator.Unsupported, Message: "Bank holidays can't be left out"},
	}})
	want := "kinds: raised missing, unsupported; wanted ambiguous, missing"
	if g.Pass || !contains(g.Failures, want) {
		t.Errorf("failures: %q, want %q", g.Failures, want)
	}
}

func TestGrade_AMessageMustBeShortAndFreeOfTheFormat(t *testing.T) {
	c := issuesCase(Rules, ExpectedIssue{Kind: patterntranslator.Ambiguous, About: "which shift"})
	g := grade(c, Answer{Outcome: Raised, Issues: []patterntranslator.Issue{
		{Kind: patterntranslator.Ambiguous, Message: "Say which of the two Sunday shifts you mean, the morning or the evening"},
		{Kind: patterntranslator.Ambiguous, Message: "Add BYHOUR to pick the shift"},
		{Kind: patterntranslator.Ambiguous, Message: "Say which Sunday shift, until when"},
	}})
	for _, want := range []string{
		`"Say which of the two Sunday shifts you mean, the morning or the evening": 14 words, want under 12`,
		`"Add BYHOUR to pick the shift": names BYHOUR`,
	} {
		if !contains(g.Failures, want) {
			t.Errorf("failures: %q, want %q", g.Failures, want)
		}
	}
	if len(g.Failures) != 2 {
		t.Errorf("failures: %q, want two (an ordinary \"until\" is not the format)", g.Failures)
	}
}

func TestGrade_GivingUpOrNotAnsweringFails(t *testing.T) {
	gaveUp := grade(patternCase(sundays), Answer{Outcome: GaveUp, Issues: []patterntranslator.Issue{
		{Kind: patterntranslator.Unsupported, Message: patterntranslator.GaveUp},
		{Kind: patterntranslator.Unsupported, Message: "entry 1: DURATION is missing"},
	}})
	if gaveUp.Pass || !contains(gaveUp.Failures, "gave up: entry 1: DURATION is missing") {
		t.Errorf("gave up: %q", gaveUp.Failures)
	}
	down := grade(patternCase(sundays), Answer{Outcome: Unavailable, Error: "couldn't translate just now: 529"})
	if down.Pass || !contains(down.Failures, "unavailable: couldn't translate just now: 529") {
		t.Errorf("unavailable: %q", down.Failures)
	}
}

func contains(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}
