package criteria

import (
	"testing"

	"github.com/stretchr/testify/assert"
)


func TestShiftSpreadCriterion_Name(t *testing.T) {
	criterion := NewShiftSpreadCriterion(1.0, 1.0)
	assert.Equal(t, "ShiftSpread", criterion.Name())
}

func TestShiftSpreadCriterion_Weights(t *testing.T) {
	criterion := NewShiftSpreadCriterion(5.0, 10.0)
	assert.Equal(t, 5.0, criterion.GroupWeight())
	assert.Equal(t, 10.0, criterion.AffinityWeight())
}

func TestShiftSpreadCriterion_PromoteVolunteerGroup(t *testing.T) {
	criterion := NewShiftSpreadCriterion(1.0, 1.0)
	state := &RotaState{}
	group := &VolunteerGroup{}

	// No promotion logic
	promotion := criterion.PromoteVolunteerGroup(state, group)
	assert.Equal(t, 0.0, promotion)
}

func TestShiftSpreadCriterion_IsShiftValid(t *testing.T) {
	criterion := NewShiftSpreadCriterion(1.0, 1.0)
	state := &RotaState{
		Shifts: []*Shift{{Index: 0}, {Index: 1}},
	}
	group := &VolunteerGroup{}

	// All shifts should be valid (no constraints)
	assert.True(t, criterion.IsShiftValid(state, group, state.Shifts[0]))
	assert.True(t, criterion.IsShiftValid(state, group, state.Shifts[1]))
}

func TestShiftSpreadCriterion_CalculateShiftAffinity_NoAllocations(t *testing.T) {
	criterion := NewShiftSpreadCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0},
			{Index: 1},
			{Index: 2},
		},
		HistoricalShifts: []*Shift{},
	}

	group := &VolunteerGroup{
		GroupKey:              "group_a",
		AllocatedShiftIndices: []int{},
	}

	// No allocations - all shifts have equal affinity
	affinity := criterion.CalculateShiftAffinity(state, group, state.Shifts[0])
	assert.Equal(t, 1.0, affinity)
}

func TestShiftSpreadCriterion_CalculateShiftAffinity_FartherShiftHigherAffinity(t *testing.T) {
	criterion := NewShiftSpreadCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0},
			{Index: 1},
			{Index: 2},
			{Index: 3},
			{Index: 4},
			{Index: 5},
			{Index: 6},
			{Index: 7},
			{Index: 8},
			{Index: 9},
			{Index: 10},
		},
	}

	group := &VolunteerGroup{
		GroupKey:              "group_a",
		AllocatedShiftIndices: []int{0}, // Allocated to shift 0
	}

	// Shift 1: distance = 1, maxDistance = 10 → 1/10 = 0.1
	affinity1 := criterion.CalculateShiftAffinity(state, group, state.Shifts[1])
	assert.Equal(t, 0.1, affinity1)

	// Shift 5: distance = 5, maxDistance = 10 → 5/10 = 0.5
	affinity5 := criterion.CalculateShiftAffinity(state, group, state.Shifts[5])
	assert.Equal(t, 0.5, affinity5)

	// Shift 10: distance = 10, maxDistance = 10 → 10/10 = 1.0
	affinity10 := criterion.CalculateShiftAffinity(state, group, state.Shifts[10])
	assert.Equal(t, 1.0, affinity10)

	// Farther shifts should have higher affinity
	assert.Less(t, affinity1, affinity5)
	assert.Less(t, affinity5, affinity10)
}

func TestShiftSpreadCriterion_CalculateShiftAffinity_MultipleAllocations(t *testing.T) {
	criterion := NewShiftSpreadCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0},
			{Index: 1},
			{Index: 2},
			{Index: 3},
			{Index: 4},
			{Index: 5},
			{Index: 6},
			{Index: 7},
			{Index: 8},
			{Index: 9},
		},
	}

	group := &VolunteerGroup{
		GroupKey:              "group_a",
		AllocatedShiftIndices: []int{0, 9}, // Allocated to shifts 0 and 9
	}

	// Shift 1: min distance = 1 (to shift 0), maxDistance = 9 → 1/9 = 0.111...
	affinity1 := criterion.CalculateShiftAffinity(state, group, state.Shifts[1])
	assert.InDelta(t, 0.111, affinity1, 0.001)

	// Shift 5: min distance = 4 (to shift 9), maxDistance = 9 → 4/9 = 0.444...
	affinity5 := criterion.CalculateShiftAffinity(state, group, state.Shifts[5])
	assert.InDelta(t, 0.444, affinity5, 0.001)

	// Shift 8: min distance = 1 (to shift 9), maxDistance = 9 → 1/9 = 0.111...
	affinity8 := criterion.CalculateShiftAffinity(state, group, state.Shifts[8])
	assert.InDelta(t, 0.111, affinity8, 0.001)
}

