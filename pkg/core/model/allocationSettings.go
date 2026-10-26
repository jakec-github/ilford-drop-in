package model

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// RuleFamily says what an Allocation Rule is about: what each Shift must
// contain, or what any one volunteer may be given (ADR 0012).
type RuleFamily string

const (
	// ShiftRuleFamily rules apply to every Shift and carry no Scope.
	ShiftRuleFamily RuleFamily = "shift"
	// VolunteerRuleFamily rules apply to the volunteers their Scope covers.
	VolunteerRuleFamily RuleFamily = "volunteer"
)

// The Rule Kinds this build has. Each id is the contract: it is what a stored
// rule names, and the kind the solver is sent. It matches a kind in
// pyallocator's RULE_KINDS exactly — that registry is the authority on what a
// kind *does*, and this library is what an Organiser is offered. The two are
// pinned by a test on each side, because an id that drifts would quietly stop
// every stored rule of that kind applying.
const (
	MaxFrequencyKind  = "max_frequency"
	ShiftGapKind      = "shift_gap"
	MonthlyCapKind    = "monthly_cap"
	ShiftRequiresKind = "shift_requires"
)

// RuleKind is one entry in the Rule Library: a kind of rule an Organiser can
// add, any number of times.
//
// Label and Description are the screen's words. They live here rather than in
// the client so that adding a kind is one edit in one language, next to the id
// it is keyed by.
type RuleKind struct {
	ID          string
	Family      RuleFamily
	Label       string
	Description string
	// ValueLabel names the one value a rule of this kind carries, empty for a
	// kind that carries none. MinValue and MaxValue bound it, inclusive.
	ValueLabel string
	MinValue   int
	MaxValue   int
	// DefaultValue is what a rule of this kind starts at when it is added from
	// the library, before an Organiser moves it.
	DefaultValue int
	// Input says which control the screen sets the value with, so the client
	// can draw a ring for a share and a stepper for a count without knowing
	// which kind it is looking at. Empty for a kind with no value; a client
	// that does not know the input falls back to a plain number.
	Input ValueInput
	// OptionLabel names the one choice a rule of this kind carries, empty for
	// a kind that carries none, and Options is every choice it accepts.
	OptionLabel string
	Options     []string
}

// ValueInput is how a kind's value is best set on screen.
type ValueInput string

const (
	// PercentInput is a share of a whole, 0–100: drawn as a ring.
	PercentInput ValueInput = "percent"
	// StepperInput is a small count usually nudged by one: drawn as − and +.
	StepperInput ValueInput = "stepper"
	// CountInput is a count across a range: drawn as a slider whose scale
	// grows as the value nears its end.
	CountInput ValueInput = "count"
)

// The genders a rule can ask for. They match the roster's free-text column
// exactly, as allocator.GenderMale does.
var ruleGenders = []string{"Male", "Female"}

// HasValue reports whether a rule of this kind carries a value.
func (k RuleKind) HasValue() bool {
	return k.ValueLabel != ""
}

// HasOption reports whether a rule of this kind carries a choice.
func (k RuleKind) HasOption() bool {
	return len(k.Options) > 0
}

// RuleLibrary is every Rule Kind this build has, in the order they are offered.
//
// Every kind in it can be satisfied by leaving Seats empty, so no set of
// Allocation Rules can stop a rota being allocated (ADR 0012). A kind that
// demands something of a Shift must carry an escape before it may join.
//
// The fundamental constraints are deliberately absent. A rota without them is
// not a rota, so they are not an Organiser's decision to make.
var RuleLibrary = []RuleKind{
	{
		ID:           MaxFrequencyKind,
		Family:       VolunteerRuleFamily,
		Label:        "Percentage attendance cap",
		Description:  "Nobody covered works more than a set share of a rota's shifts. Rounds down, but never to none.",
		ValueLabel:   "Most of a rota one person may work (%)",
		MinValue:     1,
		MaxValue:     100,
		DefaultValue: 50,
		Input:        PercentInput,
	},
	{
		ID:           ShiftGapKind,
		Family:       VolunteerRuleFamily,
		Label:        "Shift gap",
		Description:  "Between any two shifts somebody covered works, there are at least this many they do not. Closed shifts count, and so does the end of the previous rota. A gap of 1 means no back-to-back shifts.",
		ValueLabel:   "Minimum shifts between",
		MinValue:     1,
		MaxValue:     52,
		DefaultValue: 1,
		Input:        StepperInput,
	},
	{
		ID:           MonthlyCapKind,
		Family:       VolunteerRuleFamily,
		Label:        "Monthly attendance cap",
		Description:  "Nobody covered works more than this many shifts in one calendar month, counting shifts already worked that month in the previous rota.",
		ValueLabel:   "Most shifts in one month",
		MinValue:     1,
		MaxValue:     31,
		DefaultValue: 2,
		Input:        CountInput,
	},
	{
		ID:     ShiftRequiresKind,
		Family: ShiftRuleFamily,
		Label:  "All shifts require",
		// Worded as what the rule asks for and what it costs when the roster
		// cannot give it. The mechanism — a Seat held open for somebody to be
		// added into by hand — is how "left unfilled" happens rather than a
		// second thing the rule does, so it is said once, in those terms.
		Description: "Every shift has a volunteer of the chosen gender on it. Where that is not possible the shift is left unfilled, with a seat kept open for one to be added by hand.",
		OptionLabel: "Gender",
		Options:     ruleGenders,
	},
}

// RuleKindByID finds a kind in the library. The second result is false for an
// id this build does not have.
func RuleKindByID(id string) (RuleKind, bool) {
	for _, k := range RuleLibrary {
		if k.ID == id {
			return k, true
		}
	}
	return RuleKind{}, false
}

