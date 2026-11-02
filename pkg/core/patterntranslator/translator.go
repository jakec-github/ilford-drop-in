// Package patterntranslator turns a Shift pattern described in plain English
// into the pattern's VEVENT entries, and what an Override matches into its
// rule, or either into everything that stops it, the way a password field
// lists every rule a password breaks (ADR 0015).
//
// Each translation is stateless: the description is the whole pattern or rule,
// and nothing of an earlier attempt is remembered. A language model writes it,
// and the server's own parser checks it — the model's reply is never trusted
// to read. When the parser refuses a reply, its problems go back to the model
// in the same exchange, up to MaxCalls calls in all. Both translations share
// that loop; what differs is the prompt, the parser, and what the model is told.
package patterntranslator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jakechorley/ilford-drop-in/pkg/core/llm"
	"github.com/jakechorley/ilford-drop-in/pkg/core/shiftpattern"
)

// MaxText is the longest description translated, in characters. A ten-entry
// pattern written out as descriptions is about 600; anything much longer is
// not a description of when Shifts happen.
const MaxText = 1000

// Timeout is the ceiling on one translation, every call included (ADR 0015).
const Timeout = 30 * time.Second

// MaxCalls is how many times one translation asks the model: once, and twice
// more with what the parser found wrong.
const MaxCalls = 3

// GaveUp is what the Organiser is told when MaxCalls calls produced nothing the
// parser would accept. The parser's own messages follow it as further issues,
// in case they help.
const GaveUp = "Unable to generate a pattern from that description. Please try an alternative"

// RuleGaveUp is GaveUp for what an Override matches.
const RuleGaveUp = "Unable to generate a rule from that description. Please try an alternative"

// Kind is what sort of thing stops a description translating.
type Kind string

const (
	// Missing is something a Shift needs that is not there: a start time, a
	// length, which days.
	Missing Kind = "missing"
	// Ambiguous is a description with more than one reading that would give
	// different Shifts.
	Ambiguous Kind = "ambiguous"
	// Unsupported is something a pattern cannot hold, such as a single date.
	Unsupported Kind = "unsupported"
)

// Issue is one thing that stops a description translating: a short message
// naming the part it is about, and phrases the Organiser could type to fix it.
type Issue struct {
	Kind     Kind     `json:"kind"`
	Message  string   `json:"message"`
	Examples []string `json:"examples"`
}

// Result is a translation: the pattern, or the issues that stop there being
// one. Exactly one of the two is set.
type Result struct {
	Pattern string
	Issues  []Issue
}

// Request is one description to translate, and what it is read against.
type Request struct {
	// Text is what the Organiser typed: the whole pattern.
	Text string
	// Zone is the Organisation's time zone, an IANA name. Today is its date
	// there, not the server's.
	Zone string
	Now  time.Time
}

// RuleResult is what an Override matches, translated: the rule — a
// DTSTART;VALUE=DATE line and an RRULE — or the issues that stop there being
// one. Exactly one of the two is set.
type RuleResult struct {
	Rule   string
	Issues []Issue
}

// RuleRequest is one description of what an Override matches, and what it is
// read against.
type RuleRequest struct {
	Text string
	// Pattern is what the saved Shift pattern's entries say, one each: the
	// Shifts there are to match, so the model can tell whether a day has one
	// Shift or several.
	Pattern []string
	Zone    string
	Now     time.Time
}

// Translator turns a description into a pattern, or into what an Override
// matches.
//
// An error is never about the description — that is what issues are for. It
// is ErrTooLong, or ErrUnavailable when the translation could not be made just
// now.
type Translator interface {
	Translate(ctx context.Context, req Request) (Result, error)
	TranslateRule(ctx context.Context, req RuleRequest) (RuleResult, error)
}

var (
	// ErrTooLong is a description longer than MaxText. It is refused before
	// anything is asked.
	ErrTooLong = fmt.Errorf("a description is at most %d characters", MaxText)
	// ErrUnavailable is a translation that could not be made: the model could
	// not be reached, refused the request (a spending limit, say), or the time
	// for the translation ran out.
	ErrUnavailable = errors.New("couldn't translate just now")
)

