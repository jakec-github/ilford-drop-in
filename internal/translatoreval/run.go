package translatoreval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/core/patterntranslator"
	"github.com/jakechorley/ilford-drop-in/pkg/core/shiftpattern"
)

// Subject is what is being evaluated: a model at an effort.
type Subject struct {
	Model  string
	Effort string
	// Base asks the model. Nil runs the dev-mode stub instead, which costs
	// nothing and makes no calls.
	Base patterntranslator.Model
}

// Options are how a run goes.
type Options struct {
	// Repeats is how many times each case is translated: the model does not
	// give the same answer every time, and one run cannot tell a case that
	// always fails from one that fails now and then.
	Repeats int
	// Parallel is how many translations run at once.
	Parallel int
	// Progress, if set, is told each time a translation finishes.
	Progress func(done, total int)
}

// Report is a run: what was evaluated, and every case's runs.
type Report struct {
	Model   string    `json:"model"`
	Effort  string    `json:"effort"`
	Started time.Time `json:"started"`
	Repeats int       `json:"repeats"`
	// Prompts is a hash of each prompt the translators asked under, so two
	// reports say whether they ran against the same ones.
	Prompts map[string]string `json:"prompts"`
	// Priced is whether the model is in the price table, and so whether the
	// costs mean anything.
	Priced bool         `json:"priced"`
	Cases  []CaseReport `json:"cases"`
}

// CaseReport is one case's runs.
type CaseReport struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Group string `json:"group,omitempty"`
	Text  string `json:"text"`
	// Planted is, for a repair case, the refused reply it starts from.
	Planted string `json:"planted,omitempty"`
	// Translates is whether the case wants a translation rather than issues.
	Translates bool        `json:"translates"`
	Runs       []RunReport `json:"runs"`
}

// RunReport is one translation of a case: what came back, its grade, and what
// it took.
type RunReport struct {
	Answer
	Grade
	// Descriptions is the server's own reading of a translation, entry by
	// entry: what it means, as the Organiser would see it.
	Descriptions []string `json:"descriptions,omitempty"`
	Calls        int      `json:"calls"`
	// Replies is what each call came back with, as the model wrote it, so a
	// run that gave up says why when no reply reached the parser.
	Replies []Reply                 `json:"replies,omitempty"`
	Usage   patterntranslator.Usage `json:"usage"`
	// Cost is in US dollars, priced when it ran.
	Cost      float64 `json:"cost"`
	LatencyMS int64   `json:"latencyMs"`
	// TimedOut is a translation that ran out of patterntranslator.Timeout.
	TimedOut bool `json:"timedOut,omitempty"`
}

// Reply is one call's reply: its text, or why there was none to use, such as
// a refusal or a reply cut off at its token limit.
type Reply struct {
	Text  string `json:"text,omitempty"`
	Error string `json:"error,omitempty"`
}

// Latency is how long the translation took.
func (r RunReport) Latency() time.Duration {
	return time.Duration(r.LatencyMS) * time.Millisecond
}

// Run translates every case Repeats times and grades each answer. Each
// translation has the same ceiling the server gives a Generate.
func Run(ctx context.Context, s Subject, cases []Case, o Options) *Report {
	o.Repeats = max(o.Repeats, 1)
	o.Parallel = max(o.Parallel, 1)
	_, priced := prices[s.Model]
	report := &Report{
		Model:   s.Model,
		Effort:  s.Effort,
		Started: time.Now().UTC().Truncate(time.Second),
		Repeats: o.Repeats,
		Prompts: promptHashes(),
		Priced:  priced && s.Base != nil,
		Cases:   make([]CaseReport, len(cases)),
	}
	for i, c := range cases {
		report.Cases[i] = CaseReport{
			Name: c.Name, Kind: c.Kind, Group: c.Group, Text: c.Text, Planted: c.Planted, Translates: c.Translates(),
			Runs: make([]RunReport, o.Repeats),
		}
	}

	type job struct{ c, r int }
	jobs := make(chan job)
	var wg sync.WaitGroup
	var mu sync.Mutex
	done, total := 0, len(cases)*o.Repeats
	for range o.Parallel {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				run := translateOnce(ctx, s, cases[j.c])
				mu.Lock()
				report.Cases[j.c].Runs[j.r] = run
				done++
				if o.Progress != nil {
					o.Progress(done, total)
				}
				mu.Unlock()
			}
		}()
	}
	for c := range cases {
		for r := range o.Repeats {
			jobs <- job{c, r}
		}
	}
	close(jobs)
	wg.Wait()
	return report
}

