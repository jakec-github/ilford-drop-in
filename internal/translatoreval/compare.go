package translatoreval

import (
	"cmp"
	_ "embed"
	"fmt"
	"html/template"
	"maps"
	"math"
	"slices"
	"strconv"
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

//go:embed comparison.html
var comparisonHTML string

var comparisonPage = template.Must(template.New("comparison").Parse(comparisonHTML))

// RenderComparison is a Comparison as a page of HTML that needs nothing but
// itself: the summary table, the chart of pass rate against cost, and the
// grid of cases shaded by pass rate.
func RenderComparison(c Comparison) string {
	page := comparisonView{Comparison: c, Timeout: patterntranslator.Timeout.String(), Chart: plot(c)}
	if len(c.Rows) > 0 {
		page.Prompts = promptList(c.Rows[0].Report.Prompts)
	}
	for _, r := range c.Rows {
		s := r.Summary
		judged := func(n int) string {
			if s.Judged.Replies == 0 {
				return "—"
			}
			return fmt.Sprint(n)
		}
		page.Summary = append(page.Summary, summaryRow{
			Name: r.Name, Best: r.Best,
			Run:     r.Report.Started.Format("2006-01-02 15:04 MST"),
			Prompts: promptList(r.Report.Prompts),
			Only:    strings.Join(r.Only, ", "),
			Cells: []string{
				s.All.FirstCall().String(), s.All.String(), s.Repairs.Rate().String(),
				r.Translations.String(), r.Issues.String(),
				judged(s.Judged.MadeUp), judged(s.Judged.Missed), dollars(r.Report.Priced, s.PerRun()),
				seconds(s.Typical), seconds(s.Slow), fmt.Sprint(s.TimedOut),
			},
		})
	}
	for _, cr := range c.Cases {
		row := caseView{Name: cr.Name}
		for _, t := range cr.Rates {
			row.Cells = append(row.Cells, caseCell{Text: fmt.Sprintf("%d/%d", t.Passed, t.Runs), Rate: rate(t), Ran: t.Runs > 0})
		}
		page.Cases = append(page.Cases, row)
	}
	var b strings.Builder
	if err := comparisonPage.Execute(&b, page); err != nil {
		// The template and the view are both fixed here, so this is a bug.
		panic(err)
	}
	return b.String()
}

// comparisonView is what comparison.html is drawn from.
type comparisonView struct {
	Comparison
	Prompts, Timeout string
	Summary          []summaryRow
	Cases            []caseView
	Chart            *chart
}

type summaryRow struct {
	Name, Run, Prompts, Only string
	Best                     bool
	Cells                    []string
}

type caseView struct {
	Name  string
	Cells []caseCell
}

type caseCell struct {
	Text string
	Rate float64
	Ran  bool
}

// chart is pass rate in the end against cost a translation, in the SVG's own
// units. Cost is on a log scale, since a tier up can cost ten times as much.
type chart struct {
	Width, Height            float64
	Left, Right, Top, Bottom float64
	Points                   []point
	// Frontier is the polyline points of a step through the models no other
	// beats: along at each one's pass rate to the next one's cost, then up.
	Frontier       string
	XTicks, YTicks []tick
}

type point struct {
	// Name is the row's; Label is shorter, for beside the point.
	Name, Label string
	X, Y        float64
	Best        bool
	// Title is the point's figures, shown on hovering over it.
	Title string
	// LabelLeft puts the label on the left, for a point near the right edge.
	LabelLeft bool
}

type tick struct {
	At    float64
	Label string
}

// plot is the chart of c's priced models, or nil where none has a cost.
func plot(c Comparison) *chart {
	var rows []ComparisonRow
	for _, r := range c.Rows {
		if r.Report.Priced && r.Summary.PerRun() > 0 {
			rows = append(rows, r)
		}
	}
	if len(rows) == 0 {
		return nil
	}
	ch := &chart{Width: 760, Height: 440, Left: 72, Right: 736, Top: 24, Bottom: 384}

	cheapest, dearest := math.Inf(1), 0.0
	lowest := 1.0
	for _, r := range rows {
		cheapest, dearest = min(cheapest, r.Summary.PerRun()), max(dearest, r.Summary.PerRun())
		lowest = min(lowest, rate(r.Summary.All))
	}
	xlo, xhi := math.Log10(cheapest), math.Log10(dearest)
	if pad := (xhi - xlo) / 10; pad > 0 {
		xlo, xhi = xlo-pad, xhi+pad
	} else {
		xlo, xhi = xlo-0.5, xhi+0.5
	}
	x := func(cost float64) float64 {
		return tenth(ch.Left + (math.Log10(cost)-xlo)/(xhi-xlo)*(ch.Right-ch.Left))
	}
	// The pass rate axis starts a step below the worst model, so the models
	// are not squeezed into the top of it.
	ylo := max(0, math.Ceil(lowest*10-1e-9)/10-0.1)
	y := func(r float64) float64 {
		return tenth(ch.Bottom - (r-ylo)/(1-ylo)*(ch.Bottom-ch.Top))
	}

	for k := math.Floor(xlo); k <= math.Ceil(xhi); k++ {
		for _, m := range []int{1, 2, 5} {
			v, _ := strconv.ParseFloat(fmt.Sprintf("%de%d", m, int(k)), 64)
			if l := math.Log10(v); l >= xlo && l <= xhi {
				ch.XTicks = append(ch.XTicks, tick{At: x(v), Label: "$" + strconv.FormatFloat(v, 'f', -1, 64)})
			}
		}
	}
	step := 0.1
	if 1-ylo <= 0.3 {
		step = 0.05
	}
	for i := 0; ; i++ {
		r := ylo + float64(i)*step
		if r > 1+1e-9 {
			break
		}
		ch.YTicks = append(ch.YTicks, tick{At: y(r), Label: fmt.Sprintf("%.0f%%", 100*r)})
	}

	var best []point
	for _, r := range rows {
		label := r.Report.Model
		if r.Report.Effort != "" {
			label += " " + r.Report.Effort
		}
		p := point{
			Name: r.Name, Label: label, Best: r.Best,
			X: x(r.Summary.PerRun()), Y: y(rate(r.Summary.All)),
			Title: fmt.Sprintf("%s: %s in the end, %s a translation", r.Name, r.Summary.All, dollars(true, r.Summary.PerRun())),
		}
		p.LabelLeft = p.X > ch.Left+(ch.Right-ch.Left)*2/3
		ch.Points = append(ch.Points, p)
		if p.Best {
			best = append(best, p)
		}
	}
	slices.SortFunc(best, func(a, b point) int { return cmp.Compare(a.X, b.X) })
	var steps []string
	for i, p := range best {
		if i > 0 {
			steps = append(steps, fmt.Sprintf("%.1f,%.1f", p.X, best[i-1].Y))
		}
		steps = append(steps, fmt.Sprintf("%.1f,%.1f", p.X, p.Y))
	}
	ch.Frontier = strings.Join(steps, " ")
	return ch
}

// tenth is v to a tenth of the SVG's unit, finer than a screen shows.
func tenth(v float64) float64 {
	return math.Round(v*10) / 10
}

// ComparisonFileName is where a comparison made on day is written, without
// its extension.
func ComparisonFileName(day time.Time) string {
	return day.Format(time.DateOnly) + "-comparison"
}