// Scope is which volunteers a Volunteer Rule covers: an include list and an
// exclude list, at most one of them naming anybody (ADR 0012). Both empty
// covers everyone.
//
// It names volunteers by roster id, and a name that has left the roster stays
// on it, covering nobody, until somebody removes it.
type Scope struct {
	Include []string `json:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}

// Covers reports whether the rule applies to this volunteer.
func (s Scope) Covers(volunteerID string) bool {
	if len(s.Include) > 0 {
		return slices.Contains(s.Include, volunteerID)
	}
	return !slices.Contains(s.Exclude, volunteerID)
}

// IsEmpty reports whether the Scope names nobody, and so covers everyone.
func (s Scope) IsEmpty() bool {
	return len(s.Include) == 0 && len(s.Exclude) == 0
}

// normalised trims each id, drops blanks and repeats, and keeps the order an
// Organiser named them in.
func (s Scope) normalised() Scope {
	return Scope{Include: cleanIDs(s.Include), Exclude: cleanIDs(s.Exclude)}
}

func cleanIDs(ids []string) []string {
	var cleaned []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" && !slices.Contains(cleaned, id) {
			cleaned = append(cleaned, id)
		}
	}
	return cleaned
}

// AllocationRule is one Rule Kind added by an Organiser, with its own value
// and, for a Volunteer Rule, its own Scope.
//
// The JSON tags are the stored document: this type is what each entry of the
// allocation_settings column's rules holds, not a view of it.
type AllocationRule struct {
	// ID identifies the rule so it can be edited or removed in place. It is
	// minted when the rule is added and never changes.
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// Value is the one number the kind asks for — a percentage, a gap, a
	// monthly limit — and zero for a kind that asks for none.
	Value int `json:"value,omitempty"`
	// Option is the one choice the kind asks for — a gender, for "All shifts
	// require" — and empty for a kind that asks for none.
	Option string `json:"option,omitempty"`
	Scope  Scope  `json:"scope"`
	// Note says why the rule was added. The allocator never reads it.
	Note string `json:"note,omitempty"`
}

// Normalised is the rule as it is stored: Scope ids cleaned up and the note
// trimmed.
func (r AllocationRule) Normalised() AllocationRule {
	r.Scope = r.Scope.normalised()
	r.Note = strings.TrimSpace(r.Note)
	r.Option = strings.TrimSpace(r.Option)
	return r
}

// Validate says why a rule cannot be stored, or nil when it can. It refuses only
// what cannot be represented: a kind this build does not have, a value outside
// what the kind means, a choice it does not offer, a Scope including and excluding at once, or a Scope on a
// rule that has nobody to scope.
func (r AllocationRule) Validate() error {
	kind, ok := RuleKindByID(r.Kind)
	if !ok {
		return fmt.Errorf("there is no rule called %q", r.Kind)
	}

	if kind.HasValue() {
		if r.Value < kind.MinValue || r.Value > kind.MaxValue {
			return fmt.Errorf("%q needs a value from %d to %d, and %d is not one",
				kind.Label, kind.MinValue, kind.MaxValue, r.Value)
		}
	} else if r.Value != 0 {
		return fmt.Errorf("%q takes no value", kind.Label)
	}

	if kind.HasOption() {
		if !slices.Contains(kind.Options, r.Option) {
			return fmt.Errorf("%q needs one of %s, and %q is not one",
				kind.Label, strings.Join(kind.Options, ", "), r.Option)
		}
	} else if r.Option != "" {
		return fmt.Errorf("%q takes no choice", kind.Label)
	}

	if kind.Family == ShiftRuleFamily && !r.Scope.IsEmpty() {
		return fmt.Errorf("%q applies to every shift, so it cannot name volunteers", kind.Label)
	}
	if len(r.Scope.Include) > 0 && len(r.Scope.Exclude) > 0 {
		return errors.New("a rule can include volunteers or exclude them, not both at once")
	}

	return nil
}

// AllocationSettings is everything an Organiser decides about how the allocator
// solves: its Allocation Rules today, and in time the weights on its
// preferences.
//
// The JSON tags are the stored document. Decoding ignores keys it does not
// know, which is the leniency ADR 0006 asks for: a document written by a build
// with more in it still reads.
type AllocationSettings struct {
	Rules []AllocationRule `json:"rules,omitempty"`
}

// Applicable is the rules the solve applies, in the order they were added: every
// stored rule this build understands. A rule of a kind it does not have, or one
// whose stored values no longer validate, is left out rather than refused — see
// Ignored.
func (s AllocationSettings) Applicable() []AllocationRule {
	var applicable []AllocationRule
	for _, r := range s.Rules {
		if r.Validate() == nil {
			applicable = append(applicable, r)
		}
	}
	return applicable
}

// Ignored is the stored rules the solve leaves out, so an operator can be told
// why a rule somebody added stopped applying.
func (s AllocationSettings) Ignored() []AllocationRule {
	var ignored []AllocationRule
	for _, r := range s.Rules {
		if r.Validate() != nil {
			ignored = append(ignored, r)
		}
	}
	return ignored
}

// RuleByID finds a stored rule. The second result is false when there is none.
func (s AllocationSettings) RuleByID(id string) (AllocationRule, bool) {
	for _, r := range s.Rules {
		if r.ID == id {
			return r, true
		}
	}
	return AllocationRule{}, false
}

// MaxAllocationCount turns a "cap how often somebody works" percentage into the
// cap the solver works in: how many of this rota's shifts one volunteer may be
// given.
//
// It rounds down, as the cap always has, but never to zero: a share an Organiser
// set to mean "not often" must not come out meaning "never".
func MaxAllocationCount(percent, shiftCount int) int {
	count := shiftCount * percent / 100
	if count < 1 {
		return 1
	}
	return count
}
