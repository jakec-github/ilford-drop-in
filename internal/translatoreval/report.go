package translatoreval

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/core/patterntranslator"
)

// Tally is how many runs passed of how many.
type Tally struct {
	Passed, Runs int
}

func (t Tally) String() string {
	if t.Runs == 0 {
		return "0/0"
	}
	return fmt.Sprintf("%d/%d (%.0f%%)", t.Passed, t.Runs, 100*float64(t.Passed)/float64(t.Runs))
}

func (t *Tally) add(r RunReport) {
	t.Runs++
	if r.Pass {
		t.Passed++
	}
}

// Group is the tally of one kind of case: a pattern or a rule, wanting a
// translation or issues.
type Group struct {
	Label string
	Tally
}

// Calls is how many calls translations took: one, or a second or third after
// the parser refused an answer, or every call refused (gave up), or none that
// answered (unavailable).
type Calls struct {
	First, Second, Third, GaveUp, Unavailable int
}

func (c *Calls) add(r RunReport) {
	switch {
	case r.Outcome == GaveUp:
		c.GaveUp++
	case r.Outcome == Unavailable:
		c.Unavailable++
	case r.Calls <= 1:
		c.First++
	case r.Calls == 2:
		c.Second++
	default:
		c.Third++
	}
}

// Summary is a set of runs taken together.
type Summary struct {
	All    Tally
	Groups []Group
	Calls  Calls
	// Cost is the whole cost, in US dollars.
	Cost float64
	// Typical and Slow are the median and 95th-percentile latency.
	Typical, Slow time.Duration
	// TimedOut is how many ran out of patterntranslator.Timeout.
	TimedOut int
}

// PerRun is the mean cost of one translation.
func (s Summary) PerRun() float64 {
	if s.All.Runs == 0 {
		return 0
	}
	return s.Cost / float64(s.All.Runs)
}

// Summarise takes cases' runs together.
func Summarise(cases []CaseReport) Summary {
	var s Summary
	groups := map[string]*Tally{}
	var order []string
	var latencies []time.Duration
	for _, c := range cases {
		label := groupLabel(c)
		if groups[label] == nil {
			groups[label] = &Tally{}
			order = append(order, label)
		}
		for _, r := range c.Runs {
			s.All.add(r)
			groups[label].add(r)
			s.Calls.add(r)
			s.Cost += r.Cost
			latencies = append(latencies, r.Latency())
			if r.TimedOut {
				s.TimedOut++
			}
		}
	}
	sort.Strings(order)
	for _, label := range order {
		s.Groups = append(s.Groups, Group{Label: label, Tally: *groups[label]})
	}
	s.Typical, s.Slow = percentile(latencies, 0.5), percentile(latencies, 0.95)
	return s
}

func groupLabel(c CaseReport) string {
	kind := "Patterns"
	if c.Kind == Rules {
		kind = "Rules"
	}
	if c.Translates {
		return kind + ", translations"
	}
	return kind + ", issues"
}

// percentile is the nearest-rank percentile p of ds.
func percentile(ds []time.Duration, p float64) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	sorted := slices.Clone(ds)
	slices.Sort(sorted)
	i := int(math.Ceil(p*float64(len(sorted)))) - 1
	return sorted[max(i, 0)]
}

