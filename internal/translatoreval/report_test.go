package translatoreval

import (
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/core/patterntranslator"
)

func run(pass bool, outcome Outcome, calls int, cost float64, latency time.Duration) RunReport {
	r := RunReport{Answer: Answer{Outcome: outcome}, Grade: Grade{Pass: pass}, Calls: calls, Cost: cost, LatencyMS: latency.Milliseconds()}
	if !pass {
		r.Failures = []string{"other Shifts: 1 missing, 0 extra"}
	}
	return r
}

func worded(r RunReport, score float64, notes ...string) RunReport {
	r.Wording, r.WordingNotes = &score, notes
	return r
}

func sampleReport() *Report {
	return &Report{
		Model: "claude-sonnet-5-5", Effort: "low", Repeats: 2, Priced: true,
		Started: time.Date(2026, 11, 2, 14, 0, 0, 0, time.UTC),
		Prompts: map[string]string{"pattern": "aaaaaaaaaaaa", "rule": "bbbbbbbbbbbb"},
		Cases: []CaseReport{
			{Name: "sundays", Kind: Patterns, Group: "days and times", Text: "Sundays 7:30 to 10:30pm", Translates: true, Runs: []RunReport{
				run(true, Translated, 1, 0.01, 2*time.Second),
				run(false, Translated, 2, 0.02, 4*time.Second),
			}},
			{Name: "no-length", Kind: Patterns, Group: "what is missing", Text: "Sunday evenings", Runs: []RunReport{
				worded(run(true, Raised, 1, 0.01, time.Second), 1),
				worded(run(true, Raised, 1, 0.01, time.Second), 0.5, `"Add how long the shifts on a Sunday evening run, in hours or by when they end please": 17 words, limit 15`),
			}},
			{Name: "evening", Kind: Rules, Group: "days and times", Text: "The Sunday evening shift", Translates: true, Runs: []RunReport{
				run(false, GaveUp, 3, 0.03, 9*time.Second),
				{Answer: Answer{Outcome: Unavailable, Error: "deadline"}, Grade: Grade{Failures: []string{"unavailable: deadline"}}, Calls: 2, LatencyMS: 30000, TimedOut: true},
			}},
		},
	}
}

func TestSummarise_CountsPassesByGroupAndHowManyCallsEachTook(t *testing.T) {
	s := Summarise(sampleReport().Cases)
	if s.All != (Tally{Passed: 3, Runs: 6}) {
		t.Errorf("all: %+v", s.All)
	}
	// Each file's groups apart, in the order the cases ran.
	want := []Group{
		{Label: "Patterns: days and times", Tally: Tally{1, 2}},
		{Label: "Patterns: what is missing", Tally: Tally{2, 2}, Wording: Score{1.5, 2}},
		{Label: "Rules: days and times", Tally: Tally{0, 2}},
	}
	if !slices.Equal(s.Groups, want) {
		t.Errorf("groups: %+v, want %+v", s.Groups, want)
	}
	if s.Wording != (Score{1.5, 2}) || s.Wording.String() != "0.75" {
		t.Errorf("wording: %+v", s.Wording)
	}
	if s.Calls != (Calls{First: 3, Second: 1, GaveUp: 1, Unavailable: 1}) {
		t.Errorf("calls: %+v", s.Calls)
	}
	if math.Abs(s.Cost-0.08) > 1e-9 || s.TimedOut != 1 {
		t.Errorf("cost %v, timed out %d", s.Cost, s.TimedOut)
	}
	// Latencies 1, 1, 2, 4, 9, 30 seconds.
	if s.Typical != 2*time.Second || s.Slow != 30*time.Second {
		t.Errorf("typical %v, slow %v", s.Typical, s.Slow)
	}
}

func TestRender_SaysWhatRanAndWhatFailed(t *testing.T) {
	r := sampleReport()
	r.Cases[0].Runs[1].Answer.Answer = "BEGIN:VEVENT..."
	r.Cases[0].Runs[1].Descriptions = []string{"Every Sunday at 20:00 for 3 hours, from 1 Nov 2026"}
	r.Cases[0].Runs[1].Shifts = &ShiftDiff{Missing: []string{"Sun 1 Nov 2026 19:30 for 3h"}, MissingCount: 1}
	r.Cases[1].Runs[0].Issues = []patterntranslator.Issue{{Kind: patterntranslator.Missing, Message: "Add how long"}}
	r.Cases[2].Runs[0].Replies = []Reply{
		{Text: "{\"status\":\"issues\",\n\"issues\":[]}"},
		{Error: "the model's reply cannot be used: stopped for refusal"},
	}
	md := Render(r, nil)

	for _, want := range []string{
		"# Translator eval: claude-sonnet-5-5, low effort",
		"2 repeats",
		"`pattern` aaaaaaaaaaaa",
		"| All | 3/6 (50%) | 0.75 |",
		"| Rules: days and times | 0/2 (0%) | — |",
		"| Gave up | 1 |",
		"### Patterns: days and times",
		"| sundays | translation | 1/2 | — | 1 / 1 / 0 / 0 | $0.0150 | 2.0s | 4.0s |",
		"| no-length | issues | 2/2 | 0.75 |",
		"### Rules: days and times",
		"| evening | translation | 0/2 | — | 0 / 0 / 0 / 2 |",
		"### sundays",
		"> Sundays 7:30 to 10:30pm",
		"Run 2: other Shifts: 1 missing, 0 extra",
		"Every Sunday at 20:00 for 3 hours, from 1 Nov 2026",
		"Missing: Sun 1 Nov 2026 19:30 for 3h",
		"1 over the 30s limit",
		"## Wording",
		`- "Add how long the shifts on a Sunday evening run, in hours or by when they end please": 17 words, limit 15`,
		"- Call 1: `` {\"status\":\"issues\", \"issues\":[]} ``",
		"- Call 2: no reply: the model's reply cannot be used: stopped for refusal",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("report lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "### no-length\n\n>") {
		t.Error("a case that always passed is listed under failures")
	}
}

func TestRender_AModelWithNoPriceHasNoCost(t *testing.T) {
	r := sampleReport()
	r.Priced = false
	md := Render(r, nil)
	if strings.Contains(md, "$") || !strings.Contains(md, "| — |") {
		t.Errorf("unpriced report:\n%s", md)
	}
}

func TestRender_AgainstAnEarlierReportSaysWhatChanged(t *testing.T) {
	before := sampleReport()
	before.Prompts["pattern"] = "cccccccccccc"
	before.Cases = append(before.Cases[:1:1], CaseReport{Name: "dropped", Kind: Patterns, Runs: []RunReport{run(true, Raised, 1, 0.01, time.Second)}})
	before.Cases[0].Runs[1].Pass = true

	md := Render(sampleReport(), before)
	for _, want := range []string{
		"## Against claude-sonnet-5-5, low effort, 2026-11-02",
		"`pattern` prompt differs: cccccccccccc → aaaaaaaaaaaa",
		"| sundays | 2/2 | 1/2 | −50 pts |",
		"wording — → —",
		"Only in this run: evening, no-length",
		"Only in the earlier run: dropped",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("comparison lacks %q:\n%s", want, md)
		}
	}
}

