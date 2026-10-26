package model_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

// A rule starts out covering everyone, and is narrowed by naming people in
// one of its two lists (ADR 0012).
func TestAnEmptyScopeCoversEveryone(t *testing.T) {
	var scope model.Scope

	assert.True(t, scope.Covers("alice"))
	assert.True(t, scope.Covers("bob"))
}

func TestAnIncludeListCoversOnlyTheVolunteersOnIt(t *testing.T) {
	scope := model.Scope{Include: []string{"alice"}}

	assert.True(t, scope.Covers("alice"))
	assert.False(t, scope.Covers("bob"))
}

func TestAnExcludeListCoversEveryoneButTheVolunteersOnIt(t *testing.T) {
	scope := model.Scope{Exclude: []string{"alice"}}

	assert.False(t, scope.Covers("alice"))
	assert.True(t, scope.Covers("bob"))
}

// A volunteer who has left the roster stays on an include list, so a list
// naming only such people covers nobody still on it — not everyone.
func TestAnIncludeListOfLeaversCoversNobody(t *testing.T) {
	scope := model.Scope{Include: []string{"left-the-roster"}}

	assert.False(t, scope.Covers("alice"))
}

// The library's ids, pinned. They are the contract with pyallocator's
// RULE_KINDS (constraints/__init__.py), which pins the same four from the
// other side, and what every stored rule names: renaming one quietly stops
// every rule of that kind applying.
func TestTheRuleLibraryIsTheFourDayOneKinds(t *testing.T) {
	var ids []string
	families := map[string]model.RuleFamily{}
	for _, k := range model.RuleLibrary {
		ids = append(ids, k.ID)
		families[k.ID] = k.Family
		assert.NotEmpty(t, k.Label, k.ID)
		assert.NotEmpty(t, k.Description, k.ID)
	}

	assert.Equal(t, []string{"max_frequency", "shift_gap", "monthly_cap", "shift_requires"}, ids)
	assert.Equal(t, model.ShiftRuleFamily, families["shift_requires"])
	assert.Equal(t, model.VolunteerRuleFamily, families["shift_gap"])
}

func TestValidateAcceptsEveryKindWithASensibleValue(t *testing.T) {
	for _, rule := range []model.AllocationRule{
		{Kind: "max_frequency", Value: 34},
		{Kind: "shift_gap", Value: 1, Scope: model.Scope{Exclude: []string{"alice"}}},
		{Kind: "monthly_cap", Value: 2, Scope: model.Scope{Include: []string{"bob"}}},
		{Kind: "shift_requires", Option: "Male"},
		{Kind: "shift_requires", Option: "Female"},
	} {
		assert.NoError(t, rule.Validate(), rule.Kind)
	}
}

func TestValidateRefusesWhatCannotBeRepresented(t *testing.T) {
	for name, rule := range map[string]model.AllocationRule{
		"an unknown kind":               {Kind: "phase_of_the_moon"},
		"a missing value":               {Kind: "shift_gap"},
		"a percentage over 100":         {Kind: "max_frequency", Value: 101},
		"a negative gap":                {Kind: "shift_gap", Value: -1},
		"a value on a valueless rule":   {Kind: "shift_requires", Option: "Male", Value: 1},
		"a missing choice":              {Kind: "shift_requires"},
		"a choice it does not offer":    {Kind: "shift_requires", Option: "male"},
		"a choice on a choiceless rule": {Kind: "shift_gap", Value: 1, Option: "Male"},
		"a Scope on a Shift Rule":       {Kind: "shift_requires", Option: "Male", Scope: model.Scope{Include: []string{"alice"}}},
		"including and excluding": {Kind: "monthly_cap", Value: 1, Scope: model.Scope{
			Include: []string{"alice"}, Exclude: []string{"bob"},
		}},
	} {
		assert.Error(t, rule.Validate(), name)
	}
}

func TestNormalisedCleansTheScopeAndNote(t *testing.T) {
	rule := model.AllocationRule{
		Kind:  "shift_gap",
		Value: 1,
		Scope: model.Scope{Exclude: []string{" alice ", "", "bob", "alice"}},
		Note:  "  asked for a rest  ",
	}.Normalised()

	assert.Equal(t, []string{"alice", "bob"}, rule.Scope.Exclude)
	assert.Nil(t, rule.Scope.Include)
	assert.Equal(t, "asked for a rest", rule.Note)
}

// A stored rule this build cannot apply is left out of the solve and named,
// never an error (ADR 0006, the 2026-08-03 outage).
func TestRulesThisBuildCannotApplyAreIgnoredAndNamed(t *testing.T) {
	settings := model.AllocationSettings{Rules: []model.AllocationRule{
		{ID: "1", Kind: "shift_gap", Value: 1},
		{ID: "2", Kind: "phase_of_the_moon", Value: 3},
		{ID: "3", Kind: "shift_requires", Option: "Male"},
		{ID: "4", Kind: "max_frequency", Value: 0},
	}}

	var applicable, ignored []string
	for _, r := range settings.Applicable() {
		applicable = append(applicable, r.ID)
	}
	for _, r := range settings.Ignored() {
		ignored = append(ignored, r.ID)
	}

	assert.Equal(t, []string{"1", "3"}, applicable)
	assert.Equal(t, []string{"2", "4"}, ignored)
}

func TestMaxAllocationCountRoundsDown(t *testing.T) {
	assert.Equal(t, 2, model.MaxAllocationCount(34, 7))
	assert.Equal(t, 7, model.MaxAllocationCount(100, 7))
}

// A share meant as "not often" must not come out meaning "never".
func TestMaxAllocationCountNeverFallsToZero(t *testing.T) {
	assert.Equal(t, 1, model.MaxAllocationCount(10, 4))
}

// A kind's starting value is one it accepts, so a rule added from the library
// and saved untouched is never refused.
func TestEveryKindStartsAtAValueItAccepts(t *testing.T) {
	for _, k := range model.RuleLibrary {
		rule := model.AllocationRule{Kind: k.ID, Value: k.DefaultValue}
		if k.HasOption() {
			rule.Option = k.Options[0]
		}
		assert.NoError(t, rule.Validate(), k.ID)
	}
}
