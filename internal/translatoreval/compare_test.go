package translatoreval

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"
)

// passes is n runs, the first passed of them passing, each costing cost.
func passes(passed, n int, cost float64) []RunReport {
	var runs []RunReport
	for i := range n {
		runs = append(runs, run(i < passed, Translated, 1, cost, time.Second))
	}
	return runs
}

// modelReport is a report of model at effort, with the cases given.
func modelReport(model, effort string, cases ...CaseReport) *Report {
	return &Report{
		Model: model, Effort: effort, Repeats: 4, Priced: true,
		Started: time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC),
		Prompts: map[string]string{"pattern": "aaaaaaaaaaaa", "rule": "bbbbbbbbbbbb"},
		Cases:   cases,
	}
}

func translation(name string, runs []RunReport) CaseReport {
	return CaseReport{Name: name, Kind: Patterns, Translates: true, Runs: runs}
}

func wantsIssues(name string, runs []RunReport) CaseReport {
	return CaseReport{Name: name, Kind: Patterns, Runs: runs}
}

func caseNames(rows []CaseRow) []string {
	var names []string
	for _, r := range rows {
		names = append(names, r.Name)
	}
	return names
}

func TestCompare_PutsTheCasesTheModelsDisagreeOnFirst(t *testing.T) {
	c := Compare([]*Report{
		modelReport("anthropic-small", "low",
			translation("agreed", passes(4, 4, 0.01)),
			translation("split-a-little", passes(3, 4, 0.01)),
			translation("split-wide", passes(0, 4, 0.01))),
		modelReport("anthropic-medium", "low",
			translation("agreed", passes(4, 4, 0.02)),
			translation("split-a-little", passes(4, 4, 0.02)),
			translation("split-wide", passes(4, 4, 0.02))),
	})
	if got, want := caseNames(c.Cases), []string{"split-wide", "split-a-little", "agreed"}; !slices.Equal(got, want) {
		t.Errorf("case order = %v, want %v", got, want)
	}
	if got := c.Cases[0].Rates; got[0] != (Tally{Passed: 0, First: 0, Runs: 4}) || got[1] != (Tally{Passed: 4, First: 4, Runs: 4}) {
		t.Errorf("split-wide rates = %v", got)
	}
}

func TestCompare_MarksTheModelsNoOtherBeatsOnPassRateAndCost(t *testing.T) {
	c := Compare([]*Report{
		// Cheapest, worst: nothing is both cheaper and better.
		modelReport("anthropic-small", "low", translation("a", passes(2, 4, 0.01))),
		// Dearer than small and no better: beaten by small.
		modelReport("mistral-small", "high", translation("a", passes(2, 4, 0.02))),
		// Best, dearest: nothing is better.
		modelReport("anthropic-large", "low", translation("a", passes(4, 4, 0.05))),
		// As good as large and dearer: beaten by large.
		modelReport("mistral-large", "low", translation("a", passes(4, 4, 0.06))),
	})
	var best []string
	for _, r := range c.Rows {
		if r.Best {
			best = append(best, r.Name)
		}
	}
	if want := []string{"anthropic-small, low effort", "anthropic-large, low effort"}; !slices.Equal(best, want) {
		t.Errorf("best = %v, want %v", best, want)
	}
}

func TestCompare_AnUnpricedModelIsNeverBest(t *testing.T) {
	stub := modelReport("stub", "", translation("a", passes(4, 4, 0)))
	stub.Priced = false
	c := Compare([]*Report{stub, modelReport("anthropic-small", "low", translation("a", passes(2, 4, 0.01)))})
	if c.Rows[0].Best || !c.Rows[1].Best {
		t.Errorf("best = %v, %v; want the priced model only", c.Rows[0].Best, c.Rows[1].Best)
	}
}