func TestEstimate_CountsACallPerTranslationAndUpToThree(t *testing.T) {
	cases := []Case{sundaysCase(), sundaysCase()}
	e := EstimateRun("claude-sonnet-5-5", cases, 5)
	if e.Calls != 10 || e.MaxCalls != 30 || !e.Priced || e.Cost <= 0 {
		t.Errorf("estimate: %+v", e)
	}
	if e := EstimateRun("stub", cases, 5); e.Calls != 0 || e.Cost != 0 {
		t.Errorf("stub: %+v", e)
	}
}

func repairReport() *Report {
	r := sampleReport()
	repairs := []CaseReport{
		{Name: "no-duration", Kind: Patterns, Group: "repairs", Planted: noDuration, Translates: true, Runs: []RunReport{
			run(true, Translated, 2, 0.01, time.Second),
			run(true, Translated, 3, 0.02, time.Second),
			run(false, Translated, 2, 0.01, time.Second),
			run(false, GaveUp, 3, 0.02, time.Second),
		}},
		{Name: "time-in-a-date", Kind: Rules, Group: "repairs", Planted: "x", Translates: true, Runs: []RunReport{
			run(true, Translated, 2, 0.01, time.Second),
			{Answer: Answer{Outcome: Unavailable, Error: "overloaded"}, Grade: Grade{Failures: []string{"unavailable: overloaded"}}, Calls: 2},
		}},
	}
	r.Cases = append(r.Cases, repairs...)
	return r
}

// A repair is graded apart from first attempts: it starts from a refused
// reply, so counting it in the pass rate would mix two questions.
func TestSummarise_RepairsAreKeptApartFromThePassRate(t *testing.T) {
	s := Summarise(repairReport().Cases)
	if s.All != (Tally{Passed: 3, Runs: 6}) || len(s.Groups) != 3 {
		t.Errorf("first attempts: %+v, groups %+v", s.All, s.Groups)
	}
	if s.Calls.Second != 1 || s.Calls.Third != 0 {
		t.Errorf("calls count repairs: %+v", s.Calls)
	}
	want := Repairs{Fixed: 3, Changed: 1, NotFixed: 1, Unavailable: 1, ModelCalls: 4}
	if s.Repairs != want {
		t.Errorf("repairs: %+v, want %+v", s.Repairs, want)
	}
	if got := s.Repairs.CallsPerFix(); math.Abs(got-4.0/3) > 1e-9 {
		t.Errorf("calls a fix = %v, want 4/3", got)
	}
	if len(s.RepairGroups) != 2 || s.RepairGroups[0].Label != "Patterns: repairs" || s.RepairGroups[1].Repairs.Fixed != 1 {
		t.Errorf("repair groups: %+v", s.RepairGroups)
	}
}

func TestRender_RepairsHaveASectionOfTheirOwn(t *testing.T) {
	md := Render(repairReport(), nil)
	for _, want := range []string{
		"| All | 3/6 (50%) |",
		"## Repairs",
		"| | Fixed | Parsed but changed | Not fixed | Unavailable | Calls a fix |",
		"| All | 3/6 (50%) | 1 | 1 | 1 | 1.3 |",
		"| Patterns: repairs | 2/4 (50%) | 1 | 1 | 0 | 1.5 |",
		// A repair case's own row counts its fixes and the loop's calls.
		"| no-duration | translation | 2/4 | — | 0 / 2 / 1 / 1 |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("report lacks %q:\n%s", want, md)
		}
	}
}

func TestRender_ARepairCaseIsComparedByItsFixes(t *testing.T) {
	md := Render(repairReport(), repairReport())
	for _, want := range []string{
		"Repairs on the cases both ran: fixed 3/6 (50%) → 3/6 (50%), calls a fix 1.3 → 1.3",
		"| no-duration | 2/4 | 2/4 | 0 |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("comparison lacks %q:\n%s", want, md)
		}
	}
}

func TestEstimate_ThePlantedCallIsFree(t *testing.T) {
	c := sundaysCase()
	c.Planted = noDuration
	e := EstimateRun("claude-sonnet-5-5", []Case{c}, 5)
	if e.Calls != 5 || e.MaxCalls != 5*(patterntranslator.MaxCalls-1) {
		t.Errorf("estimate: %+v", e)
	}
}
