package translatoreval

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/core/patterntranslator"
)

// Tally is how many runs passed of how many, and how many of those passed on
// their first call. A pass that needed another call still passes, but the
// model's first reply did not stand.
type Tally struct {
	Passed, First, Runs int
}

func (t Tally) String() string {
	if t.Runs == 0 {
		return "0/0"
	}
	return fmt.Sprintf("%d/%d (%.0f%%)", t.Passed, t.Runs, 100*float64(t.Passed)/float64(t.Runs))
}

// FirstCall is how many runs passed on their first call, of how many.
func (t Tally) FirstCall() Tally {
	return Tally{Passed: t.First, First: t.First, Runs: t.Runs}
}

func (t *Tally) add(r RunReport) {
	t.Runs++
	if r.Pass {
		t.Passed++
		if r.Calls <= 1 { // the stub makes no calls at all
			t.First++
		}
	}
}

// Retries is every call after a run's first, by why it was made: the parser
// refused the reply before, or the reply could not be used at all.
type Retries struct {
	Refused int
	// Unusable counts unusable replies by what made them so.
	Unusable map[string]int
}

// add counts the calls a run made again, after its first skip replies.
func (rt *Retries) add(kind string, run RunReport, skip int) {
	for _, reply := range run.Replies[min(skip, len(run.Replies)):] {
		switch reply.Retry {
		case Refused:
			rt.Refused++
		case Unusable:
			if rt.Unusable == nil {
				rt.Unusable = map[string]int{}
			}
			rt.Unusable[whyUnusable(kind, reply)]++
		}
	}
}

// whyUnusable is what made a reply one the translator could not use.
func whyUnusable(kind string, r Reply) string {
	if r.Error != "" {
		return strings.TrimPrefix(r.Error, patterntranslator.ErrUnusableReply.Error()+": ")
	}
	if strings.TrimSpace(r.Text) == "" {
		return "an empty reply"
	}
	var reply struct {
		Status  string            `json:"status"`
		Issues  []json.RawMessage `json:"issues"`
		Pattern string            `json:"pattern"`
		Rule    string            `json:"rule"`
	}
	if err := json.Unmarshal([]byte(r.Text), &reply); err != nil {
		return "JSON that does not read"
	}
	answer := reply.Pattern
	if kind == Rules {
		answer = reply.Rule
	}
	switch {
	case reply.Status == "issues" && len(reply.Issues) == 0:
		return "issues with none in them"
	case reply.Status == "ok" && strings.TrimSpace(answer) == "":
		return "an empty answer"
	}
	return "a reply of neither shape"
}

// Score is a mean score from 0 to 1 over the runs that have one.
type Score struct {
	Sum  float64
	Runs int
}

func (s Score) String() string {
	if s.Runs == 0 {
		return "—"
	}
	return fmt.Sprintf("%.2f", s.Mean())
}

// Mean is the mean score, or 0 with none.
func (s Score) Mean() float64 {
	if s.Runs == 0 {
		return 0
	}
	return s.Sum / float64(s.Runs)
}

func (s *Score) add(r RunReport) {
	if r.Wording != nil {
		s.Sum += *r.Wording
		s.Runs++
	}
}

