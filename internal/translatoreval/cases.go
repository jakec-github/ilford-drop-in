// Package translatoreval measures the plain-English translators (ADR 0015)
// against a set of cases written by hand: how often each description gets
// the right answer, in how many calls, at what cost and how quickly.
//
// It asks the real model and costs money, so it runs only when someone runs
// cmd/translator-eval. Neither scripts/check.sh nor CI does; this package's own
// tests use a fake model.
//
// A translation is graded by the Shifts it gives, never by its text: there are
// many ways to write "every Sunday", and the parser is the one evaluator of
// all of them (ADR 0013).
package translatoreval

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/jakechorley/ilford-drop-in/pkg/core/patterntranslator"
	"github.com/jakechorley/ilford-drop-in/pkg/core/shiftpattern"
)

// The two kinds of case, one file each.
const (
	// Patterns are descriptions of a whole Shift pattern.
	Patterns = "patterns"
	// Rules are descriptions of what an Override matches, read against a
	// saved pattern.
	Rules = "rules"
)

// Case is one description and the reply it should get.
type Case struct {
	Name string
	// Kind is Patterns or Rules.
	Kind string
	// Group is what the case tests, shared with the cases beside it in its
	// file, such as "prompt injection".
	Group string
	Text  string
	// Today is the date the translator is told it is, so a case means the
	// same thing whenever it is run.
	Today time.Time
	Zone  string
	// Pattern is, for a rule, the saved Shift pattern it is read against.
	Pattern string
	// Planted is, for a repair case, the pattern or rule the model is taken
	// to have written first: one the parser refuses. The model is asked only
	// after the parser's messages have gone back, so what is graded is how it
	// repairs it.
	Planted string
	Expect  Expect
}

// Expect is the reply a case should get: a translation, or issues.
type Expect struct {
	Pattern string          `yaml:"pattern"`
	Rule    string          `yaml:"rule"`
	Issues  []ExpectedIssue `yaml:"issues"`
}

// ExpectedIssue is one issue a description should raise: its kind, and a note
// of what it is about. The note is for whatever pairs the issues raised with
// the ones wanted; the wording of a message is the model's own.
type ExpectedIssue struct {
	Kind patterntranslator.Kind `yaml:"kind" json:"kind"`
	// Or is a second kind the issue may fairly be, where the line between
	// them is one no Organiser would notice: "evenings" with no time is as
	// much a missing time as an ambiguous one.
	Or    patterntranslator.Kind `yaml:"or" json:"or,omitempty"`
	About string                 `yaml:"about" json:"about"`
}

// Repairs is whether the case grades a repair rather than a first attempt.
func (c Case) Repairs() bool {
	return c.Planted != ""
}

// Translates is whether the case wants a translation rather than issues.
func (c Case) Translates() bool {
	return len(c.Expect.Issues) == 0
}

// Descriptions is what a rule case's pattern says, entry by entry, as the
// server tells the rule translator.
func (c Case) Descriptions() []string {
	reading, _ := shiftpattern.Read(c.Pattern, c.Zone)
	var lines []string
	for _, e := range reading.Entries {
		lines = append(lines, e.Description)
	}
	return lines
}

// file is a case file as written: its cases in groups, by what they test.
// Zone, today and, for rules, the pattern are the defaults for every case in
// it.
type file struct {
	Zone    string      `yaml:"zone"`
	Today   string      `yaml:"today"`
	Pattern string      `yaml:"pattern"`
	Groups  []groupYAML `yaml:"groups"`
}

type groupYAML struct {
	Name  string     `yaml:"name"`
	Cases []caseYAML `yaml:"cases"`
}

type caseYAML struct {
	Name    string `yaml:"name"`
	Text    string `yaml:"text"`
	Today   string `yaml:"today"`
	Zone    string `yaml:"zone"`
	Pattern string `yaml:"pattern"`
	Planted string `yaml:"planted"`
	Expect  Expect `yaml:"expect"`
}

// ReadCases reads a file of cases of kind, and checks every expected answer
// with the real parser. A case the parser would refuse is a mistake in the
// case, found before anything is spent; so is a key the file has no use for.
func ReadCases(kind string, data []byte) ([]Case, error) {
	var f file
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%s: %w", kind, err)
	}

	var cases []Case
	var problems []string
	seen, seenGroup := map[string]bool{}, map[string]bool{}
	for _, g := range f.Groups {
		if strings.TrimSpace(g.Name) == "" || seenGroup[g.Name] {
			problems = append(problems, fmt.Sprintf("group %q: every group needs a name of its own", g.Name))
		}
		seenGroup[g.Name] = true
		for _, y := range g.Cases {
			c, err := readCase(kind, f, y)
			c.Group = g.Name
			if err == nil && seen[c.Name] {
				err = errors.New("two cases have this name")
			}
			seen[c.Name] = true
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: %v", y.Name, err))
				continue
			}
			cases = append(cases, c)
		}
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("%s has broken cases:\n  %s", kind, strings.Join(problems, "\n  "))
	}
	return cases, nil
}

