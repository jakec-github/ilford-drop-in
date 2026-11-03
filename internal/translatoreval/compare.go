package translatoreval

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/core/patterntranslator"
)

// Comparison is reports side by side, taken on the cases all of them ran: one
// row a report, and one a case.
type Comparison struct {
	Rows []ComparisonRow
	// Cases is the cases every report ran, those the reports disagree on most
	// first.
	Cases []CaseRow
	// PromptsDiffer is whether the reports were asked under different
	// prompts, so a difference may be the prompt's and not the model's.
	PromptsDiffer bool
}

// ComparisonRow is one report, on the shared cases.
type ComparisonRow struct {
	// Name is the model and effort, with the day it ran where two rows would
	// otherwise share a name.
	Name    string
	Report  *Report
	Summary Summary
	// Translations and Issues split the runs of the first-attempt cases by
	// what the case wants: a translation, or issues.
	Translations, Issues Tally
	// Best is a priced model no other priced model beats on both pass rate
	// and cost a translation.
	Best bool
	// Only is the cases this report ran that some other did not.
	Only []string
}

// CaseRow is one case's pass rate in each report, in the rows' order; for a
// repair case, how many runs were fixed.
type CaseRow struct {
	Name  string
	Rates []Tally
}

// Compare is reports side by side. It reads only the reports, so it costs
// nothing and can compare runs made on different days.
func Compare(reports []*Report) Comparison {
	var c Comparison
	counts := map[string]int{}
	for _, r := range reports {
		for _, cr := range r.Cases {
			counts[cr.Name]++
		}
	}
	shared := func(name string) bool { return counts[name] == len(reports) }

	names := rowNames(reports)
	for i, r := range reports {
		row := ComparisonRow{Name: names[i], Report: r}
		var cases []CaseReport
		for _, cr := range r.Cases {
			if !shared(cr.Name) {
				row.Only = append(row.Only, cr.Name)
				continue
			}
			cases = append(cases, cr)
			if cr.Planted != "" {
				continue
			}
			for _, run := range cr.Runs {
				if cr.Translates {
					row.Translations.add(run)
				} else {
					row.Issues.add(run)
				}
			}
		}
		row.Summary = Summarise(cases)
		c.Rows = append(c.Rows, row)
		if !maps.Equal(r.Prompts, reports[0].Prompts) {
			c.PromptsDiffer = true
		}
	}

	for i := range c.Rows {
		c.Rows[i].Best = c.Rows[i].Report.Priced && !slices.ContainsFunc(c.Rows, func(o ComparisonRow) bool {
			return beats(o, c.Rows[i])
		})
	}

	if len(reports) > 0 {
		for _, cr := range reports[0].Cases {
			if !shared(cr.Name) {
				continue
			}
			row := CaseRow{Name: cr.Name}
			for _, r := range reports {
				i := slices.IndexFunc(r.Cases, func(o CaseReport) bool { return o.Name == cr.Name })
				row.Rates = append(row.Rates, caseRate(r.Cases[i]))
			}
			c.Cases = append(c.Cases, row)
		}
	}
	slices.SortStableFunc(c.Cases, func(a, b CaseRow) int { return cmp.Compare(spread(b), spread(a)) })
	return c
}

// rowNames is each report's model and effort, with the day it ran where two
// share them, and a number in order where they share the day too.
func rowNames(reports []*Report) []string {
	names := make([]string, len(reports))
	count := func() map[string]int {
		n := map[string]int{}
		for _, name := range names {
			n[name]++
		}
		return n
	}
	for i, r := range reports {
		names[i] = subjectName(r)
	}
	n := count()
	for i, r := range reports {
		if n[names[i]] > 1 {
			names[i] += ", " + r.Started.Format(time.DateOnly)
		}
	}
	n, seen := count(), map[string]int{}
	for i := range names {
		if n[names[i]] > 1 {
			seen[names[i]]++
			names[i] = fmt.Sprintf("%s #%d", names[i], seen[names[i]])
		}
	}
	return names
}

// beats is whether a, priced, passes at least as often as b for no more a
// translation, and does better at one of them.
func beats(a, b ComparisonRow) bool {
	if !a.Report.Priced {
		return false
	}
	ra, rb := rate(a.Summary.All), rate(b.Summary.All)
	ca, cb := a.Summary.PerRun(), b.Summary.PerRun()
	return ra >= rb && ca <= cb && (ra > rb || ca < cb)
}

// rate is the share of runs that passed, from 0 to 1.
func rate(t Tally) float64 {
	if t.Runs == 0 {
		return 0
	}
	return float64(t.Passed) / float64(t.Runs)
}