// Group is the cases of one group in one file, taken together.
type Group struct {
	Label string
	Tally
	Wording Score
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

// Repairs is how repair cases' runs ended: fixed, the reply parsed and means
// what the description says; changed, it parsed (or raised issues) but means
// something else; not fixed, every call was refused.
type Repairs struct {
	Fixed, Changed, NotFixed, Unavailable int
	// FirstFix is the fixes made on the model's first call, the one after
	// the planted reply.
	FirstFix int
}

func (r *Repairs) add(run RunReport) {
	switch {
	case run.Outcome == Unavailable:
		r.Unavailable++
	case run.Outcome == GaveUp:
		r.NotFixed++
	case run.Pass:
		r.Fixed++
		if run.Calls <= 2 { // the stub makes no calls at all
			r.FirstFix++
		}
	default:
		r.Changed++
	}
}

// Rate is how many runs were fixed of how many; its first call is the
// model's, after the planted one.
func (r Repairs) Rate() Tally {
	return Tally{Passed: r.Fixed, First: r.FirstFix, Runs: r.Fixed + r.Changed + r.NotFixed + r.Unavailable}
}

// RepairGroup is the repair cases of one group in one file, taken together.
type RepairGroup struct {
	Label string
	Repairs
}

// Summary is a set of runs taken together.
type Summary struct {
	All Tally
	// Wording is the mean wording score of the runs that raised issues.
	Wording Score
	Groups  []Group
	Calls   Calls
	// Cost is the whole cost, in US dollars.
	Cost float64
	// Typical and Slow are the median and 95th-percentile latency.
	Typical, Slow time.Duration
	// TimedOut is how many ran out of patterntranslator.Timeout.
	TimedOut int
	// Repairs is the repair cases' runs, which All, Wording, Groups and Calls
	// leave out: a repair starts from a refused reply, so it is not a first
	// attempt. Cost and latency count every run.
	Repairs      Repairs
	RepairGroups []RepairGroup
	// Retries is the calls made again on first attempts; RepairRetries is
	// those on repairs, the planted reply's refusal aside.
	Retries, RepairRetries Retries
}

// PerRun is the mean cost of one translation, repairs included.
func (s Summary) PerRun() float64 {
	runs := s.All.Runs + s.Repairs.Rate().Runs
	if runs == 0 {
		return 0
	}
	return s.Cost / float64(runs)
}

// Summarise takes cases' runs together.
func Summarise(cases []CaseReport) Summary {
	var s Summary
	groups := map[string]*Group{}
	repairs := map[string]*RepairGroup{}
	var order, repairOrder []string
	var latencies []time.Duration
	for _, c := range cases {
		label := groupLabel(c)
		if c.Planted != "" {
			if repairs[label] == nil {
				repairs[label] = &RepairGroup{Label: label}
				repairOrder = append(repairOrder, label)
			}
			for _, r := range c.Runs {
				s.Repairs.add(r)
				repairs[label].add(r)
				s.RepairRetries.add(c.Kind, r, 1)
				s.Cost += r.Cost
				latencies = append(latencies, r.Latency())
				if r.TimedOut {
					s.TimedOut++
				}
			}
			continue
		}
		if groups[label] == nil {
			groups[label] = &Group{Label: label}
			order = append(order, label)
		}
		g := groups[label]
		for _, r := range c.Runs {
			s.All.add(r)
			s.Wording.add(r)
			g.Tally.add(r)
			g.Wording.add(r)
			s.Calls.add(r)
			s.Retries.add(c.Kind, r, 0)
			s.Cost += r.Cost
			latencies = append(latencies, r.Latency())
			if r.TimedOut {
				s.TimedOut++
			}
		}
	}
	for _, label := range order {
		s.Groups = append(s.Groups, *groups[label])
	}
	for _, label := range repairOrder {
		s.RepairGroups = append(s.RepairGroups, *repairs[label])
	}
	s.Typical, s.Slow = percentile(latencies, 0.5), percentile(latencies, 0.95)
	return s
}

// groupLabel names a case's group with its kind, since each file has groups
// of its own.
func groupLabel(c CaseReport) string {
	kind := "Patterns"
	if c.Kind == Rules {
		kind = "Rules"
	}
	if c.Group == "" {
		return kind
	}
	return kind + ": " + c.Group
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
	b.WriteString("## Summary\n\nWording scores the messages of issues the model raised, from 0 to 1. It never fails a run.\n\n")
	b.WriteString("First call is the runs right on the model's first reply; in the end, those right on any. ")
	b.WriteString("Every call after the first means a reply did not stand.\n\n")
	b.WriteString("| | First call | In the end | Wording |\n|---|---|---|---|\n")
	fmt.Fprintf(&b, "| All | %s | %s | %s |\n", s.All.FirstCall(), s.All, s.Wording)
	for _, g := range s.Groups {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", g.Label, g.FirstCall(), g.Tally, g.Wording)
	}
	b.WriteString("\n| Calls | Runs |\n|---|---|\n")
	fmt.Fprintf(&b, "| First call | %d |\n| Second call | %d |\n| Third call | %d |\n| Gave up | %d |\n| Unavailable | %d |\n\n",
		s.Calls.First, s.Calls.Second, s.Calls.Third, s.Calls.GaveUp, s.Calls.Unavailable)
	writeRetries(&b, s.Retries)
	fmt.Fprintf(&b, "Cost %s in all, %s a translation · latency %s typical, %s slow (p95) · %d over the %s limit\n\n",
		money(s.Cost), money(s.PerRun()), seconds(s.Typical), seconds(s.Slow), s.TimedOut, patterntranslator.Timeout)

	if len(s.RepairGroups) > 0 {
		b.WriteString("## Repairs\n\nEach repair case's first call is a planted reply the parser refuses, made without asking the model; ")
		b.WriteString("what is graded is the model's replies after the parser's messages go back. ")
		b.WriteString("They are left out of the rates and calls above.\n\n")
		b.WriteString("| | Fixed | On the first model call | Parsed but changed | Not fixed | Unavailable |\n|---|---|---|---|---|---|\n")
		row := func(label string, r Repairs) {
			fmt.Fprintf(&b, "| %s | %s | %s | %d | %d | %d |\n", label, r.Rate(), r.Rate().FirstCall(), r.Changed, r.NotFixed, r.Unavailable)
		}
		row("All", s.Repairs)
		for _, g := range s.RepairGroups {
			row(g.Label, g.Repairs)
		}
		b.WriteString("\n")
		writeRetries(&b, s.RepairRetries)
	}

	b.WriteString("## Cases\n\nCalls are first / second / third / none that worked (gave up or unavailable).\n")
	label := ""
	for _, c := range r.Cases {
		if l := groupLabel(c); l != label {
			label = l
			fmt.Fprintf(&b, "\n### %s\n\n", label)
			b.WriteString("| Case | Wants | First call | In the end | Wording | Calls | Cost a run | Typical | Slow |\n|---|---|---|---|---|---|---|---|---|\n")
		}
		cs := Summarise([]CaseReport{c})
		wants := "issues"
		if c.Translates {
			wants = "translation"
		}
		// The summary leaves a repair case's runs out of its pass rate and
		// calls, so its own row counts them here: its fixes, and the loop's
		// calls, the planted one first.
		passed, calls := caseRate(c), cs.Calls
		if c.Planted != "" {
			calls = Calls{}
			for _, run := range c.Runs {
				calls.add(run)
			}
		}
		fmt.Fprintf(&b, "| %s | %s | %d/%d | %d/%d | %s | %d / %d / %d / %d | %s | %s | %s |\n",
			c.Name, wants, passed.First, passed.Runs, passed.Passed, passed.Runs, cs.Wording,
			calls.First, calls.Second, calls.Third, calls.GaveUp+calls.Unavailable,
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
	writeSecondGoes(&b, r.Cases)

	writeWording(&b, r.Cases)

	if earlier != nil {
		writeComparison(&b, r, earlier)
	}
	return b.String()
}

// writeWording lists every message that lost wording points, case by case,
// whether or not its run passed.
func writeWording(b *strings.Builder, cases []CaseReport) {
	wrote := false
	for _, c := range cases {
		var notes []string
		for _, run := range c.Runs {
			for _, n := range run.WordingNotes {
				if !slices.Contains(notes, n) {
					notes = append(notes, n)
				}
			}
		}
		if len(notes) == 0 {
			continue
		}
		if !wrote {
			fmt.Fprintf(b, "\n## Wording\n\nMessages over %d words or naming the format.\n", maxWords)
			wrote = true
		}
		fmt.Fprintf(b, "\n### %s\n\n", c.Name)
		for _, n := range notes {
			fmt.Fprintf(b, "- %s\n", n)
		}
	}
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
		// A run that gave up says why only in its replies; one that took
		// another call says how it got to its answer.
		if run.Outcome == GaveUp || run.Calls > firstCall(c) {
			writeCalls(b, c, run)
		}
	}
}

// firstCall is the call a case's first reply of the model's own comes back
// on: a repair case's first is planted.
func firstCall(c CaseReport) int {
	if c.Planted != "" {
		return 2
	}
	return 1
}

// writeSecondGoes lists every run that passed, but not on the model's first
// reply.
func writeSecondGoes(b *strings.Builder, cases []CaseReport) {
	wrote := false
	for _, c := range cases {
		var runs []int
		for i, run := range c.Runs {
			if run.Pass && run.Calls > firstCall(c) {
				runs = append(runs, i)
			}
		}
		if len(runs) == 0 {
			continue
		}
		if !wrote {
			b.WriteString("\n## Second goes\n\nRuns that passed, but not on the model's first reply.\n")
			wrote = true
		}
		fmt.Fprintf(b, "\n### %s\n\n> %s\n\n", c.Name, strings.ReplaceAll(c.Text, "\n", "\n> "))
		for _, i := range runs {
			fmt.Fprintf(b, "- Run %d: passed on call %d\n", i+1, c.Runs[i].Calls)
			writeCalls(b, c, c.Runs[i])
		}
	}
}

// writeCalls is each of a run's calls: the reply, the thinking behind it, and
// why the model was asked again after it.
func writeCalls(b *strings.Builder, c CaseReport, run RunReport) {
	for n, reply := range run.Replies {
		planted := ""
		if c.Planted != "" && n == 0 {
			planted = " (planted)"
		}
		fmt.Fprintf(b, "  - Call %d%s: %s\n", n+1, planted, replyLine(reply))
		if thought := strings.Join(strings.Fields(reply.Thinking), " "); thought != "" {
			fmt.Fprintf(b, "    - Thought: %s\n", thought)
		}
		switch reply.Retry {
		case Refused:
			fmt.Fprintf(b, "    - Asked again: the parser refused it: %s\n", sentBackProblems(reply.SentBack))
		case Unusable:
			fmt.Fprintf(b, "    - Asked again: unusable, %s\n", whyUnusable(c.Kind, reply))
		}
	}
}

// sentBackProblems is the parser's problems a message took back to the model,
// on one line.
func sentBackProblems(text string) string {
	var problems []string
	for _, line := range strings.Split(text, "\n") {
		if p, ok := strings.CutPrefix(line, "- "); ok {
			problems = append(problems, p)
		}
	}
	if len(problems) == 0 {
		return strings.Join(strings.Fields(text), " ")
	}
	return strings.Join(problems, "; ")
}

// writeRetries is every call made again, by why, most often first.
func writeRetries(b *strings.Builder, rt Retries) {
	if rt.Refused == 0 && len(rt.Unusable) == 0 {
		return
	}
	b.WriteString("| Why the model was asked again | Calls |\n|---|---|\n")
	if rt.Refused > 0 {
		fmt.Fprintf(b, "| Refused by the parser | %d |\n", rt.Refused)
	}
	whys := slices.Collect(maps.Keys(rt.Unusable))
	sort.Slice(whys, func(i, j int) bool {
		if rt.Unusable[whys[i]] != rt.Unusable[whys[j]] {
			return rt.Unusable[whys[i]] > rt.Unusable[whys[j]]
		}
		return whys[i] < whys[j]
	})
	for _, why := range whys {
		fmt.Fprintf(b, "| Unusable: %s | %d |\n", why, rt.Unusable[why])
	}
	b.WriteString("\n")
}

// replyLine is a reply on one line: its text as code, or why it had none.
func replyLine(r Reply) string {
	if r.Error != "" {
		return "no reply: " + r.Error
	}
	text := strings.Join(strings.Fields(r.Text), " ")
	if text == "" {
		return "an empty reply"
	}
	return "`` " + strings.ReplaceAll(text, "`", "'") + " ``"
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
	fmt.Fprintf(b, "On the cases both ran: first call %s → %s, in the end %s → %s, wording %s → %s, %s a translation → %s, typical %s → %s, slow %s → %s\n\n",
		sb.All.FirstCall(), sn.All.FirstCall(), sb.All, sn.All, sb.Wording, sn.Wording, dollars(earlier.Priced, sb.PerRun()), dollars(r.Priced, sn.PerRun()),
		seconds(sb.Typical), seconds(sn.Typical), seconds(sb.Slow), seconds(sn.Slow))

	if sb.Repairs.Rate().Runs > 0 && sn.Repairs.Rate().Runs > 0 {
		fmt.Fprintf(b, "Repairs on the cases both ran: fixed %s → %s, on the first model call %s → %s\n\n",
			sb.Repairs.Rate(), sn.Repairs.Rate(), sb.Repairs.Rate().FirstCall(), sn.Repairs.Rate().FirstCall())
	}

	b.WriteString("| Case | First call | Change | In the end | Change |\n|---|---|---|---|---|\n")
	for _, c := range shared {
		was, is := caseRate(before[c.Name]), caseRate(c)
		fmt.Fprintf(b, "| %s | %d/%d → %d/%d | %s | %d/%d → %d/%d | %s |\n", c.Name,
			was.First, was.Runs, is.First, is.Runs, change(was.FirstCall(), is.FirstCall()),
			was.Passed, was.Runs, is.Passed, is.Runs, change(was, is))
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

// caseRate is how many of a case's runs passed, or for a repair case, were
// fixed.
func caseRate(c CaseReport) Tally {
	s := Summarise([]CaseReport{c})
	if c.Planted != "" {
		return s.Repairs.Rate()
	}
	return s.All
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
	e := Estimate{}
	_, e.Priced = prices[model]
	for _, c := range cases {
		// A repair's planted call is free, so it can ask one call fewer, and
		// each is asked with the planted reply and the parser's messages too.
		planted, extra := 0, 0
		if c.Repairs() {
			planted, extra = 1, 2*len(c.Planted)+300
		}
		e.Calls += repeats
		e.MaxCalls += (patterntranslator.MaxCalls - planted) * repeats
		chars := system["pattern"] + len(c.Text) + 200 + extra
		if c.Kind == Rules {
			chars = system["rule"] + len(c.Text) + 200 + extra + len(strings.Join(c.Descriptions(), "\n"))
		}
		// About four characters a token.
		u := patterntranslator.Usage{InputTokens: int64(chars / 4), OutputTokens: replyGuess}
		e.Cost += cost(model, u) * float64(repeats)
	}
	return e
}
