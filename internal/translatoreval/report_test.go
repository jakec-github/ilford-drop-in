package translatoreval

import (
	"encoding/json"
	"maps"
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

func replied(r RunReport, replies ...Reply) RunReport {
	r.Replies = replies
	return r
}

const (
	emptyIssues = `{"status":"issues","issues":[]}`
	sentBack    = "The app cannot use that pattern:\n- entry 1: DURATION is missing\n\nReply again for the same description."
)

func sampleReport() *Report {
	return &Report{
		Model: "claude-sonnet-5-5", Effort: "low", Repeats: 2, Priced: true,
		Started: time.Date(2026, 11, 2, 14, 0, 0, 0, time.UTC),
		Prompts: map[string]string{"pattern": "aaaaaaaaaaaa", "rule": "bbbbbbbbbbbb"},
		Cases: []CaseReport{
			{Name: "sundays", Kind: Patterns, Group: "days and times", Text: "Sundays 7:30 to 10:30pm", Translates: true, Runs: []RunReport{
				run(true, Translated, 1, 0.01, 2*time.Second),
				replied(run(false, Translated, 2, 0.02, 4*time.Second),
					Reply{Text: okPattern(noDuration), Retry: Refused, SentBack: sentBack},
					Reply{Text: okPattern(sundays)}),
			}},
			{Name: "no-length", Kind: Patterns, Group: "what is missing", Text: "Sunday evenings", Runs: []RunReport{
				worded(run(true, Raised, 1, 0.01, time.Second), 1),
				replied(worded(run(true, Raised, 2, 0.01, time.Second), 0.5, `"Add how long the shifts on a Sunday evening run, in hours or by when they end please": 17 words, limit 15`),
					Reply{Text: emptyIssues, Retry: Unusable, Thinking: "Is 7:30 morning?"},
					Reply{Text: `{"status":"issues","issues":[{"kind":"missing","message":"Add how long"}]}`}),
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
	if s.All != (Tally{Passed: 3, First: 2, Runs: 6}) {
		t.Errorf("all: %+v", s.All)
	}
	// Each file's groups apart, in the order the cases ran.
	want := []Group{
		{Label: "Patterns: days and times", Tally: Tally{Passed: 1, First: 1, Runs: 2}},
		{Label: "Patterns: what is missing", Tally: Tally{Passed: 2, First: 1, Runs: 2}, Wording: Score{1.5, 2}},
		{Label: "Rules: days and times", Tally: Tally{Runs: 2}},
	}
	if !slices.Equal(s.Groups, want) {
		t.Errorf("groups: %+v, want %+v", s.Groups, want)
	}
	if s.Wording != (Score{1.5, 2}) || s.Wording.String() != "0.75" {
		t.Errorf("wording: %+v", s.Wording)
	}
	if s.Calls != (Calls{First: 2, Second: 2, GaveUp: 1, Unavailable: 1}) {
		t.Errorf("calls: %+v", s.Calls)
	}
	if s.Retries.Refused != 1 || !maps.Equal(s.Retries.Unusable, map[string]int{"issues with none in them": 1}) {
		t.Errorf("retries: %+v", s.Retries)
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
		{Text: "{\"status\":\"issues\",\n\"issues\":[]}", Thinking: "The time is clear.\n\nBut the length is not.", Retry: Unusable},
		{Error: "the model's reply cannot be used: stopped for refusal", Retry: Unusable},
		{Text: okPattern(noDuration)},
	}
	md := Render(r, nil)

	for _, want := range []string{
		"# Translator eval: claude-sonnet-5-5, low effort",
		"2 repeats",
		"`pattern` aaaaaaaaaaaa",
		"| | First call | In the end | Wording |",
		"| All | 2/6 (33%) | 3/6 (50%) | 0.75 |",
		"| Patterns: what is missing | 1/2 (50%) | 2/2 (100%) | 0.75 |",
		"| Rules: days and times | 0/2 (0%) | 0/2 (0%) | — |",
		"| Gave up | 1 |",
		// Every call after the first, by why it was made.
		"| Why the model was asked again | Calls |\n|---|---|\n| Refused by the parser | 1 |\n| Unusable: issues with none in them | 2 |\n| Unusable: stopped for refusal | 1 |\n",
		"### Patterns: days and times",
		"| Case | Wants | First call | In the end | Wording | Calls |",
		"| sundays | translation | 1/2 | 1/2 | — | 1 / 1 / 0 / 0 | $0.0150 | 2.0s | 4.0s |",
		"| no-length | issues | 1/2 | 2/2 | 0.75 | 1 / 1 / 0 / 0 |",
		"### Rules: days and times",
		"| evening | translation | 0/2 | 0/2 | — | 0 / 0 / 0 / 2 |",
		"### sundays",
		"> Sundays 7:30 to 10:30pm",
		"Run 2: other Shifts: 1 missing, 0 extra",
		"Every Sunday at 20:00 for 3 hours, from 1 Nov 2026",
		"Missing: Sun 1 Nov 2026 19:30 for 3h",
		"1 over the 30s limit",
		"## Wording",
		`- "Add how long the shifts on a Sunday evening run, in hours or by when they end please": 17 words, limit 15`,
		"- Call 1: `` {\"status\":\"issues\", \"issues\":[]} ``\n    - Thought: The time is clear. But the length is not.\n    - Asked again: unusable, issues with none in them\n  - Call 2: no reply",
		"- Call 2: no reply: the model's reply cannot be used: stopped for refusal\n    - Asked again: unusable, stopped for refusal\n  - Call 3:",
		// A failed run that was not given up still shows how it got there.
		"- Run 2: other Shifts: 1 missing, 0 extra",
		"    - Asked again: the parser refused it: entry 1: DURATION is missing\n",
		// A pass that took a second call is listed with its calls.
		"## Second goes",
		"### no-length\n\n> Sunday evenings\n\n- Run 2: passed on call 2\n  - Call 1: `` {\"status\":\"issues\",\"issues\":[]} ``\n    - Thought: Is 7:30 morning?\n    - Asked again: unusable, issues with none in them\n  - Call 2:",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("report lacks %q:\n%s", want, md)
		}
	}
	failures := md[strings.Index(md, "## Failures"):strings.Index(md, "## Second goes")]
	if strings.Contains(failures, "### no-length") {
		t.Error("a case that always passed is listed under failures")
	}
	if strings.Contains(md[strings.Index(md, "## Second goes"):], "### sundays") {
		t.Error("a run that failed is listed under second goes")
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
		"first call 1/2 (50%) → 1/2 (50%), in the end 2/2 (100%) → 1/2 (50%)",
		"| Case | First call | Change | In the end | Change |",
		"| sundays | 1/2 → 1/2 | 0 | 2/2 → 1/2 | −50 pts |",
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
	e := EstimateRun(Subject{Model: "m", Price: subjectPrice, Base: &fakeModel{}}, cases, 5)
	if e.Calls != 10 || e.MaxCalls != 30 || !e.Priced || e.Cost <= 0 {
		t.Errorf("estimate: %+v", e)
	}
	if e := EstimateRun(Subject{Model: "stub"}, cases, 5); e.Calls != 0 || e.Cost != 0 {
		t.Errorf("stub: %+v", e)
	}
}

func repairReport() *Report {
	r := sampleReport()
	repairs := []CaseReport{
		{Name: "no-duration", Kind: Patterns, Group: "repairs", Planted: noDuration, Translates: true, Runs: []RunReport{
			run(true, Translated, 2, 0.01, time.Second),
			replied(run(true, Translated, 3, 0.02, time.Second),
				Reply{Text: okPattern(noDuration), Retry: Refused, SentBack: sentBack},
				Reply{Text: `{"pattern":"","status":"ok"}`, Retry: Unusable},
				Reply{Text: okPattern(sundays)}),
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
	if s.All != (Tally{Passed: 3, First: 2, Runs: 6}) || len(s.Groups) != 3 {
		t.Errorf("first attempts: %+v, groups %+v", s.All, s.Groups)
	}
	if s.Calls.Second != 2 || s.Calls.Third != 0 || s.Retries.Refused != 1 {
		t.Errorf("calls count repairs: %+v, %+v", s.Calls, s.Retries)
	}
	want := Repairs{Fixed: 3, FirstFix: 2, Changed: 1, NotFixed: 1, Unavailable: 1}
	if s.Repairs != want {
		t.Errorf("repairs: %+v, want %+v", s.Repairs, want)
	}
	// The planted reply's refusal is the case, not the model's doing.
	if s.RepairRetries.Refused != 0 || !maps.Equal(s.RepairRetries.Unusable, map[string]int{"an empty answer": 1}) {
		t.Errorf("repair retries: %+v", s.RepairRetries)
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
		"| | Fixed | On the first model call | Parsed but changed | Not fixed | Unavailable |",
		"| All | 3/6 (50%) | 2/6 (33%) | 1 | 1 | 1 |",
		"| Patterns: repairs | 2/4 (50%) | 1/4 (25%) | 1 | 1 | 0 |",
		"| Why the model was asked again | Calls |\n|---|---|\n| Unusable: an empty answer | 1 |\n\n## Cases",
		// A repair case's own row counts its fixes and the loop's calls.
		"| no-duration | translation | 1/4 | 2/4 | — | 0 / 2 / 1 / 1 |",
		// A repair that took more than one model call is a second go; its
		// first call is the planted one.
		"### no-duration\n\n> ",
		"- Run 2: passed on call 3\n  - Call 1 (planted): ",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("report lacks %q:\n%s", want, md)
		}
	}
}

func TestRender_ARepairCaseIsComparedByItsFixes(t *testing.T) {
	md := Render(repairReport(), repairReport())
	for _, want := range []string{
		"Repairs on the cases both ran: fixed 3/6 (50%) → 3/6 (50%), on the first model call 2/6 (33%) → 2/6 (33%)",
		"| no-duration | 1/4 → 1/4 | 0 | 2/4 → 2/4 | 0 |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("comparison lacks %q:\n%s", want, md)
		}
	}
}

// A report names the tier asked for and the model it was; one written before
// tiers named only the model, and still reads as it did.
func TestRender_NamesTheTierAndTheModelItWas(t *testing.T) {
	r := sampleReport()
	r.Model, r.ModelID = "anthropic-medium", "claude-sonnet-5-5"
	if md := Render(r, nil); !strings.Contains(md, "# Translator eval: anthropic-medium (claude-sonnet-5-5), low effort\n") {
		t.Errorf("header:\n%s", md)
	}
	if got := FileName(r); got != "2026-11-02-anthropic-medium-low" {
		t.Errorf("FileName = %q", got)
	}

	data, err := json.Marshal(sampleReport())
	if err != nil {
		t.Fatal(err)
	}
	old, err := ReadReport(data)
	if err != nil {
		t.Fatalf("ReadReport: %v", err)
	}
	if md := Render(r, old); !strings.Contains(md, "claude-sonnet-5-5, low effort") {
		t.Errorf("a comparison with a report from before tiers does not name its model:\n%s", md)
	}
}

func TestEstimate_ThePlantedCallIsFree(t *testing.T) {
	c := sundaysCase()
	c.Planted = noDuration
	e := EstimateRun(Subject{Model: "m", Price: subjectPrice, Base: &fakeModel{}}, []Case{c}, 5)
	if e.Calls != 5 || e.MaxCalls != 5*(patterntranslator.MaxCalls-1) {
		t.Errorf("estimate: %+v", e)
	}
}

// judgedReport is sampleReport with a judge, and a case whose runs it judged:
// one with a match, a made-up issue and a miss, one it could not judge.
func judgedReport() *Report {
	r := sampleReport()
	r.Judge, r.JudgeID, r.JudgeEffort, r.JudgePrompt = "anthropic-large", "claude-opus-5-5", "medium", "dddddddddddd"
	raised := func(r RunReport, issues ...patterntranslator.Issue) RunReport {
		r.Issues = issues
		return r
	}
	r.Cases = append(r.Cases, CaseReport{
		Name: "sunday-evenings", Kind: Patterns, Group: "what is missing", Text: "Sunday evenings",
		Expected: []ExpectedIssue{
			{Kind: patterntranslator.Missing, About: "how long the Sunday shifts run"},
			{Kind: patterntranslator.Ambiguous, About: "what time the evening shifts start"},
		},
		Runs: []RunReport{
			func() RunReport {
				run := raised(run(false, Raised, 1, 0.01, time.Second),
					patterntranslator.Issue{Kind: patterntranslator.Missing, Message: "Add how long"},
					patterntranslator.Issue{Kind: patterntranslator.Unsupported, Message: "Bank holidays are not supported"})
				run.Judgement = &Judgement{
					Raised: []Verdict{{Matches: 1, Reason: "Both are about the length."}, {Matches: 0, Reason: "No bank holiday is named."}},
					Missed: []Miss{{Expected: 2, Reason: "Nothing asks what time evenings start."}},
					Cost:   0.02,
				}
				return run
			}(),
			func() RunReport {
				run := raised(run(true, Raised, 1, 0.01, time.Second),
					patterntranslator.Issue{Kind: patterntranslator.Missing, Message: "Add how long"})
				run.Judgement = &Judgement{Error: "the judge left out issue 1", Cost: 0.01}
				return run
			}(),
		},
	})
	return r
}

func TestSummarise_CountsWhatTheJudgeFoundAndWhatItCost(t *testing.T) {
	s := Summarise(judgedReport().Cases)
	want := Judged{Replies: 1, Right: 0, MadeUp: 1, Missed: 1, NotJudged: 1, Cost: 0.03}
	if math.Abs(s.Judged.Cost-want.Cost) > 1e-9 {
		t.Errorf("judge cost = %v, want %v", s.Judged.Cost, want.Cost)
	}
	s.Judged.Cost = want.Cost
	if s.Judged != want {
		t.Errorf("judged: %+v, want %+v", s.Judged, want)
	}
	// The run's cost leaves the judge's out.
	if math.Abs(s.Cost-0.10) > 1e-9 {
		t.Errorf("cost = %v, want the runs' own 0.10", s.Cost)
	}
}

func TestRender_ShowsTheJudgesPairingAndReasonBesideEachIssue(t *testing.T) {
	md := Render(judgedReport(), nil)
	for _, want := range []string{
		"Judged by anthropic-large (claude-opus-5-5) at medium effort · prompt dddddddddddd",
		// Judged right is replies with nothing made up and nothing missed,
		// of those judged.
		"| | First call | In the end | Wording | Judged right |",
		"| All | 3/8 (38%) | 4/8 (50%) | 0.75 | 0/1 (0%) |",
		"| Patterns: days and times | 1/2 (50%) | 1/2 (50%) | — | — |",
		"| Patterns: what is missing | 2/4 (50%) | 3/4 (75%) | 0.75 | 0/1 (0%) |",
		"| Case | Wants | First call | In the end | Wording | Judged right | Calls |",
		"| sunday-evenings | issues | 1/2 | 1/2 | — | 0/1 | 2 / 0 / 0 / 0 |",
		"| sundays | translation | 1/2 | 1/2 | — | — | 1 / 1 / 0 / 0 |",
		"Judged in all: 1 made up, 1 missed; 1 reply it could not judge · judge cost $0.0300, apart from the cost above",
		"## Judged issues",
		"### sunday-evenings\n\n> Sunday evenings\n\nWanted:\n\n1. missing: how long the Sunday shifts run\n2. ambiguous: what time the evening shifts start\n",
		"- Run 1:\n  - Raised missing: Add how long — about 1: Both are about the length.\n" +
			"  - Raised unsupported: Bank holidays are not supported — made up: No bank holiday is named.\n" +
			"  - Missed 2: Nothing asks what time evenings start.\n",
		"- Run 2: not judged: the judge left out issue 1\n",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("report lacks %q:\n%s", want, md)
		}
	}

	if md := Render(sampleReport(), nil); strings.Contains(md, "judge") || strings.Contains(md, "Judge") {
		t.Errorf("a report with no judge mentions one:\n%s", md)
	}
}

func TestEstimate_CountsAJudgeCallPerIssuesReply(t *testing.T) {
	issues := issuesCase(Patterns, ExpectedIssue{Kind: patterntranslator.Missing, About: "length"})
	issues.Text = "Sunday evenings"
	judged := Subject{Model: "m", Price: subjectPrice, Base: &fakeModel{}, JudgeModel: "j", JudgePrice: judgePrice, Judge: &judgeModel{}}
	e := EstimateRun(judged, []Case{sundaysCase(), issues}, 5)
	if e.JudgeCalls != 5 || e.JudgeCost <= 0 || !e.JudgePriced {
		t.Errorf("estimate: %+v", e)
	}
	if e := EstimateRun(Subject{Model: "m", Price: subjectPrice, Base: &fakeModel{}}, []Case{issues}, 5); e.JudgeCalls != 0 || e.JudgeCost != 0 {
		t.Errorf("no judge: %+v", e)
	}
}