// translateOnce is one Generate of a case, as the server would make it.
func translateOnce(ctx context.Context, s Subject, c Case) RunReport {
	var model patterntranslator.Model = s.Base
	if c.Repairs() {
		model = planter{model: s.Base, reply: plantedReply(c)}
	}
	rec := &recorder{model: model}
	var translator patterntranslator.Translator = patterntranslator.Stub{}
	if s.Base != nil {
		translator = patterntranslator.New(rec)
	}

	ctx, cancel := context.WithTimeout(ctx, patterntranslator.Timeout)
	defer cancel()
	loc, _ := time.LoadLocation(c.Zone)
	// Midday, so today is the case's date in its zone whatever the zone.
	now := time.Date(c.Today.Year(), c.Today.Month(), c.Today.Day(), 12, 0, 0, 0, loc)

	began := time.Now()
	var answer, gaveUp string
	var issues []patterntranslator.Issue
	var err error
	if c.Kind == Rules {
		var res patterntranslator.RuleResult
		res, err = translator.TranslateRule(ctx, patterntranslator.RuleRequest{
			Text: c.Text, Pattern: c.Descriptions(), Zone: c.Zone, Now: now,
		})
		answer, issues, gaveUp = res.Rule, res.Issues, patterntranslator.RuleGaveUp
	} else {
		var res patterntranslator.Result
		res, err = translator.Translate(ctx, patterntranslator.Request{Text: c.Text, Zone: c.Zone, Now: now})
		answer, issues, gaveUp = res.Pattern, res.Issues, patterntranslator.GaveUp
	}
	latency := time.Since(began)

	var a Answer
	switch {
	case err != nil:
		a = Answer{Outcome: Unavailable, Error: err.Error()}
	case answer != "":
		a = Answer{Outcome: Translated, Answer: answer}
	case len(issues) > 0 && issues[0].Message == gaveUp:
		a = Answer{Outcome: GaveUp, Issues: issues}
	default:
		a = Answer{Outcome: Raised, Issues: issues}
	}

	run := RunReport{
		Answer:       a,
		Grade:        grade(c, a),
		Descriptions: describe(c, a),
		Calls:        len(rec.replies),
		Replies:      rec.replies,
		Usage:        rec.usage,
		Cost:         cost(s.Model, rec.usage),
		LatencyMS:    latency.Milliseconds(),
		TimedOut:     errors.Is(ctx.Err(), context.DeadlineExceeded),
	}
	return run
}

// describe is the server's reading of a translation.
func describe(c Case, a Answer) []string {
	if a.Outcome != Translated {
		return nil
	}
	if c.Kind == Rules {
		line, _ := shiftpattern.ReadRule(a.Answer)
		return []string{line.Description}
	}
	reading, _ := shiftpattern.Read(a.Answer, c.Zone)
	var lines []string
	for _, e := range reading.Entries {
		lines = append(lines, e.Description)
	}
	return lines
}

// recorder is a Model that keeps each reply one translation's calls get, and
// sums what they use. Each translation has its own.
type recorder struct {
	model   patterntranslator.Model
	replies []Reply
	usage   patterntranslator.Usage
}

func (r *recorder) Reply(ctx context.Context, prompt *patterntranslator.Prompt, turns []patterntranslator.Turn) (string, patterntranslator.Usage, error) {
	text, u, err := r.model.Reply(ctx, prompt, turns)
	reply := Reply{Text: text}
	if err != nil {
		reply.Error = err.Error()
	}
	r.replies = append(r.replies, reply)
	r.usage.InputTokens += u.InputTokens
	r.usage.OutputTokens += u.OutputTokens
	r.usage.CacheReadTokens += u.CacheReadTokens
	r.usage.CacheWriteTokens += u.CacheWriteTokens
	return text, u, err
}

// planter is a Model whose first call is a planted reply, made without
// asking, at no cost. Every later call asks the model it wraps.
type planter struct {
	model patterntranslator.Model
	reply string
}

func (p planter) Reply(ctx context.Context, prompt *patterntranslator.Prompt, turns []patterntranslator.Turn) (string, patterntranslator.Usage, error) {
	if len(turns) == 1 {
		return p.reply, patterntranslator.Usage{}, nil
	}
	return p.model.Reply(ctx, prompt, turns)
}

// plantedReply is a repair case's planted answer as the model would have sent
// it.
func plantedReply(c Case) string {
	reply := struct {
		Status  string `json:"status"`
		Pattern string `json:"pattern,omitempty"`
		Rule    string `json:"rule,omitempty"`
	}{Status: "ok"}
	if c.Kind == Rules {
		reply.Rule = c.Planted
	} else {
		reply.Pattern = c.Planted
	}
	text, _ := json.Marshal(reply)
	return string(text)
}

// promptHashes is a short hash of each prompt, its system prompt and schema
// together.
func promptHashes() map[string]string {
	hashes := map[string]string{}
	for _, p := range patterntranslator.Prompts() {
		schema, _ := json.Marshal(p.Schema)
		sum := sha256.Sum256(append([]byte(p.System), schema...))
		hashes[p.Name] = hex.EncodeToString(sum[:])[:12]
	}
	return hashes
}
