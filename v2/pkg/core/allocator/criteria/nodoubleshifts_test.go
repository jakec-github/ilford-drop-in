package criteria

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNoDoubleShiftsCriterion_Name(t *testing.T) {
	criterion := NewNoDoubleShiftsCriterion(1.0, 1.0)
	assert.Equal(t, "NoDoubleShifts", criterion.Name())
}

func TestNoDoubleShiftsCriterion_Weights(t *testing.T) {
	criterion := NewNoDoubleShiftsCriterion(5.0, 10.0)
	assert.Equal(t, 5.0, criterion.GroupWeight())
	assert.Equal(t, 10.0, criterion.AffinityWeight())
}

func TestNoDoubleShiftsCriterion_PromoteVolunteerGroup(t *testing.T) {
	criterion := NewNoDoubleShiftsCriterion(1.0, 1.0)
	state := &RotaState{}
	group := &VolunteerGroup{}

	// No promotion logic
	promotion := criterion.PromoteVolunteerGroup(state, group)
	assert.Equal(t, 0.0, promotion)
}

func TestNoDoubleShiftsCriterion_IsShiftValid_NoAdjacentAllocations(t *testing.T) {
	criterion := NewNoDoubleShiftsCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0},
			{Index: 1},
			{Index: 2},
			{Index: 3},
			{Index: 4},
		},
	}

	group := &VolunteerGroup{
		AllocatedShiftIndices: []int{0, 3}, // Allocated to shifts 0 and 3
	}

	// Shift 1 is adjacent to allocated shift 0 - invalid
	assert.False(t, criterion.IsShiftValid(state, group, state.Shifts[1]))

	// Shift 2 is adjacent to allocated shift 3 - invalid
	assert.False(t, criterion.IsShiftValid(state, group, state.Shifts[2]))

	// Shift 4 is adjacent to allocated shift 3 - invalid
	assert.False(t, criterion.IsShiftValid(state, group, state.Shifts[4]))
}

func TestNoDoubleShiftsCriterion_IsShiftValid_NotAdjacent(t *testing.T) {
	criterion := NewNoDoubleShiftsCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0},
			{Index: 1},
			{Index: 2},
			{Index: 3},
			{Index: 4},
		},
	}

	group := &VolunteerGroup{
		AllocatedShiftIndices: []int{1}, // Allocated to shift 1
	}

	// Shift 3 is not adjacent to shift 1 - valid
	assert.True(t, criterion.IsShiftValid(state, group, state.Shifts[3]))

	// Shift 4 is not adjacent to shift 1 - valid
	assert.True(t, criterion.IsShiftValid(state, group, state.Shifts[4]))
}

func TestNoDoubleShiftsCriterion_IsShiftValid_FirstShift(t *testing.T) {
	criterion := NewNoDoubleShiftsCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0},
			{Index: 1},
			{Index: 2},
		},
	}

	group := &VolunteerGroup{
		AllocatedShiftIndices: []int{1}, // Allocated to shift 1
	}

	// Shift 0 is adjacent to allocated shift 1 - invalid
	assert.False(t, criterion.IsShiftValid(state, group, state.Shifts[0]))
}

func TestNoDoubleShiftsCriterion_IsShiftValid_LastShift(t *testing.T) {
	criterion := NewNoDoubleShiftsCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0},
			{Index: 1},
			{Index: 2},
		},
	}

	group := &VolunteerGroup{
		AllocatedShiftIndices: []int{1}, // Allocated to shift 1
	}

	// Shift 2 is adjacent to allocated shift 1 - invalid
	assert.False(t, criterion.IsShiftValid(state, group, state.Shifts[2]))
}

func TestNoDoubleShiftsCriterion_IsShiftValid_NoAllocations(t *testing.T) {
	criterion := NewNoDoubleShiftsCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0},
			{Index: 1},
			{Index: 2},
		},
	}

	group := &VolunteerGroup{
		AllocatedShiftIndices: []int{}, // No allocations yet
	}

	// All shifts should be valid when there are no allocations
	assert.True(t, criterion.IsShiftValid(state, group, state.Shifts[0]))
	assert.True(t, criterion.IsShiftValid(state, group, state.Shifts[1]))
	assert.True(t, criterion.IsShiftValid(state, group, state.Shifts[2]))
}

