package translatoreval

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/core/patterntranslator"
	"github.com/jakechorley/ilford-drop-in/pkg/core/shiftpattern"
)

// Outcome is how one translation ended.
type Outcome string

const (
	// Translated is a pattern or rule the parser accepted.
	Translated Outcome = "translated"
	// Raised is issues the model raised.
	Raised Outcome = "issues"
	// GaveUp is every call's answer refused by the parser.
	GaveUp Outcome = "gave up"
	// Unavailable is a translation that could not be made: the model could
	// not be reached, or the time ran out.
	Unavailable Outcome = "unavailable"
)

// Answer is what one translation came back with.
type Answer struct {
	Outcome Outcome                   `json:"outcome"`
	Answer  string                    `json:"answer,omitempty"`
	Issues  []patterntranslator.Issue `json:"issues,omitempty"`
	Error   string                    `json:"error,omitempty"`
}

// Grade is whether an answer is the one its case wants, and if not, why not.
type Grade struct {
	Pass     bool       `json:"pass"`
	Failures []string   `json:"failures,omitempty"`
	Shifts   *ShiftDiff `json:"shifts,omitempty"`
}

// ShiftDiff is how a translation's Shifts differ from the ones wanted: the
// first few each way, and how many in all.
type ShiftDiff struct {
	Missing      []string `json:"missing,omitempty"`
	MissingCount int      `json:"missingCount"`
	Extra        []string `json:"extra,omitempty"`
	ExtraCount   int      `json:"extraCount"`
}

// shownShifts is how many Shifts each way a ShiftDiff lists.
const shownShifts = 5

// window is how far from a case's today its Shifts are compared: a year, so
// every monthly and yearly rule has come round.
func window(today time.Time) (time.Time, time.Time) {
	from := shiftpattern.Day(today)
	return from, from.AddDate(1, 0, 0)
}

// maxWords is how long an issue's message may be: "under 12 words", as both
// prompts say.
const maxWords = 11

// formatTerms are the format's own words, which no message may use. They
// are matched as written — an ordinary "until" is not UNTIL.
var formatTerms = regexp.MustCompile(`\b(RRULE|DTSTART|DURATION|BYSETPOS|BYDAY|BYHOUR|BYMINUTE|BYMONTHDAY|BYMONTH|BYYEARDAY|BYWEEKNO|BYSECOND|EXDATE|RDATE|EXRULE|VEVENT|FREQ|INTERVAL|COUNT|UNTIL|WKST|VALUE=DATE|iCalendar)\b`)

func grade(c Case, a Answer) Grade {
	var g Grade
	switch a.Outcome {
	case Unavailable:
		g.Failures = []string{"unavailable: " + a.Error}
	case GaveUp:
		var messages []string
		for _, issue := range a.Issues[min(1, len(a.Issues)):] {
			messages = append(messages, issue.Message)
		}
		g.Failures = []string{"gave up: " + strings.Join(messages, "; ")}
	case Translated:
		if !c.Translates() {
			g.Failures = []string{"translated, but issues were wanted"}
			break
		}
		g.Shifts = diffShifts(c, a.Answer)
		if g.Shifts.MissingCount+g.Shifts.ExtraCount > 0 {
			g.Failures = []string{fmt.Sprintf("other Shifts: %d missing, %d extra", g.Shifts.MissingCount, g.Shifts.ExtraCount)}
		}
	case Raised:
		if c.Translates() {
			g.Failures = []string{"raised issues, but a translation was wanted"}
			break
		}
		g.Failures = checkIssues(c.Expect.Issues, a.Issues)
	}
	g.Pass = len(g.Failures) == 0
	return g
}

// diffShifts compares the Shifts a translation gives over the case's window
// with the ones its expected answer gives. A pattern's Shifts are when it
// starts them and for how long; a rule's are which of the case's pattern's
// Shifts it picks out.
func diffShifts(c Case, answer string) *ShiftDiff {
	from, to := window(c.Today)
	var want, got []shiftpattern.Start
	if c.Kind == Rules {
		_, pattern := shiftpattern.Read(c.Pattern, c.Zone)
		starts := pattern.Between(from, to)
		want = picked(c.Expect.Rule, starts)
		got = picked(answer, starts)
	} else {
		want = between(c.Expect.Pattern, c.Zone, from, to)
		got = between(answer, c.Zone, from, to)
	}

	key := func(s shiftpattern.Start) string { return formatShift(s) }
	wanted := map[string]bool{}
	for _, s := range want {
		wanted[key(s)] = true
	}
	given := map[string]bool{}
	for _, s := range got {
		given[key(s)] = true
	}
	d := &ShiftDiff{}
	for _, s := range want {
		if !given[key(s)] {
			d.MissingCount++
			if len(d.Missing) < shownShifts {
				d.Missing = append(d.Missing, key(s))
			}
		}
	}
	for _, s := range got {
		if !wanted[key(s)] {
			d.ExtraCount++
			if len(d.Extra) < shownShifts {
				d.Extra = append(d.Extra, key(s))
			}
		}
	}
	return d
}

func between(pattern, zone string, from, to time.Time) []shiftpattern.Start {
	_, p := shiftpattern.Read(pattern, zone)
	if p == nil {
		return nil
	}
	return p.Between(from, to)
}

func picked(rule string, starts []shiftpattern.Start) []shiftpattern.Start {
	_, r := shiftpattern.ReadRule(rule)
	if r == nil {
		return nil
	}
	ats := make([]time.Time, len(starts))
	for i, s := range starts {
		ats[i] = s.At
	}
	var out []shiftpattern.Start
	for i, hit := range r.Picks(ats) {
		if hit {
			out = append(out, starts[i])
		}
	}
	return out
}

func formatShift(s shiftpattern.Start) string {
	length := strings.TrimSuffix(strings.TrimSuffix(s.Length.String(), "0s"), "0m")
	return s.At.Format("Mon 2 Jan 2006 15:04") + " for " + length
}

// checkIssues is everything the code can tell is wrong with the issues
// raised: their kinds, and each message's length and words. Whether an issue
// is about the right thing takes a judge.
func checkIssues(want []ExpectedIssue, got []patterntranslator.Issue) []string {
	var failures []string
	var wantKinds, gotKinds []string
	for _, w := range want {
		wantKinds = append(wantKinds, string(w.Kind))
	}
	for _, g := range got {
		gotKinds = append(gotKinds, string(g.Kind))
	}
	wantKinds, gotKinds = kindSet(wantKinds), kindSet(gotKinds)
	if !slices.Equal(wantKinds, gotKinds) {
		failures = append(failures, fmt.Sprintf("kinds: raised %s; wanted %s",
			strings.Join(gotKinds, ", "), strings.Join(wantKinds, ", ")))
	}
	for _, g := range got {
		if n := len(strings.Fields(g.Message)); n > maxWords {
			failures = append(failures, fmt.Sprintf("%q: %d words, want under %d", g.Message, n, maxWords+1))
		}
		if term := formatTerms.FindString(g.Message); term != "" {
			failures = append(failures, fmt.Sprintf("%q: names %s", g.Message, term))
		}
	}
	return failures
}

func kindSet(kinds []string) []string {
	slices.Sort(kinds)
	return slices.Compact(kinds)
}
