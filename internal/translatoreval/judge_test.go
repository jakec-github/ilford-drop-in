package translatoreval

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jakechorley/ilford-drop-in/pkg/core/llm"
	"github.com/jakechorley/ilford-drop-in/pkg/core/patterntranslator"
)

// judgeModel replies with one judgement, whatever it is asked, and keeps what
// it was asked.
type judgeModel struct {
	reply string
	err   error

	prompts []*llm.Prompt
	turns   []string
}

func (m *judgeModel) Reply(_ context.Context, prompt *llm.Prompt, turns []llm.Turn) (llm.Response, error) {
	m.prompts = append(m.prompts, prompt)
	m.turns = append(m.turns, turns[0].Text)
	return llm.Response{Text: m.reply, Usage: callUsage}, m.err
}

// twoIssuesCase wants two issues; the model under test raises two, one about
// the first and one made up, so the second is missed.
func twoIssuesCase() Case {
	c := issuesCase(Patterns,
		ExpectedIssue{Kind: patterntranslator.Missing, About: "how long the Sunday shifts run"},
		ExpectedIssue{Kind: patterntranslator.Ambiguous, Or: patterntranslator.Missing, About: "what time the evening shifts start"},
	)
	c.Name, c.Text = "sunday-evenings", "Sunday evenings"
	return c
}

func raisedTwo() string {
	return `{"status":"issues","issues":[` +
		`{"kind":"missing","message":"Add how long the Sunday shifts run","examples":["for 3 hours"]},` +
		`{"kind":"unsupported","message":"Bank holidays are not supported","examples":[]}]}`
}

const judgedTwo = `{"raised":[` +
	`{"issue":1,"matches":1,"reason":"Both are about the length."},` +
	`{"issue":2,"matches":0,"reason":"The description names no bank holiday."}],` +
	`"missed":[{"expected":2,"reason":"Nothing asks what time evenings start."}]}`

func judgedRun(t *testing.T, judge *judgeModel) RunReport {
	t.Helper()
	model := &fakeModel{replies: map[string][]string{"Sunday evenings": {raisedTwo()}}}
	report := Run(context.Background(),
		Subject{Model: "anthropic-small", Price: subjectPrice, Base: model,
			JudgeModel: "anthropic-large", JudgeModelID: "claude-opus-5-5", JudgeEffort: "medium", JudgePrice: judgePrice, Judge: judge},
		[]Case{twoIssuesCase()}, Options{Repeats: 1})
	if report.Judge != "anthropic-large" || report.JudgeID != "claude-opus-5-5" || report.JudgeEffort != "medium" || len(report.JudgePrompt) != 12 {
		t.Errorf("report names judge %q at %q effort, prompt %q", report.Judge, report.JudgeEffort, report.JudgePrompt)
	}
	return report.Cases[0].Runs[0]
}

func TestJudge_PairsEachIssueRaisedAndSaysWhichWereMissed(t *testing.T) {
	judge := &judgeModel{reply: judgedTwo}
	r := judgedRun(t, judge)

	j := r.Judgement
	if j == nil || j.Error != "" {
		t.Fatalf("judgement: %+v", j)
	}
	if len(j.Raised) != 2 || j.Raised[0].Matches != 1 || j.Raised[0].Reason != "Both are about the length." ||
		j.Raised[1].Matches != 0 || j.Raised[1].Reason != "The description names no bank holiday." {
		t.Errorf("raised: %+v", j.Raised)
	}
	if len(j.Missed) != 1 || j.Missed[0].Expected != 2 || j.Missed[0].Reason != "Nothing asks what time evenings start." {
		t.Errorf("missed: %+v", j.Missed)
	}
	if j.MadeUp() != 1 {
		t.Errorf("made up = %d, want 1", j.MadeUp())
	}
	// The judge's cost is its own, apart from the run's.
	if j.Usage != callUsage || j.Cost != judgePrice.Cost(callUsage) || r.Cost != subjectPrice.Cost(callUsage) {
		t.Errorf("judge usage %+v cost %v; run cost %v", j.Usage, j.Cost, r.Cost)
	}

	if len(judge.prompts) != 1 || judge.prompts[0] != judgePrompt {
		t.Fatalf("the judge was asked %d times, under %v", len(judge.prompts), judge.prompts)
	}
	asked := judge.turns[0]
	for _, want := range []string{
		"Sunday evenings",
		"1. missing: how long the Sunday shifts run",
		"2. ambiguous or missing: what time the evening shifts start",
		"1. missing: Add how long the Sunday shifts run",
		"2. unsupported: Bank holidays are not supported",
	} {
		if !strings.Contains(asked, want) {
			t.Errorf("the judge's turn lacks %q:\n%s", want, asked)
		}
	}
}