func TestCompare_ComparesOnTheSharedCasesAndListsTheRest(t *testing.T) {
	small := modelReport("anthropic-small", "low",
		translation("both", passes(4, 4, 0.01)),
		translation("small-only", passes(0, 4, 0.01)))
	medium := modelReport("anthropic-medium", "low",
		translation("both", passes(2, 4, 0.02)),
		wantsIssues("medium-only", passes(4, 4, 0.02)))
	c := Compare([]*Report{small, medium})

	if got := caseNames(c.Cases); !slices.Equal(got, []string{"both"}) {
		t.Errorf("cases = %v, want only the shared one", got)
	}
	// small-only's four failures are left out of small's rate.
	if got := c.Rows[0].Summary.All; got != (Tally{Passed: 4, First: 4, Runs: 4}) {
		t.Errorf("small's rate = %v, want 4/4 on the shared case", got)
	}
	if got := c.Rows[0].Only; !slices.Equal(got, []string{"small-only"}) {
		t.Errorf("only small ran %v", got)
	}
	if got := c.Rows[1].Only; !slices.Equal(got, []string{"medium-only"}) {
		t.Errorf("only medium ran %v", got)
	}
}

func TestCompare_SplitsAccuracyBetweenTranslationsAndIssues(t *testing.T) {
	c := Compare([]*Report{modelReport("anthropic-small", "low",
		translation("t", passes(3, 4, 0.01)),
		wantsIssues("i", passes(1, 4, 0.01)))})
	r := c.Rows[0]
	if r.Translations.Passed != 3 || r.Translations.Runs != 4 || r.Issues.Passed != 1 || r.Issues.Runs != 4 {
		t.Errorf("translations %v, issues %v; want 3/4 and 1/4", r.Translations, r.Issues)
	}
}

func TestCompare_FlagsReportsAskedUnderDifferentPrompts(t *testing.T) {
	same := Compare([]*Report{modelReport("a", "low"), modelReport("b", "low")})
	if same.PromptsDiffer {
		t.Error("the same prompts are flagged")
	}
	changed := modelReport("b", "low")
	changed.Prompts = map[string]string{"pattern": "cccccccccccc", "rule": "bbbbbbbbbbbb"}
	c := Compare([]*Report{modelReport("a", "low"), changed})
	if !c.PromptsDiffer {
		t.Fatal("different prompts are not flagged")
	}
	if md := RenderComparison(c); !strings.Contains(md, "cccccccccccc") || !strings.Contains(md, "aaaaaaaaaaaa") {
		t.Errorf("the header does not say which prompts each ran:\n%s", md)
	}
}

func TestCompare_TellsApartTwoRunsOfOneModel(t *testing.T) {
	later := modelReport("anthropic-small", "low")
	later.Started = later.Started.AddDate(0, 0, 1)
	c := Compare([]*Report{modelReport("anthropic-small", "low"), later, modelReport("anthropic-small", "low"), modelReport("anthropic-medium", "low")})
	var names []string
	for _, r := range c.Rows {
		names = append(names, r.Name)
	}
	want := []string{"anthropic-small, low effort, 2026-11-03 #1", "anthropic-small, low effort, 2026-11-04", "anthropic-small, low effort, 2026-11-03 #2", "anthropic-medium, low effort"}
	if !slices.Equal(names, want) {
		t.Errorf("names = %q, want %q", names, want)
	}
}

// pointFor is the chart's point for the row named name.
func pointFor(t *testing.T, ch *chart, name string) point {
	t.Helper()
	i := slices.IndexFunc(ch.Points, func(p point) bool { return p.Name == name })
	if i < 0 {
		t.Fatalf("no point for %s among %v", name, ch.Points)
	}
	return ch.Points[i]
}

