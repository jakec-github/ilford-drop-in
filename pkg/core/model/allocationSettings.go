package model

import "slices"

// SwitchableConstraint is one optional allocator rule an admin can switch on.
//
// Name is the contract: it is the key the settings record stores an answer
// under, and the name the solver is sent. It matches a constraint in
// pyallocator's SWITCHABLE_CONSTRAINTS exactly — that registry is the authority
// on what the rule *does* (ADR 0006), and this list is what an admin is offered.
// The two are pinned by a test on each side, because a name that drifts would
// quietly switch a rule off on every deployment that had it on.
//
// Label and Description are the screen's words. They live here rather than in
// the client so that adding a rule is one edit in one language, and so the
// answer to "what does this toggle do?" is next to the name it is keyed by.
type SwitchableConstraint struct {
	Name        string
	Label       string
	Description string
	// ValueLabel names the extra answer a rule needs beyond on-or-off, empty
	// for the rules that need none. Only max_frequency has one, and the
	// screen renders a field for it when the toggle is on.
	ValueLabel string
}

// SwitchableConstraints is the registry: the optional rules that exist, in the
// order they are offered. Order is the solver's too — EnabledConstraints sorts
// by it — so a run is built the same way whatever order the answers were saved
// in.
//
// The fundamental constraints are deliberately absent. A rota without them is
// not a rota, so they are not an admin's decision to make.
var SwitchableConstraints = []SwitchableConstraint{
	{
		Name:        MaxFrequencyConstraint,
		Label:       "Cap how often somebody works",
		Description: "No volunteer is allocated more than a set share of a rota's shifts.",
		ValueLabel:  "Most of a rota one person may work",
	},
	{
		Name:        "male_required",
		Label:       "Keep a seat for male cover",
		Description: "A shift with no male volunteer keeps a seat open, so one can be added by hand afterwards.",
	},
	{
		Name:        "no_back_to_back",
		Label:       "No back-to-back shifts",
		Description: "Nobody works two shifts in a row, counting the last shift of the previous rota.",
	},
	{
		Name:        "one_shift_per_month",
		Label:       "At most one shift a month",
		Description: "Nobody works twice in the same calendar month. Often impossible to satisfy at real volunteer numbers.",
	},
}

// AllocationSettings is which optional allocator rules apply: an admin's
// answers, not the rules themselves.
//
// Every field is a zero value until an admin saves the section, and that reads
// as every rule off. Unset is the ordinary first state of a deployment rather
// than a fault (ADR 0006) — a rota allocated with none of these switched on is
// a legal rota, just an unconstrained one.
//
// The JSON tags are the stored document: this type is what the
// allocation_settings column holds, not a view of it. Decoding ignores keys it
// does not know, which is exactly the leniency ADR 0006 asks for — a document
// written by a build that had a rule this one does not still reads.
type AllocationSettings struct {
	// Enabled holds one answer per rule, keyed by SwitchableConstraint.Name.
	// A missing key and a stored false mean the same thing: off. A key naming
	// a rule this build does not have is ignored — see UnknownConstraints.
	Enabled map[string]bool `json:"enabled,omitempty"`
	// MaxFrequency is the share of a rota's shifts one volunteer may work,
	// between 0 and 1. Read only when max_frequency is enabled; zero when an
	// admin has never set it.
	//
	// It is a top-level field rather than something hanging off the toggle
	// because it is the only rule that carries a value. A second one would get
	// its own field here — a change to the document, which is a change to no
	// schema at all.
	MaxFrequency float64 `json:"maxFrequency,omitempty"`
}

// MaxFrequencyConstraint is the one switchable rule that carries a value as
// well as a switch, named here so the places that special-case it say which
// rule they mean.
const MaxFrequencyConstraint = "max_frequency"

// IsEnabled reports whether a rule applies. An unknown name is not enabled,
// which is what makes a withdrawn rule harmless.
func (s AllocationSettings) IsEnabled(name string) bool {
	return s.Enabled[name]
}

// EnabledConstraints names the rules this allocation applies, in registry
// order. It is what the solver is sent.
//
// It is built by walking the registry rather than the stored answers, so a key
// naming a rule that no longer exists cannot reach the solver at all.
func (s AllocationSettings) EnabledConstraints() []string {
	var enabled []string
	for _, c := range SwitchableConstraints {
		if s.Enabled[c.Name] {
			enabled = append(enabled, c.Name)
		}
	}
	return enabled
}

// UnknownConstraints names stored answers this build has no rule for, so an
// operator can be told why a rule they remember switching on stopped applying.
//
// Only answers that asked for something are named. A stored false for a
// withdrawn rule asks for nothing, so dropping it is not news.
func (s AllocationSettings) UnknownConstraints() []string {
	known := make(map[string]bool, len(SwitchableConstraints))
	for _, c := range SwitchableConstraints {
		known[c.Name] = true
	}

	var unknown []string
	for name, on := range s.Enabled {
		if on && !known[name] {
			unknown = append(unknown, name)
		}
	}
	slices.Sort(unknown)
	return unknown
}

// Missing names the answers an admin has yet to give that a rule they switched
// on needs, worded as they read on the Settings screen. Empty means these
// settings can be allocated against.
//
// Only max_frequency can be incomplete: it is the one rule carrying a value as
// well as a switch, so it is the one that can be on and still say nothing.
func (s AllocationSettings) Missing() []string {
	var missing []string
	if s.IsEnabled(MaxFrequencyConstraint) && !validFrequency(s.MaxFrequency) {
		missing = append(missing, "the maximum allocation frequency")
	}
	return missing
}

// MaxAllocationCount turns the frequency into the cap the solver works in: how
// many of this rota's shifts one volunteer may be given.
//
// With the rule off it is every shift in the rota — a cap nothing can trip
// over. The solver is not sent the constraint at all in that case, so this is
// belt and braces rather than the mechanism, and it means the number in the
// contract is never a nonsense one.
//
// It rounds down, as the config-derived cap always did, but never to zero: a
// frequency an admin set to mean "not often" must not come out meaning "never".
func (s AllocationSettings) MaxAllocationCount(shiftCount int) int {
	if !s.IsEnabled(MaxFrequencyConstraint) || !validFrequency(s.MaxFrequency) {
		return shiftCount
	}

	count := int(float64(shiftCount) * s.MaxFrequency)
	if count < 1 {
		return 1
	}
	return count
}

// validFrequency reports whether a share of a rota is one an admin could have
// meant: more than none of it, and no more than all of it.
func validFrequency(frequency float64) bool {
	return frequency > 0 && frequency <= 1
}