// An expected issue nothing was paired with is missed whether or not the
// judge listed it; one it listed but paired anyway is not.
func TestJudge_MissedIsWhatNothingWasPairedWith(t *testing.T) {
	r := judgedRun(t, &judgeModel{reply: `{"raised":[` +
		`{"issue":1,"matches":1,"reason":"length"},{"issue":2,"matches":0,"reason":"made up"}],` +
		`"missed":[{"expected":1,"reason":"wrongly listed"}]}`})

	j := r.Judgement
	if j.Error != "" || len(j.Missed) != 1 || j.Missed[0].Expected != 2 || j.Missed[0].Reason != "" {
		t.Errorf("judgement: %+v", j)
	}
}

func TestJudge_AVerdictThatDoesNotFitIsNoJudgement(t *testing.T) {
	for name, reply := range map[string]string{
		"not JSON":            "Sure",
		"an issue left out":   `{"raised":[{"issue":1,"matches":1,"reason":"x"}],"missed":[]}`,
		"an issue twice":      `{"raised":[{"issue":1,"matches":1,"reason":"x"},{"issue":1,"matches":0,"reason":"y"}],"missed":[]}`,
		"no such issue":       `{"raised":[{"issue":1,"matches":1,"reason":"x"},{"issue":3,"matches":0,"reason":"y"}],"missed":[]}`,
		"no such expectation": `{"raised":[{"issue":1,"matches":1,"reason":"x"},{"issue":2,"matches":3,"reason":"y"}],"missed":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			r := judgedRun(t, &judgeModel{reply: reply})
			if r.Judgement == nil || r.Judgement.Error == "" || len(r.Judgement.Raised) != 0 {
				t.Errorf("judgement: %+v", r.Judgement)
			}
			// The kinds fail it, as they would with no judge; the judge
			// adds nothing.
			if len(r.Failures) != 1 || !strings.HasPrefix(r.Failures[0], "kinds:") {
				t.Errorf("failures: %v", r.Failures)
			}
		})
	}

	r := judgedRun(t, &judgeModel{err: errors.New("overloaded")})
	if r.Judgement == nil || !strings.Contains(r.Judgement.Error, "overloaded") {
		t.Errorf("judgement: %+v", r.Judgement)
	}
}

// Only issues are judged, and only where issues were wanted: a translation,
// or issues where a translation was wanted, have nothing to pair.
func TestJudge_OnlyIssuesWhereIssuesWereWantedAreJudged(t *testing.T) {
	judge := &judgeModel{reply: judgedTwo}
	model := &fakeModel{replies: map[string][]string{
		"Sundays 7:30 to 10:30pm": {raisedTwo()},
	}}
	r := Run(context.Background(),
		Subject{Model: "m", Base: model, JudgeModel: "j", Judge: judge},
		[]Case{sundaysCase()}, Options{Repeats: 1}).Cases[0].Runs[0]
	if r.Judgement != nil || len(judge.prompts) != 0 {
		t.Errorf("judged a case that wants a translation: %+v", r.Judgement)
	}

	r = Run(context.Background(), Subject{Model: "m", Base: &fakeModel{replies: map[string][]string{"Sunday evenings": {raisedTwo()}}}},
		[]Case{twoIssuesCase()}, Options{Repeats: 1}).Cases[0].Runs[0]
	if r.Judgement != nil {
		t.Errorf("judged with no judge: %+v", r.Judgement)
	}
}

func TestJudge_ThePromptLoadsAtStartUp(t *testing.T) {
	if judgePrompt.Name != "judge" || !strings.Contains(judgePrompt.System, "made up") || judgePrompt.Schema["type"] != "object" {
		t.Errorf("judge prompt: %+v", judgePrompt)
	}
}
