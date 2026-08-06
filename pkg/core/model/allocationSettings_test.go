package model_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

// The four switchable rules, pinned by name. These strings are the contract
// with the Python constraint registry (constraints/__init__.py), which is the
// authority on what they may mean; this list is what an admin is offered.
// pyallocator's own test pins the same four from the other side.
func TestSwitchableConstraintsAreTheFourOptionalRules(t *testing.T) {
	var names []string
	for _, c := range model.SwitchableConstraints {
		names = append(names, c.Name)
		assert.NotEmpty(t, c.Label, "%s needs a label an admin can read", c.Name)
		assert.NotEmpty(t, c.Description, "%s needs to say what it does", c.Name)
	}

	assert.Equal(t, []string{
		"max_frequency",
		"male_required",
		"no_back_to_back",
		"one_shift_per_month",
	}, names)
}

// A rule nobody has answered is off. Empty settings are the ordinary first
// state of a deployment, not a fault, and they allocate — with no optional
// rules applied.
func TestUnansweredConstraintsAreOff(t *testing.T) {
	var settings model.AllocationSettings

	assert.False(t, settings.IsEnabled("no_back_to_back"))
	assert.Empty(t, settings.EnabledConstraints())
}

// The enabled list is in registry order whatever order the answers were
// stored in, so what reaches the solver reads the same every time.
func TestEnabledConstraintsAreInRegistryOrder(t *testing.T) {
	settings := model.AllocationSettings{
		Enabled: map[string]bool{
			"no_back_to_back": true,
			"max_frequency":   true,
			"male_required":   false,
		},
	}

	assert.Equal(t, []string{"max_frequency", "no_back_to_back"}, settings.EnabledConstraints())
	assert.True(t, settings.IsEnabled("max_frequency"))
	assert.False(t, settings.IsEnabled("male_required"), "an explicit false is off")
}

// An answer for a rule this build does not have is dropped rather than
// refused, and named so an operator can see why it stopped applying.
func TestUnknownConstraintsAreIgnoredAndNamed(t *testing.T) {
	settings := model.AllocationSettings{
		Enabled: map[string]bool{"max_frequency": true, "phase_of_the_moon": true},
	}

	assert.Equal(t, []string{"max_frequency"}, settings.EnabledConstraints())
	assert.Equal(t, []string{"phase_of_the_moon"}, settings.UnknownConstraints())
}

// A stored answer that is false for an unknown rule is not worth naming: it
// asks for nothing, so dropping it changes nothing.
func TestUnknownConstraintsOnlyNamesEnabledOnes(t *testing.T) {
	settings := model.AllocationSettings{
		Enabled: map[string]bool{"phase_of_the_moon": false},
	}

	assert.Empty(t, settings.UnknownConstraints())
}

// The max-frequency rule carries a value as well as a switch, so it is the one
// toggle that can be incoherent: on, with no share of the rota to cap at.
func TestMissingAllocationSettingsNamesAnEnabledFrequencyWithNoValue(t *testing.T) {
	settings := model.AllocationSettings{Enabled: map[string]bool{"max_frequency": true}}

	assert.Equal(t, []string{"the maximum allocation frequency"}, settings.Missing())

	settings.MaxFrequency = 0.34
	assert.Empty(t, settings.Missing())
}

// Off, the value is not asked for — an admin who switches the rule off has not
// left anything unfilled.
func TestMissingAllocationSettingsIgnoresADisabledFrequency(t *testing.T) {
	var settings model.AllocationSettings

	assert.Empty(t, settings.Missing())
}

// The cap the solver receives is a count of shifts, not a share. It rounds the
// way the config-derived one always did — down, so a third of seven shifts is
// two rather than three.
func TestMaxAllocationCount(t *testing.T) {
	settings := model.AllocationSettings{
		Enabled:      map[string]bool{"max_frequency": true},
		MaxFrequency: 0.34,
	}

	assert.Equal(t, 2, settings.MaxAllocationCount(7))
}

// With the rule off the cap is every shift in the rota: a number the solver
// cannot trip over, so nothing depends on Python having left the constraint
// out as well.
func TestMaxAllocationCountWithTheRuleOff(t *testing.T) {
	var settings model.AllocationSettings

	assert.Equal(t, 7, settings.MaxAllocationCount(7))
}

// A rota one volunteer could work entirely still caps at one shift rather than
// at none: a cap of zero is a rota nobody may work, which is never what an
// admin setting a frequency meant.
func TestMaxAllocationCountNeverFallsToZero(t *testing.T) {
	settings := model.AllocationSettings{
		Enabled:      map[string]bool{"max_frequency": true},
		MaxFrequency: 0.1,
	}

	require.Equal(t, 1, settings.MaxAllocationCount(4))
}