// Render is a report as Markdown, to be read: the summary, a line per case,
// and what went wrong in every run that failed. Against an earlier report it
// also says what changed.
func Render(r *Report, earlier *Report) string {
	var b strings.Builder
	money := func(v float64) string {
		if !r.Priced {
			return "—"
		}
		return fmt.Sprintf("$%.4f", v)
	}

	fmt.Fprintf(&b, "# Translator eval: %s\n\n", subjectName(r))
	fmt.Fprintf(&b, "Run %s · %d repeats · prompts %s\n\n", r.Started.Format("2006-01-02 15:04 MST"), r.Repeats, promptList(r.Prompts))

	s := Summarise(r.Cases)
	b.WriteString("## Summary\n\n| | Passed |\n|---|---|\n")
	fmt.Fprintf(&b, "| All | %s |\n", s.All)
	for _, g := range s.Groups {
		fmt.Fprintf(&b, "| %s | %s |\n", g.Label, g.Tally)
	}
	b.WriteString("\n| Calls | Runs |\n|---|---|\n")
	fmt.Fprintf(&b, "| First call | %d |\n| Second call | %d |\n| Third call | %d |\n| Gave up | %d |\n| Unavailable | %d |\n\n",
		s.Calls.First, s.Calls.Second, s.Calls.Third, s.Calls.GaveUp, s.Calls.Unavailable)
	fmt.Fprintf(&b, "Cost %s in all, %s a translation · latency %s typical, %s slow (p95) · %d over the %s limit\n\n",
		money(s.Cost), money(s.PerRun()), seconds(s.Typical), seconds(s.Slow), s.TimedOut, patterntranslator.Timeout)

	b.WriteString("## Cases\n\nCalls are first / second / third / none that worked (gave up or unavailable).\n\n")
	b.WriteString("| Case | Kind | Wants | Passed | Calls | Cost a run | Typical | Slow |\n|---|---|---|---|---|---|---|---|\n")
	for _, c := range r.Cases {
		cs := Summarise([]CaseReport{c})
		wants := "issues"
		if c.Translates {
			wants = "translation"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %d/%d | %d / %d / %d / %d | %s | %s | %s |\n",
			c.Name, c.Kind, wants, cs.All.Passed, cs.All.Runs,
			cs.Calls.First, cs.Calls.Second, cs.Calls.Third, cs.Calls.GaveUp+cs.Calls.Unavailable,
			money(cs.PerRun()), seconds(cs.Typical), seconds(cs.Slow))
	}

	var failed []CaseReport
	for _, c := range r.Cases {
		if slices.ContainsFunc(c.Runs, func(run RunReport) bool { return !run.Pass }) {
			failed = append(failed, c)
		}
	}
	if len(failed) > 0 {
		b.WriteString("\n## Failures\n")
		for _, c := range failed {
			writeFailures(&b, c)
		}
	}

	if earlier != nil {
		writeComparison(&b, r, earlier)
	}
	return b.String()
}

func writeFailures(b *strings.Builder, c CaseReport) {
	fmt.Fprintf(b, "\n### %s\n\n> %s\n\n", c.Name, strings.ReplaceAll(c.Text, "\n", "\n> "))
	for i, run := range c.Runs {
		if run.Pass {
			continue
		}
		fmt.Fprintf(b, "- Run %d: %s\n", i+1, strings.Join(run.Failures, "; "))
		for _, d := range run.Descriptions {
			fmt.Fprintf(b, "  - Gave: %s\n", d)
		}
		if run.Outcome == Raised {
			for _, issue := range run.Issues {
				fmt.Fprintf(b, "  - Raised %s: %s\n", issue.Kind, issue.Message)
			}
		}
		if d := run.Shifts; d != nil {
			if d.MissingCount > 0 {
				fmt.Fprintf(b, "  - Missing: %s%s\n", strings.Join(d.Missing, ", "), more(d.MissingCount, len(d.Missing)))
			}
			if d.ExtraCount > 0 {
				fmt.Fprintf(b, "  - Extra: %s%s\n", strings.Join(d.Extra, ", "), more(d.ExtraCount, len(d.Extra)))
			}
		}
	}
}

func more(count, shown int) string {
	if count > shown {
		return fmt.Sprintf(" and %d more", count-shown)
	}
	return ""
}