// New is the Translator that asks model and checks everything it writes with
// the server's parsers.
func New(model llm.Model) Translator {
	return checked{model: model}
}

type checked struct {
	model llm.Model
}

// reply is either reply shape, as the model writes it. Answer is the pattern
// or the rule, whichever the prompt asks for.
type reply struct {
	Status  string  `json:"status"`
	Pattern string  `json:"pattern"`
	Rule    string  `json:"rule"`
	Issues  []Issue `json:"issues"`
}

// task is what one kind of translation brings to the loop.
type task struct {
	prompt *llm.Prompt
	// noun is what the model writes, as the retry turn names it.
	noun   string
	gaveUp string
	// answer is the reply's pattern or rule.
	answer func(reply) string
	// check is everything the server's parser finds wrong with an answer.
	check func(answer string) []string
}

func (c checked) Translate(ctx context.Context, req Request) (Result, error) {
	if utf8.RuneCountInString(req.Text) > MaxText {
		return Result{}, ErrTooLong
	}
	today, err := todayIn(req.Zone, req.Now)
	if err != nil {
		return Result{}, err
	}
	pattern, issues, err := c.run(ctx, task{
		prompt: patternPrompt,
		noun:   "pattern",
		gaveUp: GaveUp,
		answer: func(r reply) string { return r.Pattern },
		check:  func(p string) []string { return check(p, req.Zone) },
	}, firstTurn(req.Text, today))
	return Result{Pattern: pattern, Issues: issues}, err
}

func (c checked) TranslateRule(ctx context.Context, req RuleRequest) (RuleResult, error) {
	if utf8.RuneCountInString(req.Text) > MaxText {
		return RuleResult{}, ErrTooLong
	}
	today, err := todayIn(req.Zone, req.Now)
	if err != nil {
		return RuleResult{}, err
	}
	rule, issues, err := c.run(ctx, task{
		prompt: rulePrompt,
		noun:   "rule",
		gaveUp: RuleGaveUp,
		answer: func(r reply) string { return r.Rule },
		check:  checkRule,
	}, firstRuleTurn(req.Text, req.Pattern, today))
	return RuleResult{Rule: rule, Issues: issues}, err
}

// run is the loop both translations share: ask, check what comes back, and
// send the parser's problems back, or what the reply left out, until it reads,
// the model says what stops it, or MaxCalls calls have been made. A reply that
// cannot be read at all is asked again the same way: there is nothing in it
// to answer.
func (c checked) run(ctx context.Context, t task, first string) (string, []Issue, error) {
	turns := []llm.Turn{{Role: llm.User, Text: first}}
	var problems []string
	for range MaxCalls {
		if err := ctx.Err(); err != nil {
			return "", nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		resp, err := c.model.Reply(ctx, t.prompt, turns)
		if errors.Is(err, llm.ErrUnusableReply) {
			continue
		}
		if err != nil {
			return "", nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}

		r, ok := read(resp.Text)
		if !ok {
			continue
		}
		// A reply that reads but says nothing would come back the same if
		// asked the same way, so the model is told what it left out.
		if r.Status == "issues" && len(r.Issues) == 0 {
			turns = append(turns,
				llm.Turn{Role: llm.Assistant, Text: resp.Text},
				llm.Turn{Role: llm.User, Text: fmt.Sprintf("Your reply listed no issues. List each issue, or give the whole %s.", t.noun)})
			continue
		}
		if r.Status == "issues" {
			return "", r.Issues, nil
		}
		answer := strings.TrimSpace(t.answer(r))
		if answer == "" {
			turns = append(turns,
				llm.Turn{Role: llm.Assistant, Text: resp.Text},
				llm.Turn{Role: llm.User, Text: fmt.Sprintf("Your reply gave no %s. Give the whole %[1]s, or list the issues.", t.noun)})
			continue
		}

		problems = t.check(answer)
		if len(problems) == 0 {
			return answer, nil, nil
		}
		turns = append(turns,
			llm.Turn{Role: llm.Assistant, Text: resp.Text},
			llm.Turn{Role: llm.User, Text: retryTurn(t.noun, problems)})
	}

	issues := []Issue{{Kind: Unsupported, Message: t.gaveUp, Examples: []string{}}}
	for _, p := range problems {
		issues = append(issues, Issue{Kind: Unsupported, Message: p, Examples: []string{}})
	}
	return "", issues, nil
}

// todayIn is now in the Organisation's zone, whose date is today.
func todayIn(zone string, now time.Time) (time.Time, error) {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: the organisation's time zone %q cannot be loaded", ErrUnavailable, zone)
	}
	return now.In(loc), nil
}