// spread is how far apart a case's best and worst pass rates are.
func spread(c CaseRow) float64 {
	lo, hi := 1.0, 0.0
	for _, t := range c.Rates {
		lo, hi = min(lo, rate(t)), max(hi, rate(t))
	}
	return max(hi-lo, 0)
}

// RenderComparison is a Comparison as Markdown: the summary table, the chart
// of pass rate against cost, and the grid of cases.
func RenderComparison(c Comparison) string {
	var b strings.Builder
	b.WriteString("# Translator eval: comparison\n\n")
	for _, r := range c.Rows {
		fmt.Fprintf(&b, "- %s: run %s", r.Name, r.Report.Started.Format("2006-01-02 15:04 MST"))
		if c.PromptsDiffer {
			fmt.Fprintf(&b, " · prompts %s", promptList(r.Report.Prompts))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	if c.PromptsDiffer {
		b.WriteString("**The reports were asked under different prompts**, so a difference may be the prompt's, not the model's.\n\n")
	} else if len(c.Rows) > 0 {
		fmt.Fprintf(&b, "Every report was asked under the same prompts: %s.\n\n", promptList(c.Rows[0].Report.Prompts))
	}
	fmt.Fprintf(&b, "Compared on the %d cases every report ran.", len(c.Cases))
	for _, r := range c.Rows {
		if len(r.Only) > 0 {
			fmt.Fprintf(&b, " Only %s ran %s.", r.Name, strings.Join(r.Only, ", "))
		}
	}
	b.WriteString("\n\n")

	b.WriteString("## Summary\n\n")
	b.WriteString("First call and in the end are the first-attempt cases, as in each report; repairs are apart. ")
	b.WriteString("Translations and issues split the first-attempt runs by what the case wants. ")
	b.WriteString("Made up and missed are what the judge found. ★ is a model no other beats on both pass rate and cost.\n\n")
	b.WriteString("| | First call | In the end | Repairs fixed | Translations | Issues | Made up | Missed | A translation | Typical | Slow (p95) | Over " + patterntranslator.Timeout.String() + " |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range c.Rows {
		s := r.Summary
		name := r.Name
		if r.Best {
			name += " ★"
		}
		judged := func(n int) string {
			if s.Judged.Replies == 0 {
				return "—"
			}
			return fmt.Sprint(n)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %d |\n", name,
			s.All.FirstCall(), s.All, s.Repairs.Rate(), r.Translations, r.Issues,
			judged(s.Judged.MadeUp), judged(s.Judged.Missed), dollars(r.Report.Priced, s.PerRun()),
			seconds(s.Typical), seconds(s.Slow), s.TimedOut)
	}
	b.WriteString("\n")

	writeChart(&b, c)

	b.WriteString("## Cases\n\nIn the end, or fixed for a repair case; the cases the models disagree on most come first.\n\n")
	b.WriteString("| Case |")
	for _, r := range c.Rows {
		fmt.Fprintf(&b, " %s |", r.Name)
	}
	b.WriteString("\n|---|" + strings.Repeat("---|", len(c.Rows)) + "\n")
	for _, cr := range c.Cases {
		fmt.Fprintf(&b, "| %s |", cr.Name)
		for _, t := range cr.Rates {
			fmt.Fprintf(&b, " %d/%d |", t.Passed, t.Runs)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// writeChart is pass rate against cost a translation. GitHub's Mermaid has no
// scatter plot, so it is a quadrant chart, with cost scaled to the dearest
// model; a model without a price has no place on it.
func writeChart(b *strings.Builder, c Comparison) {
	dearest := 0.0
	var priced []ComparisonRow
	for _, r := range c.Rows {
		if r.Report.Priced {
			priced = append(priced, r)
			dearest = max(dearest, r.Summary.PerRun())
		}
	}
	if len(priced) == 0 || dearest == 0 {
		return
	}
	b.WriteString("## Pass rate against cost\n\n")
	fmt.Fprintf(b, "Pass rate in the end, against cost a translation as a share of the dearest, %s.\n\n", dollars(true, dearest))
	b.WriteString("```mermaid\nquadrantChart\n    x-axis Cheaper --> Dearer\n    y-axis Fails more --> Passes more\n")
	for _, r := range priced {
		label := r.Report.Model
		if r.Report.Effort != "" {
			label += " " + r.Report.Effort
		}
		if r.Best {
			label += " ★"
		}
		fmt.Fprintf(b, "    %s: [%.2f, %.2f]\n", label, r.Summary.PerRun()/dearest, rate(r.Summary.All))
	}
	b.WriteString("```\n\n")
}

// ComparisonFileName is where a comparison made on day is written, without
// its extension.
func ComparisonFileName(day time.Time) string {
	return day.Format(time.DateOnly) + "-comparison"
}
