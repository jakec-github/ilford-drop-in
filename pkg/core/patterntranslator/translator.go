// Package patterntranslator turns a Shift pattern described in plain English
// into the pattern's VEVENT entries, or into everything that stops it, the way
// a password field lists every rule a password breaks (ADR 0015).
//
// Each translation is stateless: the description is the whole pattern, and
// nothing of an earlier attempt is remembered. A language model writes the
// pattern, and the server's own parser checks it — the model's reply is never
// trusted to read. When the parser refuses a reply, its problems go back to the
// model in the same exchange, up to MaxCalls calls in all.
package patterntranslator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jakechorley/ilford-drop-in/pkg/core/shiftpattern"
)

// MaxText is the longest description translated, in characters. A ten-entry
// pattern written out as descriptions is about 600; anything much longer is
// not a description of when Shifts happen.
const MaxText = 1000

// MaxCalls is how many times one translation asks the model: once, and twice
// more with what the parser found wrong.
const MaxCalls = 3

// GaveUp is what the Organiser is told when MaxCalls calls produced nothing the
// parser would accept. The parser's own messages follow it as further issues,
// in case they help.
const GaveUp = "Unable to generate a pattern from that description. Please try an alternative"

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

// Translator turns a description into a pattern.
//
// An error is never about the description — that is what issues are for. It
// is ErrTooLong, or ErrUnavailable when the translation could not be made just
// now.
type Translator interface {
	Translate(ctx context.Context, req Request) (Result, error)
}

var (
	// ErrTooLong is a description longer than MaxText. It is refused before
	// anything is asked.
	ErrTooLong = fmt.Errorf("a description is at most %d characters", MaxText)
	// ErrUnavailable is a translation that could not be made: the model could
	// not be reached, refused the request (a spending limit, say), or the time
	// for the translation ran out.
	ErrUnavailable = errors.New("couldn't translate just now")
	// ErrUnusableReply is a reply a Model returns that cannot be read as an
	// answer — a refusal, or one cut short at its token limit. It counts as a
	// failed call, like a reply that is not JSON.
	ErrUnusableReply = errors.New("the model's reply cannot be used")
)

// Role is who says a Turn.
type Role string

const (
	User      Role = "user"
	Assistant Role = "assistant"
)

// Turn is one message of the exchange with the model.
type Turn struct {
	Role Role
	Text string
}

// Model is a language model behind the system prompt and reply schema: given
// the exchange so far, its next reply, as the JSON text it wrote. An error
// other than ErrUnusableReply means it could not be asked.
type Model interface {
	Reply(ctx context.Context, turns []Turn) (string, error)
}

// New is the Translator that asks model and checks every pattern it writes
// with the server's parser.
func New(model Model) Translator {
	return checked{model: model}
}

type checked struct {
	model Model
}

// reply is either reply shape, as the model writes it.
type reply struct {
	Status  string  `json:"status"`
	Pattern string  `json:"pattern"`
	Issues  []Issue `json:"issues"`
}

func (c checked) Translate(ctx context.Context, req Request) (Result, error) {
	if utf8.RuneCountInString(req.Text) > MaxText {
		return Result{}, ErrTooLong
	}
	loc, err := time.LoadLocation(req.Zone)
	if err != nil {
		return Result{}, fmt.Errorf("%w: the organisation's time zone %q cannot be loaded", ErrUnavailable, req.Zone)
	}

	turns := []Turn{{Role: User, Text: firstTurn(req.Text, req.Now.In(loc))}}
	var problems []string
	for range MaxCalls {
		if err := ctx.Err(); err != nil {
			return Result{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		text, err := c.model.Reply(ctx, turns)
		if errors.Is(err, ErrUnusableReply) {
			continue
		}
		if err != nil {
			return Result{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}

		r, ok := read(text)
		if !ok {
			continue
		}
		if r.Status == "issues" {
			return Result{Issues: r.Issues}, nil
		}

		pattern := strings.TrimSpace(r.Pattern)
		problems = check(pattern, req.Zone)
		if len(problems) == 0 {
			return Result{Pattern: pattern}, nil
		}
		turns = append(turns,
			Turn{Role: Assistant, Text: text},
			Turn{Role: User, Text: retryTurn(problems)})
	}

	issues := []Issue{{Kind: Unsupported, Message: GaveUp, Examples: []string{}}}
	for _, p := range problems {
		issues = append(issues, Issue{Kind: Unsupported, Message: p, Examples: []string{}})
	}
	return Result{Issues: issues}, nil
}

// read is a reply as one of its two shapes, or false. The schema makes
// anything else unlikely, not impossible.
func read(text string) (reply, bool) {
	var r reply
	if err := json.Unmarshal([]byte(text), &r); err != nil {
		return reply{}, false
	}
	switch r.Status {
	case "ok":
		return r, strings.TrimSpace(r.Pattern) != ""
	case "issues":
		if len(r.Issues) == 0 {
			return reply{}, false
		}
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

func firstTurn(text string, today time.Time) string {
	return fmt.Sprintf("<context>\nToday: %s\nOrganisation time zone: %s\n</context>\n\n<description>\n%s\n</description>",
		today.Format("2006-01-02 (Monday)"), today.Location(), text)
}

func retryTurn(problems []string) string {
	var b strings.Builder
	b.WriteString("The app cannot use that pattern:\n")
	for _, p := range problems {
		b.WriteString("- ")
		b.WriteString(p)
		b.WriteString("\n")
	}
	b.WriteString("\nReply again for the same description: the whole pattern, corrected, or the issues if it cannot be written.")
	return b.String()
}