func TestNoDoubleShiftsCriterion_CalculateShiftAffinity_PreservesAllOptions(t *testing.T) {
	criterion := NewNoDoubleShiftsCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0},
			{Index: 1},
			{Index: 2},
			{Index: 3},
			{Index: 4},
		},
	}

	group := &VolunteerGroup{
		AvailableShiftIndices: []int{0, 1, 2, 3, 4},
		AllocatedShiftIndices: []int{}, // No allocations yet
	}

	// Allocating to shift 2 would make shifts 1 and 3 invalid
	// Currently valid: 0, 1, 2, 3, 4 (5 total, excluding shift 2 itself = 4)
	// After allocation: 0, 4 (2 remain valid)
	// Affinity: 2/4 = 0.5
	affinity := criterion.CalculateShiftAffinity(state, group, state.Shifts[2])
	assert.Equal(t, 0.5, affinity)
}

func TestNoDoubleShiftsCriterion_CalculateShiftAffinity_EdgeShiftPreservesMore(t *testing.T) {
	criterion := NewNoDoubleShiftsCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0},
			{Index: 1},
			{Index: 2},
			{Index: 3},
			{Index: 4},
		},
	}

	group := &VolunteerGroup{
		AvailableShiftIndices: []int{0, 1, 2, 3, 4},
		AllocatedShiftIndices: []int{}, // No allocations yet
	}

	// Allocating to shift 0 would only make shift 1 invalid
	// Currently valid: 0, 1, 2, 3, 4 (5 total, excluding shift 0 itself = 4)
	// After allocation: 2, 3, 4 (3 remain valid)
	// Affinity: 3/4 = 0.75
	affinityEdge := criterion.CalculateShiftAffinity(state, group, state.Shifts[0])
	assert.Equal(t, 0.75, affinityEdge)

	// Allocating to shift 2 (middle) would make shifts 1 and 3 invalid
	// Affinity: 2/4 = 0.5
	affinityMiddle := criterion.CalculateShiftAffinity(state, group, state.Shifts[2])
	assert.Equal(t, 0.5, affinityMiddle)

	// Edge shifts should have higher affinity (preserve more options)
	assert.Greater(t, affinityEdge, affinityMiddle)
}

func TestNoDoubleShiftsCriterion_CalculateShiftAffinity_WithExistingAllocations(t *testing.T) {
	criterion := NewNoDoubleShiftsCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0},
			{Index: 1},
			{Index: 2},
			{Index: 3},
			{Index: 4},
			{Index: 5},
			{Index: 6},
		},
	}

	group := &VolunteerGroup{
		AvailableShiftIndices: []int{0, 1, 2, 3, 4, 5, 6},
		AllocatedShiftIndices: []int{1, 5}, // Already allocated to shifts 1 and 5
	}

	// Currently valid shifts (excluding adjacents to 1 and 5): 3 (1 valid)
	// If we allocate to shift 3:
	// - Remaining: none
	// Affinity: 0/1 = 0.0
	// This low affinity is ok as it is the only shift that will pass the validity test
	affinity := criterion.CalculateShiftAffinity(state, group, state.Shifts[3])
	assert.Equal(t, 0.0, affinity)
}

func TestNoDoubleShiftsCriterion_CalculateShiftAffinity_NoValidShiftsRemaining(t *testing.T) {
	criterion := NewNoDoubleShiftsCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0},
			{Index: 1},
			{Index: 2},
		},
	}

	group := &VolunteerGroup{
		AvailableShiftIndices: []int{0, 1, 2},
		AllocatedShiftIndices: []int{0, 2}, // Already allocated to shifts 0 and 2
	}

	// Only shift 1 is available and not allocated
	// But it's adjacent to both 0 and 2, so it's not valid
	// Actually, let's check if it's considering shift 1:
	// Currently valid shifts (not adjacent to 0 or 2 and not allocated): none
	// If there are no currently valid shifts, affinity should be 0
	affinity := criterion.CalculateShiftAffinity(state, group, state.Shifts[1])
	assert.Equal(t, 0.0, affinity)
}