func TestPlot_SpacesCostOnALogScale(t *testing.T) {
	ch := plot(Compare([]*Report{
		modelReport("anthropic-small", "low", translation("a", passes(2, 4, 0.001))),
		modelReport("anthropic-medium", "low", translation("a", passes(3, 4, 0.01))),
		modelReport("anthropic-large", "low", translation("a", passes(4, 4, 0.1))),
	}))
	small := pointFor(t, ch, "anthropic-small, low effort")
	medium := pointFor(t, ch, "anthropic-medium, low effort")
	large := pointFor(t, ch, "anthropic-large, low effort")
	// Ten times dearer is as far again, so medium sits halfway.
	if mid := (small.X + large.X) / 2; math.Abs(medium.X-mid) > 0.1 {
		t.Errorf("medium at x %.2f, want halfway between %.2f and %.2f", medium.X, small.X, large.X)
	}
	if small.X <= ch.Left || large.X >= ch.Right {
		t.Errorf("points at %.2f and %.2f fall outside the plot, %.2f to %.2f", small.X, large.X, ch.Left, ch.Right)
	}
}

func TestPlot_PutsTheModelsThatPassMoreHigher(t *testing.T) {
	ch := plot(Compare([]*Report{
		modelReport("anthropic-small", "low", translation("a", passes(2, 4, 0.01))),
		modelReport("anthropic-large", "low", translation("a", passes(4, 4, 0.04))),
	}))
	small, large := pointFor(t, ch, "anthropic-small, low effort"), pointFor(t, ch, "anthropic-large, low effort")
	if large.Y != ch.Top {
		t.Errorf("all passing at y %.2f, want the top, %.2f", large.Y, ch.Top)
	}
	if !(small.Y > large.Y && small.Y < ch.Bottom) {
		t.Errorf("half passing at y %.2f, want below %.2f and above the axis at %.2f", small.Y, large.Y, ch.Bottom)
	}
}

func TestPlot_DrawsAStepThroughTheModelsNoOtherBeats(t *testing.T) {
	ch := plot(Compare([]*Report{
		modelReport("anthropic-small", "low", translation("a", passes(2, 4, 0.01))),
		modelReport("mistral-small", "high", translation("a", passes(2, 4, 0.02))),
		modelReport("anthropic-large", "low", translation("a", passes(4, 4, 0.05))),
	}))
	small, large := pointFor(t, ch, "anthropic-small, low effort"), pointFor(t, ch, "anthropic-large, low effort")
	// Along from small at its pass rate to large's cost, then up to large.
	want := fmt.Sprintf("%.1f,%.1f %.1f,%.1f %.1f,%.1f", small.X, small.Y, large.X, small.Y, large.X, large.Y)
	if ch.Frontier != want {
		t.Errorf("frontier = %q, want %q", ch.Frontier, want)
	}
}

func TestPlot_LeavesOutAModelWithoutAPrice(t *testing.T) {
	stub := modelReport("stub", "", translation("a", passes(4, 4, 0)))
	stub.Priced = false
	ch := plot(Compare([]*Report{stub, modelReport("anthropic-small", "low", translation("a", passes(2, 4, 0.01)))}))
	if len(ch.Points) != 1 || ch.Points[0].Name != "anthropic-small, low effort" {
		t.Errorf("points = %v, want the priced model only", ch.Points)
	}
	if plot(Compare([]*Report{stub})) != nil {
		t.Error("a chart with nothing priced to put on it")
	}
}

func TestRenderComparison_ShadesEachCaseByItsPassRate(t *testing.T) {
	html := RenderComparison(Compare([]*Report{
		modelReport("anthropic-small", "low", translation("a", passes(3, 4, 0.01))),
		modelReport("anthropic-medium", "low", translation("a", passes(1, 4, 0.01))),
	}))
	for _, want := range []string{"--rate:0.75", "--rate:0.25"} {
		if !strings.Contains(html, want) {
			t.Errorf("no %q in:\n%s", want, html)
		}
	}
}

func TestRenderComparison_EscapesWhatTheReportsSay(t *testing.T) {
	html := RenderComparison(Compare([]*Report{
		modelReport("anthropic-small", "low", translation("<i>a</i>", passes(3, 4, 0.01))),
	}))
	if strings.Contains(html, "<i>a</i>") || !strings.Contains(html, "&lt;i&gt;a&lt;/i&gt;") {
		t.Errorf("a case name is not escaped:\n%s", html)
	}
}