func writeComparison(b *strings.Builder, r, earlier *Report) {
	fmt.Fprintf(b, "\n## Against %s, %s\n\n", subjectName(earlier), earlier.Started.Format(time.DateOnly))
	var names []string
	for name := range r.Prompts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if before, ok := earlier.Prompts[name]; ok && before != r.Prompts[name] {
			fmt.Fprintf(b, "`%s` prompt differs: %s → %s\n\n", name, before, r.Prompts[name])
		}
	}

	before := map[string]CaseReport{}
	for _, c := range earlier.Cases {
		before[c.Name] = c
	}
	now := map[string]bool{}
	var shared []CaseReport
	var onlyNow, onlyBefore []string
	for _, c := range r.Cases {
		now[c.Name] = true
		if _, ok := before[c.Name]; ok {
			shared = append(shared, c)
		} else {
			onlyNow = append(onlyNow, c.Name)
		}
	}
	for _, c := range earlier.Cases {
		if !now[c.Name] {
			onlyBefore = append(onlyBefore, c.Name)
		}
	}

	var sharedBefore []CaseReport
	for _, c := range shared {
		sharedBefore = append(sharedBefore, before[c.Name])
	}
	sb, sn := Summarise(sharedBefore), Summarise(shared)
	fmt.Fprintf(b, "On the cases both ran: passed %s → %s, %s a translation → %s, typical %s → %s, slow %s → %s\n\n",
		sb.All, sn.All, dollars(earlier.Priced, sb.PerRun()), dollars(r.Priced, sn.PerRun()),
		seconds(sb.Typical), seconds(sn.Typical), seconds(sb.Slow), seconds(sn.Slow))

	b.WriteString("| Case | Before | Now | Change |\n|---|---|---|---|\n")
	for _, c := range shared {
		was, is := Summarise([]CaseReport{before[c.Name]}).All, Summarise([]CaseReport{c}).All
		fmt.Fprintf(b, "| %s | %d/%d | %d/%d | %s |\n", c.Name, was.Passed, was.Runs, is.Passed, is.Runs, change(was, is))
	}
	if len(onlyNow) > 0 {
		sort.Strings(onlyNow)
		fmt.Fprintf(b, "\nOnly in this run: %s\n", strings.Join(onlyNow, ", "))
	}
	if len(onlyBefore) > 0 {
		sort.Strings(onlyBefore)
		fmt.Fprintf(b, "\nOnly in the earlier run: %s\n", strings.Join(onlyBefore, ", "))
	}
}

// change is the change in pass rate, in percentage points.
func change(was, is Tally) string {
	rate := func(t Tally) float64 {
		if t.Runs == 0 {
			return 0
		}
		return 100 * float64(t.Passed) / float64(t.Runs)
	}
	d := math.Round(rate(is) - rate(was))
	switch {
	case d > 0:
		return fmt.Sprintf("+%.0f pts", d)
	case d < 0:
		return fmt.Sprintf("−%.0f pts", -d)
	}
	return "0"
}

func dollars(priced bool, v float64) string {
	if !priced {
		return "—"
	}
	return fmt.Sprintf("$%.4f", v)
}

func subjectName(r *Report) string {
	if r.Effort == "" {
		return r.Model
	}
	return fmt.Sprintf("%s, %s effort", r.Model, r.Effort)
}

func promptList(prompts map[string]string) string {
	var names []string
	for name := range prompts {
		names = append(names, name)
	}
	sort.Strings(names)
	var parts []string
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("`%s` %s", name, prompts[name]))
	}
	return strings.Join(parts, ", ")
}

func seconds(d time.Duration) string {
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// FileName is where a report is written, without its extension.
func FileName(r *Report) string {
	name := r.Started.Format(time.DateOnly) + "-" + r.Model
	if r.Effort != "" {
		name += "-" + r.Effort
	}
	return name
}

// ReadReport reads a report written as JSON.
func ReadReport(data []byte) (*Report, error) {
	var r Report
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Estimate is what a run will make and spend before it starts.
type Estimate struct {
	// Calls is one a translation; MaxCalls is if every answer were refused.
	Calls, MaxCalls int
	// Cost is for Calls, in US dollars: rough, from the prompts' length and a
	// guess at the reply.
	Cost   float64
	Priced bool
}

// replyGuess is the output tokens a call is taken to use, thinking included.
const replyGuess = 600

// EstimateRun is what translating cases repeats times on model will take.
func EstimateRun(model string, cases []Case, repeats int) Estimate {
	if model == "stub" {
		return Estimate{}
	}
	system := map[string]int{}
	for _, p := range patterntranslator.Prompts() {
		schema, _ := json.Marshal(p.Schema)
		system[p.Name] = len(p.System) + len(schema)
	}
	e := Estimate{Calls: len(cases) * repeats, MaxCalls: len(cases) * repeats * patterntranslator.MaxCalls}
	_, e.Priced = prices[model]
	for _, c := range cases {
		chars := system["pattern"] + len(c.Text) + 200
		if c.Kind == Rules {
			chars = system["rule"] + len(c.Text) + 200 + len(strings.Join(c.Descriptions(), "\n"))
		}
		// About four characters a token.
		u := patterntranslator.Usage{InputTokens: int64(chars / 4), OutputTokens: replyGuess}
		e.Cost += cost(model, u) * float64(repeats)
	}
	return e
}
