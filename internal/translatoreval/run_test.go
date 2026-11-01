package translatoreval

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jakechorley/ilford-drop-in/pkg/core/patterntranslator"
)

// fakeModel answers by description: the nth reply for a description is the
// one given for its nth call, read from how many turns there are so far, so
// Generates can run at once without sharing a count.
type fakeModel struct {
	replies map[string][]string
	err     error

	mu    sync.Mutex
	turns []string
}

var callUsage = patterntranslator.Usage{InputTokens: 1000, OutputTokens: 200}

func (m *fakeModel) Reply(_ context.Context, _ *patterntranslator.Prompt, turns []patterntranslator.Turn) (string, patterntranslator.Usage, error) {
	m.mu.Lock()
	m.turns = append(m.turns, turns[0].Text)
	m.mu.Unlock()
	if m.err != nil {
		return "", patterntranslator.Usage{}, m.err
	}
	for text, replies := range m.replies {
		if strings.Contains(turns[0].Text, "<description>\n"+text+"\n</description>") {
			return replies[min(len(turns)/2, len(replies)-1)], callUsage, nil
		}
	}
	return "", callUsage, errors.New("fakeModel: no reply for this description")
}

func okPattern(p string) string {
	return `{"status":"ok","pattern":"` + strings.ReplaceAll(p, "\n", `\n`) + `"}`
}

func okRule(r string) string {
	return `{"status":"ok","rule":"` + strings.ReplaceAll(r, "\n", `\n`) + `"}`
}

const noDuration = "BEGIN:VEVENT\nDTSTART:20261101T193000\nRRULE:FREQ=WEEKLY;BYDAY=SU\nEND:VEVENT"

func sundaysCase() Case {
	c := patternCase(sundays)
	c.Name, c.Text = "sundays", "Sundays 7:30 to 10:30pm"
	return c
}

func TestRun_EachRepeatIsGradedWithTheCallsItTookAndWhatTheyCost(t *testing.T) {
	model := &fakeModel{replies: map[string][]string{
		"Sundays 7:30 to 10:30pm": {okPattern(noDuration), okPattern(sundays)},
	}}
	var progress []int
	report := Run(context.Background(),
		Subject{Model: "claude-sonnet-5-5", Effort: "low", Base: model},
		[]Case{sundaysCase()},
		Options{Repeats: 3, Parallel: 2, Progress: func(done, total int) {
			if total != 3 {
				t.Errorf("total = %d, want 3", total)
			}
			progress = append(progress, done)
		}})

	if len(report.Cases) != 1 || len(report.Cases[0].Runs) != 3 {
		t.Fatalf("report: %+v", report.Cases)
	}
	for _, r := range report.Cases[0].Runs {
		if !r.Pass || r.Outcome != Translated || r.Calls != 2 {
			t.Errorf("run: %+v", r)
		}
		if r.Usage.InputTokens != 2000 || r.Usage.OutputTokens != 400 {
			t.Errorf("usage: %+v", r.Usage)
		}
		// Sonnet 5.5: $2 a million in, $10 out.
		if want := 2000*2/1e6 + 400*10/1e6; r.Cost != want {
			t.Errorf("cost = %v, want %v", r.Cost, want)
		}
		if strings.Join(r.Descriptions, "") != "Every Sunday at 19:30 for 3 hours, from 1 Nov 2026" {
			t.Errorf("descriptions: %q", r.Descriptions)
		}
	}
	if len(progress) != 3 || progress[2] != 3 {
		t.Errorf("progress: %v", progress)
	}
	if !report.Priced || report.Repeats != 3 || report.Model != "claude-sonnet-5-5" {
		t.Errorf("header: %+v", report)
	}
	for _, p := range patterntranslator.Prompts() {
		if len(report.Prompts[p.Name]) != 12 {
			t.Errorf("prompt %s hash %q", p.Name, report.Prompts[p.Name])
		}
	}
}

func TestRun_TheTranslatorIsToldTheCasesTodayAndARulesPattern(t *testing.T) {
	c := ruleCase("DTSTART;VALUE=DATE:20261101\nRRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30")
	c.Name, c.Text, c.Today = "evening", "The Sunday evening shift", date(2026, 12, 25)
	model := &fakeModel{replies: map[string][]string{c.Text: {okRule(c.Expect.Rule)}}}
	report := Run(context.Background(), Subject{Model: "m", Base: model}, []Case{c}, Options{Repeats: 1})

	if r := report.Cases[0].Runs[0]; !r.Pass || r.Descriptions[0] != "Every Sunday at 19:30, from 1 Nov 2026" {
		t.Errorf("run: %+v", r)
	}
	turn := model.turns[0]
	for _, want := range []string{"Today: 2026-12-25 (Friday)", "- Every Sunday at 10:00 for 2 hours, from 1 Nov 2026"} {
		if !strings.Contains(turn, want) {
			t.Errorf("first turn %q lacks %q", turn, want)
		}
	}
	if report.Priced {
		t.Error("a model with no price is priced")
	}
}

func TestRun_GivingUpAndBeingUnavailableAreOutcomesOfTheirOwn(t *testing.T) {
	refused := &fakeModel{replies: map[string][]string{"Sundays 7:30 to 10:30pm": {okPattern(noDuration)}}}
	r := Run(context.Background(), Subject{Model: "m", Base: refused}, []Case{sundaysCase()}, Options{Repeats: 1}).Cases[0].Runs[0]
	if r.Outcome != GaveUp || r.Calls != patterntranslator.MaxCalls || r.Pass {
		t.Errorf("gave up: %+v", r)
	}

	down := &fakeModel{err: errors.New("overloaded")}
	r = Run(context.Background(), Subject{Model: "m", Base: down}, []Case{sundaysCase()}, Options{Repeats: 1}).Cases[0].Runs[0]
	if r.Outcome != Unavailable || r.Calls != 1 || r.TimedOut || !strings.Contains(r.Error, "overloaded") {
		t.Errorf("unavailable: %+v", r)
	}
}

func TestRun_TheStubCostsNothing(t *testing.T) {
	c := issuesCase(Patterns, ExpectedIssue{Kind: patterntranslator.Missing, About: "length"})
	c.Name, c.Text = "issues", "an issue"
	r := Run(context.Background(), Subject{Model: "stub"}, []Case{c}, Options{Repeats: 1}).Cases[0].Runs[0]
	if r.Outcome != Raised || r.Calls != 0 || r.Cost != 0 {
		t.Errorf("stub: %+v", r)
	}
}
