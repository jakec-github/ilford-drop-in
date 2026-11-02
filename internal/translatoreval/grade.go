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
// Pass is about what the answer means; how its messages are worded is scored
// apart, in Wording, and never fails it.
type Grade struct {
	Pass     bool       `json:"pass"`
	Failures []string   `json:"failures,omitempty"`
	Shifts   *ShiftDiff `json:"shifts,omitempty"`
	// Wording is, for issues the model raised, how well their messages keep
	// to the prompt's rules, from 0 to 1: the mean of each message's score.
	// It is nil for any other answer.
	Wording *float64 `json:"wording,omitempty"`
	// WordingNotes says what each message that lost points lost them for.
	WordingNotes []string `json:"wordingNotes,omitempty"`
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

// maxWords is how long an issue's message may be: "15 words at most", as
// both prompts say.
const maxWords = 15

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
		g.Failures = checkKinds(c.Expect.Issues, a.Issues)
	}
	if a.Outcome == Raised {
		g.Wording, g.WordingNotes = scoreWording(a.Issues)
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

// checkKinds is whether the issues raised are of the kinds wanted, taking
// each issue that may be either of two kinds as whichever fits. Whether each
// is about the right thing takes a judge.
func checkKinds(want []ExpectedIssue, got []patterntranslator.Issue) []string {
	var gotKinds []string
	for _, g := range got {
		gotKinds = append(gotKinds, string(g.Kind))
	}
	gotKinds = kindSet(gotKinds)

	var fixed, alternatives []string
	var either []ExpectedIssue
	for _, w := range want {
		if w.Or == "" {
			fixed = append(fixed, string(w.Kind))
		} else {
			either = append(either, w)
			alternatives = append(alternatives, fmt.Sprintf("%s or %s", w.Kind, w.Or))
		}
	}
	// Every way of taking each either-kind issue as one kind or the other.
	for choice := range 1 << len(either) {
		kinds := slices.Clone(fixed)
		for i, w := range either {
			k := w.Kind
			if choice&(1<<i) != 0 {
				k = w.Or
			}
			kinds = append(kinds, string(k))
		}
		if slices.Equal(kindSet(kinds), gotKinds) {
			return nil
		}
	}

	wanted := strings.Join(kindSet(fixed), ", ")
	if len(alternatives) > 0 {
		if wanted != "" {
			wanted += ", and "
		}
		wanted += strings.Join(alternatives, ", and ")
	}
	return []string{fmt.Sprintf("kinds: raised %s; wanted %s", strings.Join(gotKinds, ", "), wanted)}
}

func kindSet(kinds []string) []string {
	slices.Sort(kinds)
	return slices.Compact(kinds)
}

// scoreWording scores each message from 0 to 1 and gives their mean. A
// message naming the format scores 0: the Organiser cannot act on it. One over
// the word limit loses in proportion to how far over it is, so a message two
// words long loses little and one twice the limit scores 0.
func scoreWording(issues []patterntranslator.Issue) (*float64, []string) {
	if len(issues) == 0 {
		return nil, nil
	}
	var sum float64
	var notes []string
	for _, issue := range issues {
		score := 1.0
		if term := formatTerms.FindString(issue.Message); term != "" {
			score = 0
			notes = append(notes, fmt.Sprintf("%q: names %s", issue.Message, term))
		} else if n := len(strings.Fields(issue.Message)); n > maxWords {
			score = max(0, 1-float64(n-maxWords)/maxWords)
			notes = append(notes, fmt.Sprintf("%q: %d words, limit %d", issue.Message, n, maxWords))
		}
		sum += score
	}
	mean := sum / float64(len(issues))
	return &mean, notes
}
