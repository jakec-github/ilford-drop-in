package translatoreval

import (
	"math"
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

func sampleReport() *Report {
	return &Report{
		Model: "claude-sonnet-5-5", Effort: "low", Repeats: 2, Priced: true,
		Started: time.Date(2026, 11, 2, 14, 0, 0, 0, time.UTC),
		Prompts: map[string]string{"pattern": "aaaaaaaaaaaa", "rule": "bbbbbbbbbbbb"},
		Cases: []CaseReport{
			{Name: "sundays", Kind: Patterns, Text: "Sundays 7:30 to 10:30pm", Translates: true, Runs: []RunReport{
				run(true, Translated, 1, 0.01, 2*time.Second),
				run(false, Translated, 2, 0.02, 4*time.Second),
			}},
			{Name: "no-length", Kind: Patterns, Text: "Sunday evenings", Runs: []RunReport{
				run(true, Raised, 1, 0.01, time.Second),
				run(true, Raised, 1, 0.01, time.Second),
			}},
			{Name: "evening", Kind: Rules, Text: "The Sunday evening shift", Translates: true, Runs: []RunReport{
				run(false, GaveUp, 3, 0.03, 9*time.Second),
				{Answer: Answer{Outcome: Unavailable, Error: "deadline"}, Grade: Grade{Failures: []string{"unavailable: deadline"}}, Calls: 2, LatencyMS: 30000, TimedOut: true},
			}},
		},
	}
}

func TestSummarise_CountsPassesByKindAndHowManyCallsEachTook(t *testing.T) {
	s := Summarise(sampleReport().Cases)
	if s.All != (Tally{Passed: 3, Runs: 6}) {
		t.Errorf("all: %+v", s.All)
	}
	want := map[string]Tally{
		"Patterns, translations": {1, 2},
		"Patterns, issues":       {2, 2},
		"Rules, translations":    {0, 2},
	}
	if len(s.Groups) != len(want) {
		t.Errorf("groups: %+v", s.Groups)
	}
	for _, g := range s.Groups {
		if want[g.Label] != g.Tally {
			t.Errorf("%s: %+v, want %+v", g.Label, g.Tally, want[g.Label])
		}
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
	md := Render(r, nil)

	for _, want := range []string{
		"# Translator eval: claude-sonnet-5-5, low effort",
		"2 repeats",
		"`pattern` aaaaaaaaaaaa",
		"| All | 3/6 (50%) |",
		"| Rules, translations | 0/2 (0%) |",
		"| Gave up | 1 |",
		"| sundays | patterns | translation | 1/2 | 1 / 1 / 0 / 0 | $0.0150 | 2.0s | 4.0s |",
		"| evening | rules | translation | 0/2 | 0 / 0 / 0 / 2 |",
		"### sundays",
		"> Sundays 7:30 to 10:30pm",
		"Run 2: other Shifts: 1 missing, 0 extra",
		"Every Sunday at 20:00 for 3 hours, from 1 Nov 2026",
		"Missing: Sun 1 Nov 2026 19:30 for 3h",
		"1 over the 30s limit",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("report lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "### no-length") {
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