// read is a reply as one of its two shapes, or false. The schema makes
// anything else unlikely, not impossible. Either shape may still say nothing:
// issues with none in them, or an ok with no answer.
func read(text string) (reply, bool) {
	var r reply
	if err := json.Unmarshal([]byte(text), &r); err != nil {
		return reply{}, false
	}
	switch r.Status {
	case "ok":
		return r, true
	case "issues":
		for i := range r.Issues {
			if r.Issues[i].Examples == nil {
				r.Issues[i].Examples = []string{}
			}
		}
		return r, true
	}
	return reply{}, false
}

// check is everything the parser finds wrong with a pattern, each naming the
// entry it is about. Warnings are not problems: what they name is read past.
func check(pattern, zone string) []string {
	reading, _ := shiftpattern.Read(pattern, zone)
	problems := append([]string(nil), reading.Problems...)
	for _, e := range reading.Entries {
		for _, p := range e.Problems {
			problems = append(problems, fmt.Sprintf("entry %d: %s", e.Number, p))
		}
	}
	if len(problems) == 0 && len(reading.Entries) == 0 {
		problems = append(problems, "there are no entries")
	}
	return problems
}

// checkRule is everything the rule parser finds wrong with an Override's rule.
// A rule that matches no Shift is not one of them: it is saved like a typed
// one.
func checkRule(rule string) []string {
	line, _ := shiftpattern.ReadRule(rule)
	return line.Problems
}

func firstTurn(text string, today time.Time) string {
	return contextBlock(today) + fmt.Sprintf("\n\n<description>\n%s\n</description>", text)
}

func firstRuleTurn(text string, pattern []string, today time.Time) string {
	var b strings.Builder
	b.WriteString(contextBlock(today))
	b.WriteString("\n\n<pattern>\n")
	for _, entry := range pattern {
		b.WriteString("- ")
		b.WriteString(entry)
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "</pattern>\n\n<description>\n%s\n</description>", text)
	return b.String()
}

// contextBlock is today, the organisation's time zone, and each weekday's
// dates from today to the end of next month: a model counts dates badly, and
// reads them off a list well.
func contextBlock(today time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<context>\nToday: %s\nOrganisation time zone: %s\nDates from today to the end of next month:\n",
		today.Format("2006-01-02 (Monday)"), today.Location())
	first := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	end := time.Date(today.Year(), today.Month()+2, 1, 0, 0, 0, 0, time.UTC)
	for _, weekday := range []time.Weekday{
		time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday,
	} {
		day := first.AddDate(0, 0, (int(weekday)-int(first.Weekday())+7)%7)
		var months []string
		for day.Before(end) {
			month := day.Month()
			var days []string
			for ; day.Before(end) && day.Month() == month; day = day.AddDate(0, 0, 7) {
				days = append(days, strconv.Itoa(day.Day()))
			}
			months = append(months, strings.Join(days, ", ")+" "+day.AddDate(0, 0, -7).Format("January 2006"))
		}
		fmt.Fprintf(&b, "- %s: %s\n", weekday, strings.Join(months, "; "))
	}
	b.WriteString("</context>")
	return b.String()
}

func retryTurn(noun string, problems []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The app cannot use that %s:\n", noun)
	for _, p := range problems {
		b.WriteString("- ")
		b.WriteString(p)
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "\nReply again for the same description: the whole %s, corrected, or the issues if it cannot be written.", noun)
	return b.String()
}