func TestNoDoubleShiftsCriterion_CalculateShiftAffinity_LastValidShift(t *testing.T) {
	criterion := NewNoDoubleShiftsCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0},
			{Index: 1},
			{Index: 2},
			{Index: 3},
		},
	}

	group := &VolunteerGroup{
		AvailableShiftIndices: []int{0, 1, 2, 3},
		AllocatedShiftIndices: []int{0}, // Already allocated to shift 0
	}

	// Currently valid shifts (not adjacent to 0 and not allocated): 2, 3
	// If we allocate to shift 2:
	// - Shift 3 becomes invalid (adjacent to 2)
	// - Remaining: none (shift 1 is adjacent to 0)
	// Affinity: 0/2 = 0.0
	affinity2 := criterion.CalculateShiftAffinity(state, group, state.Shifts[2])
	assert.Equal(t, 0.0, affinity2)

	// If we allocate to shift 3:
	// - Shift 2 becomes invalid (adjacent to 3)
	// - Remaining: none
	// Affinity: 0/2 = 0.0
	affinity3 := criterion.CalculateShiftAffinity(state, group, state.Shifts[3])
	assert.Equal(t, 0.0, affinity3)
}

func TestNoDoubleShiftsCriterion_CalculateShiftAffinity_OnlyUnavailableShiftsWouldBeInvalidated(t *testing.T) {
	criterion := NewNoDoubleShiftsCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0},
			{Index: 1},
			{Index: 2},
			{Index: 3},
			{Index: 4},
		},
	}

	group := &VolunteerGroup{
		AvailableShiftIndices: []int{0, 2, 4}, // Not available for shifts 1 and 3
		AllocatedShiftIndices: []int{},
	}

	// Currently valid: 0, 2, 4 (excluding shift 2 itself = 2)
	// If we allocate to shift 2:
	// - Shifts 1 and 3 would become invalid, but they're not available anyway
	// - Remaining valid: 0, 4 (both still valid)
	// Affinity: 2/2 = 1.0
	affinity := criterion.CalculateShiftAffinity(state, group, state.Shifts[2])
	assert.Equal(t, 1.0, affinity)
}

func TestNoDoubleShiftsCriterion_IsShiftValid_FirstShiftAfterHistorical(t *testing.T) {
	criterion := NewNoDoubleShiftsCriterion(1.0, 1.0)

	group := &VolunteerGroup{
		GroupKey:              "group_a",
		AllocatedShiftIndices: []int{},
	}

	// Group was allocated to the last historical shift
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
			{Index: 0, AllocatedGroups: []*VolunteerGroup{}},
			{Index: 1, AllocatedGroups: []*VolunteerGroup{historicalGroup}}, // Group allocated to last historical shift
		},
	}

	// Shift 0 is adjacent to the last historical shift - invalid
	assert.False(t, criterion.IsShiftValid(state, group, state.Shifts[0]))

	// Shift 1 is not adjacent to the last historical shift - valid
	assert.True(t, criterion.IsShiftValid(state, group, state.Shifts[1]))
}

func TestNoDoubleShiftsCriterion_IsShiftValid_FirstShiftNoHistorical(t *testing.T) {
	criterion := NewNoDoubleShiftsCriterion(1.0, 1.0)

	group := &VolunteerGroup{
		GroupKey:              "group_a",
		AllocatedShiftIndices: []int{},
	}

	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0},
			{Index: 1},
		},
		HistoricalShifts: []*Shift{}, // No historical shifts
	}

	// Shift 0 should be valid when there are no historical shifts
	assert.True(t, criterion.IsShiftValid(state, group, state.Shifts[0]))
}

func TestNoDoubleShiftsCriterion_IsShiftValid_FirstShiftGroupNotInHistorical(t *testing.T) {
	criterion := NewNoDoubleShiftsCriterion(1.0, 1.0)

	group := &VolunteerGroup{
		GroupKey:              "group_a",
		AllocatedShiftIndices: []int{},
	}

	// Different group was allocated to the last historical shift
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

	// Shift 0 should be valid because this group wasn't in the last historical shift
	assert.True(t, criterion.IsShiftValid(state, group, state.Shifts[0]))
}

func TestNoDoubleShiftsCriterion_CalculateShiftAffinity_WithHistoricalShift(t *testing.T) {
	criterion := NewNoDoubleShiftsCriterion(1.0, 1.0)

	group := &VolunteerGroup{
		GroupKey:              "group_a",
		AvailableShiftIndices: []int{0, 1, 2, 3},
		AllocatedShiftIndices: []int{},
	}

	// Group was allocated to the last historical shift
	historicalGroup := &VolunteerGroup{
		GroupKey: "group_a",
	}

	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0},
			{Index: 1},
			{Index: 2},
			{Index: 3},
		},
		HistoricalShifts: []*Shift{
			{Index: 0, AllocatedGroups: []*VolunteerGroup{historicalGroup}}, // Last historical shift
		},
	}

	// Currently valid shifts (shift 0 is invalid due to historical): 1, 2, 3
	// Excluding shift 1 itself: 2, 3 (2 total)
	// If we allocate to shift 1:
	// - Shift 0 is already invalid (adjacent to historical)
	// - Shift 2 becomes invalid (adjacent to 1)
	// - Remaining valid: 3 (1 shift)
	// Affinity: 1/2 = 0.5
	affinity := criterion.CalculateShiftAffinity(state, group, state.Shifts[1])
	assert.Equal(t, 0.5, affinity)
}