func readCase(kind string, f file, y caseYAML) (Case, error) {
	c := Case{
		Name:    y.Name,
		Kind:    kind,
		Text:    strings.TrimSpace(y.Text),
		Zone:    or(y.Zone, f.Zone),
		Pattern: or(y.Pattern, f.Pattern),
		Planted: strings.TrimSpace(y.Planted),
		Expect:  y.Expect,
	}
	if c.Name == "" {
		return c, errors.New("a case needs a name")
	}
	if c.Text == "" || utf8.RuneCountInString(c.Text) > patterntranslator.MaxText {
		return c, fmt.Errorf("the text must be 1 to %d characters", patterntranslator.MaxText)
	}
	today, err := time.Parse(time.DateOnly, or(y.Today, f.Today))
	if err != nil {
		return c, errors.New("today must be a date, like 2026-11-01")
	}
	c.Today = today
	if _, err := time.LoadLocation(c.Zone); err != nil || c.Zone == "" {
		return c, fmt.Errorf("zone %q cannot be loaded", c.Zone)
	}

	e := c.Expect
	if kind == Rules {
		if strings.TrimSpace(c.Pattern) == "" {
			return c, errors.New("no pattern for the rule to be read against")
		}
		if _, p := shiftpattern.Read(c.Pattern, c.Zone); p == nil {
			return c, fmt.Errorf("the pattern does not read: %s", strings.Join(problemsOf(c.Pattern, c.Zone), "; "))
		}
	}
	if c.Planted != "" && !refused(kind, c.Planted, c.Zone) {
		return c, errors.New("the planted reply reads: a repair case needs one the parser refuses")
	}
	answers := 0
	for _, s := range []string{e.Pattern, e.Rule} {
		if strings.TrimSpace(s) != "" {
			answers++
		}
	}
	if len(e.Issues) > 0 {
		answers++
	}
	wrongAnswer := (kind == Patterns && e.Rule != "") || (kind == Rules && e.Pattern != "")
	if answers != 1 || wrongAnswer {
		answer := "pattern"
		if kind == Rules {
			answer = "rule"
		}
		return c, fmt.Errorf("expect a %s or issues, exactly one", answer)
	}

	switch {
	case e.Pattern != "":
		if _, p := shiftpattern.Read(e.Pattern, c.Zone); p == nil {
			return c, fmt.Errorf("the expected pattern does not read: %s", strings.Join(problemsOf(e.Pattern, c.Zone), "; "))
		}
	case e.Rule != "":
		if line, r := shiftpattern.ReadRule(e.Rule); r == nil {
			return c, fmt.Errorf("the expected rule does not read: %s", strings.Join(line.Problems, "; "))
		}
	}
	for _, issue := range e.Issues {
		kinds := []patterntranslator.Kind{issue.Kind}
		if issue.Or != "" {
			if issue.Or == issue.Kind {
				return c, fmt.Errorf("issue kind %q is its own or", issue.Kind)
			}
			kinds = append(kinds, issue.Or)
		}
		for _, k := range kinds {
			switch k {
			case patterntranslator.Missing, patterntranslator.Ambiguous, patterntranslator.Unsupported:
			default:
				return c, fmt.Errorf("issue kind %q is not missing, ambiguous or unsupported", k)
			}
		}
		if strings.TrimSpace(issue.About) == "" {
			return c, errors.New("every expected issue says what it is about")
		}
	}
	return c, nil
}

// refused is whether the translator's own check would send a reply back to
// the model.
func refused(kind, reply, zone string) bool {
	if kind == Rules {
		line, _ := shiftpattern.ReadRule(reply)
		return len(line.Problems) > 0
	}
	reading, _ := shiftpattern.Read(reply, zone)
	return len(problemsOf(reply, zone)) > 0 || len(reading.Entries) == 0
}

func problemsOf(pattern, zone string) []string {
	reading, _ := shiftpattern.Read(pattern, zone)
	problems := append([]string(nil), reading.Problems...)
	for _, e := range reading.Entries {
		for _, p := range e.Problems {
			problems = append(problems, fmt.Sprintf("entry %d: %s", e.Number, p))
		}
	}
	return problems
}

func or(s, otherwise string) string {
	if strings.TrimSpace(s) != "" {
		return s
	}
	return otherwise
}

// Select is the cases a run asks for: every one, either kind, one group, or
// one case by name.
func Select(cases []Case, which string) ([]Case, error) {
	switch which {
	case "", "all":
		return cases, nil
	case Patterns, Rules:
		var out []Case
		for _, c := range cases {
			if c.Kind == which {
				out = append(out, c)
			}
		}
		return out, nil
	}
	for _, c := range cases {
		if c.Name == which {
			return []Case{c}, nil
		}
	}
	var group []Case
	for _, c := range cases {
		if c.Group == which {
			group = append(group, c)
		}
	}
	if len(group) > 0 {
		return group, nil
	}
	return nil, fmt.Errorf("no case or group is called %q: give all, patterns, rules, a group's name or a case's", which)
}