func TestShiftSpreadCriterion_CalculateShiftAffinity_WithHistoricalAllocations(t *testing.T) {
	criterion := NewShiftSpreadCriterion(1.0, 1.0)

	historicalGroup := &VolunteerGroup{
		GroupKey: "group_a",
	}

	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0},
			{Index: 1},
			{Index: 2},
			{Index: 3},
			{Index: 4},
		},
		HistoricalShifts: []*Shift{
			{Index: 0, AllocatedGroups: []*VolunteerGroup{}},
			{Index: 1, AllocatedGroups: []*VolunteerGroup{}},
			{Index: 2, AllocatedGroups: []*VolunteerGroup{historicalGroup}}, // Last historical allocation at index 2
		},
	}

	group := &VolunteerGroup{
		GroupKey:              "group_a",
		AllocatedShiftIndices: []int{}, // No current allocations
	}

	// Distance from last historical (index 2) to shift 0 in new rota:
	// Historical shifts end at index 2, new rota starts at index 3 (absolute)
	// Shift 0 in new rota = absolute index 3
	// Distance = 3 - 2 = 1
	// Max distance = (3 + 4) - 2 = 5 (from historical index 2 to last shift in new rota)
	// Affinity = 1/5 = 0.2
	affinity0 := criterion.CalculateShiftAffinity(state, group, state.Shifts[0])
	assert.Equal(t, 0.2, affinity0)

	// Shift 4 in new rota = absolute index 7
	// Distance = 7 - 2 = 5
	// Max distance = 5
	// Affinity = 5/5 = 1.0
	affinity4 := criterion.CalculateShiftAffinity(state, group, state.Shifts[4])
	assert.Equal(t, 1.0, affinity4)
}

func TestShiftSpreadCriterion_CalculateShiftAffinity_HistoricalAndCurrentAllocations(t *testing.T) {
	criterion := NewShiftSpreadCriterion(1.0, 1.0)

	historicalGroup := &VolunteerGroup{
		GroupKey: "group_a",
	}

	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0},
			{Index: 1},
			{Index: 2},
			{Index: 3},
			{Index: 4},
			{Index: 5},
		},
		HistoricalShifts: []*Shift{
			{Index: 0, AllocatedGroups: []*VolunteerGroup{historicalGroup}}, // Historical at index 0
		},
	}

	group := &VolunteerGroup{
		GroupKey:              "group_a",
		AllocatedShiftIndices: []int{5}, // Current allocation at shift 5
	}

	// For shift 0:
	// Distance to shift 5 = 5
	// Distance to last historical (index 0) = (1 - 0 - 1) + 0 + 1 = 1
	// Min distance = 1
	// Max distance from historical (index 0) to last shift in new rota = (1 - 0 - 1) + 6 = 6
	// Affinity = 1/6 = 0.166...
	affinity0 := criterion.CalculateShiftAffinity(state, group, state.Shifts[0])
	assert.InDelta(t, 0.166, affinity0, 0.001)

	// For shift 3:
	// Distance to shift 5 = 2
	// Distance to last historical (index 0) = (1 - 0 - 1) + 3 + 1 = 4
	// Min distance = 2
	// Max distance = 6
	// Affinity = 2/6 = 0.333...
	affinity3 := criterion.CalculateShiftAffinity(state, group, state.Shifts[3])
	assert.InDelta(t, 0.333, affinity3, 0.001)
}

func TestShiftSpreadCriterion_CalculateShiftAffinity_SingleShiftRota(t *testing.T) {
	criterion := NewShiftSpreadCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0},
		},
		HistoricalShifts: []*Shift{},
	}

	group := &VolunteerGroup{
		GroupKey:              "group_a",
		AllocatedShiftIndices: []int{},
	}

	// Single shift - return neutral affinity
	affinity := criterion.CalculateShiftAffinity(state, group, state.Shifts[0])
	assert.Equal(t, 0.5, affinity)
}

func TestShiftSpreadCriterion_CalculateShiftAffinity_HistoricalButDifferentGroup(t *testing.T) {
	criterion := NewShiftSpreadCriterion(1.0, 1.0)

	otherGroup := &VolunteerGroup{
		GroupKey: "group_b",
	}

	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0},
			{Index: 1},
		},
		HistoricalShifts: []*Shift{
			{Index: 0, AllocatedGroups: []*VolunteerGroup{otherGroup}}, // Different group
		},
	}

	group := &VolunteerGroup{
		GroupKey:              "group_a",
		AllocatedShiftIndices: []int{},
	}

	// Group A has no historical allocations - all equal affinity
	affinity0 := criterion.CalculateShiftAffinity(state, group, state.Shifts[0])
	affinity1 := criterion.CalculateShiftAffinity(state, group, state.Shifts[1])
	assert.Equal(t, 1.0, affinity0, affinity1)
}

func TestShiftSpreadCriterion_CalculateShiftAffinity_MultipleHistoricalAllocations(t *testing.T) {
	criterion := NewShiftSpreadCriterion(1.0, 1.0)

	historicalGroup := &VolunteerGroup{
		GroupKey: "group_a",
	}

	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0},
			{Index: 1},
			{Index: 2},
		},
		HistoricalShifts: []*Shift{
			{Index: 0, AllocatedGroups: []*VolunteerGroup{historicalGroup}}, // First historical
			{Index: 1, AllocatedGroups: []*VolunteerGroup{}},
			{Index: 2, AllocatedGroups: []*VolunteerGroup{historicalGroup}}, // Last historical (most recent)
		},
	}

	group := &VolunteerGroup{
		GroupKey:              "group_a",
		AllocatedShiftIndices: []int{},
	}

	// Should use the last (most recent) historical allocation at index 2
	// Distance from index 2 to shift 0 in new rota = (3) - 2 = 1
	// Max distance = (3 + 2) - 2 = 3
	// Affinity = 1/3 = 0.333...
	affinity := criterion.CalculateShiftAffinity(state, group, state.Shifts[0])
	assert.InDelta(t, 0.333, affinity, 0.001)
}