func TestNoDoubleShiftsCriterion_CalculateShiftAffinity_HistoricalBlocksFirstShift(t *testing.T) {
	criterion := NewNoDoubleShiftsCriterion(1.0, 1.0)

	group := &VolunteerGroup{
		GroupKey:              "group_a",
		AvailableShiftIndices: []int{0, 1, 2},
		AllocatedShiftIndices: []int{},
	}

	// Group was allocated to the last historical shift
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
			{Index: 0, AllocatedGroups: []*VolunteerGroup{historicalGroup}},
		},
	}

	// Currently valid shifts (0 is blocked by historical): 1, 2 (2 total)
	// If we allocate to shift 2:
	// - Shift 1 becomes invalid (adjacent to 2)
	// - Shift 0 is already invalid (adjacent to historical)
	// - Remaining valid: 0
	// Affinity: 0/2 = 0.0
	affinity := criterion.CalculateShiftAffinity(state, group, state.Shifts[2])
	assert.Equal(t, 0.0, affinity)
}

func TestNoDoubleShiftsCriterion_ValidateRotaState_NoDoubleShifts(t *testing.T) {
	criterion := NewNoDoubleShiftsCriterion(1.0, 1.0)

	groupA := &VolunteerGroup{GroupKey: "group_a"}
	groupB := &VolunteerGroup{GroupKey: "group_b"}
	groupC := &VolunteerGroup{GroupKey: "group_c"}

	state := &RotaState{
		Shifts: []*Shift{
			{
				Index:           0,
				Date:            "2024-01-01",
				AllocatedGroups: []*VolunteerGroup{groupA},
			},
			{
				Index:           1,
				Date:            "2024-01-08",
				AllocatedGroups: []*VolunteerGroup{groupB},
			},
			{
				Index:           2,
				Date:            "2024-01-15",
				AllocatedGroups: []*VolunteerGroup{groupA, groupC},
			},
			{
				Index:           3,
				Date:            "2024-01-22",
				AllocatedGroups: []*VolunteerGroup{groupB},
			},
		},
	}

	errors := criterion.ValidateRotaState(state)
	assert.Empty(t, errors, "Should have no errors when no groups have adjacent allocations")
}

func TestNoDoubleShiftsCriterion_ValidateRotaState_DetectsDoubleShift(t *testing.T) {
	criterion := NewNoDoubleShiftsCriterion(1.0, 1.0)

	groupA := &VolunteerGroup{GroupKey: "group_a"}
	groupB := &VolunteerGroup{GroupKey: "group_b"}
	groupC := &VolunteerGroup{GroupKey: "group_c"}

	state := &RotaState{
		Shifts: []*Shift{
			{
				Index:           0,
				Date:            "2024-01-01",
				AllocatedGroups: []*VolunteerGroup{groupA},
			},
			{
				Index:           1,
				Date:            "2024-01-08",
				AllocatedGroups: []*VolunteerGroup{groupA, groupB}, // group_a has double shift (0 and 1)
			},
			{
				Index:           2,
				Date:            "2024-01-15",
				AllocatedGroups: []*VolunteerGroup{groupC}, // Different group, no double shift
			},
		},
	}

	errors := criterion.ValidateRotaState(state)
	assert.Len(t, errors, 1, "Should detect one double shift violation")

	assert.Equal(t, 1, errors[0].ShiftIndex)
	assert.Equal(t, "2024-01-08", errors[0].ShiftDate)
	assert.Equal(t, "NoDoubleShifts", errors[0].CriterionName)
	assert.Contains(t, errors[0].Description, "group_a")
	assert.Contains(t, errors[0].Description, "adjacent shifts")
}

func TestNoDoubleShiftsCriterion_ValidateRotaState_MultipleDoubleShifts(t *testing.T) {
	criterion := NewNoDoubleShiftsCriterion(1.0, 1.0)

	groupA := &VolunteerGroup{GroupKey: "group_a"}
	groupB := &VolunteerGroup{GroupKey: "group_b"}

	state := &RotaState{
		Shifts: []*Shift{
			{
				Index:           0,
				Date:            "2024-01-01",
				AllocatedGroups: []*VolunteerGroup{groupA},
			},
			{
				Index:           1,
				Date:            "2024-01-08",
				AllocatedGroups: []*VolunteerGroup{groupA, groupB}, // Both groups have double shifts
			},
			{
				Index:           2,
				Date:            "2024-01-15",
				AllocatedGroups: []*VolunteerGroup{groupB}, // group_b continues double shift
			},
		},
	}

	errors := criterion.ValidateRotaState(state)
	assert.Len(t, errors, 2, "Should detect two double shift violations")

	// First error: group_a allocated to shifts 0 and 1
	assert.Equal(t, 1, errors[0].ShiftIndex)
	assert.Contains(t, errors[0].Description, "group_a")

	// Second error: group_b allocated to shifts 1 and 2
	assert.Equal(t, 2, errors[1].ShiftIndex)
	assert.Contains(t, errors[1].Description, "group_b")
}

func TestNoDoubleShiftsCriterion_ValidateRotaState_HistoricalBoundary(t *testing.T) {
	criterion := NewNoDoubleShiftsCriterion(1.0, 1.0)

	groupA := &VolunteerGroup{GroupKey: "group_a"}
	groupB := &VolunteerGroup{GroupKey: "group_b"}

	state := &RotaState{
		Shifts: []*Shift{
			{
				Index:           0,
				Date:            "2024-02-01",
				AllocatedGroups: []*VolunteerGroup{groupA}, // group_a double shift across boundary
			},
			{
				Index:           1,
				Date:            "2024-02-08",
				AllocatedGroups: []*VolunteerGroup{groupB},
			},
		},
		HistoricalShifts: []*Shift{
			{
				Index:           0,
				Date:            "2024-01-01",
				AllocatedGroups: []*VolunteerGroup{groupB},
			},
			{
				Index:           1,
				Date:            "2024-01-25",
				AllocatedGroups: []*VolunteerGroup{groupA}, // Last historical shift
			},
		},
	}

	errors := criterion.ValidateRotaState(state)
	assert.Len(t, errors, 1, "Should detect double shift across rota boundary")

	assert.Equal(t, 0, errors[0].ShiftIndex)
	assert.Equal(t, "2024-02-01", errors[0].ShiftDate)
	assert.Contains(t, errors[0].Description, "group_a")
	assert.Contains(t, errors[0].Description, "rota boundary")
}

func TestNoDoubleShiftsCriterion_ValidateRotaState_NoHistoricalShifts(t *testing.T) {
	criterion := NewNoDoubleShiftsCriterion(1.0, 1.0)

	groupA := &VolunteerGroup{GroupKey: "group_a"}

	state := &RotaState{
		Shifts: []*Shift{
			{
				Index:           0,
				Date:            "2024-01-01",
				AllocatedGroups: []*VolunteerGroup{groupA},
			},
			{
				Index:           1,
				Date:            "2024-01-08",
				AllocatedGroups: []*VolunteerGroup{groupA},
			},
		},
		HistoricalShifts: []*Shift{}, // No historical data
	}

	errors := criterion.ValidateRotaState(state)
	assert.Len(t, errors, 1, "Should detect double shift within current rota")
	assert.Contains(t, errors[0].Description, "adjacent shifts")
}

func TestNoDoubleShiftsCriterion_ValidateRotaState_GroupNotInHistorical(t *testing.T) {
	criterion := NewNoDoubleShiftsCriterion(1.0, 1.0)

	groupA := &VolunteerGroup{GroupKey: "group_a"}
	groupB := &VolunteerGroup{GroupKey: "group_b"}

	state := &RotaState{
		Shifts: []*Shift{
			{
				Index:           0,
				Date:            "2024-02-01",
				AllocatedGroups: []*VolunteerGroup{groupA}, // Different group, no violation
			},
		},
		HistoricalShifts: []*Shift{
			{
				Index:           0,
				Date:            "2024-01-25",
				AllocatedGroups: []*VolunteerGroup{groupB}, // Last historical shift
			},
		},
	}

	errors := criterion.ValidateRotaState(state)
	assert.Empty(t, errors, "Should not detect violation when different group is in historical shift")
}
