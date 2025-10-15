package rotageneration

import "fmt"

// NoDoubleShiftsCriterion prevents allocation to shifts immediately adjacent to already allocated shifts.
//
// Validity:
//   - Returns false if allocating to this shift would create a double shift
//     (shift index is immediately before or after an already allocated shift)
//
// Affinity:
//   - Increases affinity for shifts that preserve more valid shift options for future allocations
//   - Lower affinity for shifts that would reduce the number of valid shifts available
//
// Promotion:
//   - No promotion logic for this criterion
type NoDoubleShiftsCriterion struct {
	groupWeight    float64
	affinityWeight float64
}

// NewNoDoubleShiftsCriterion creates a new NoDoubleShiftsCriterion with the given weights
func NewNoDoubleShiftsCriterion(groupWeight, affinityWeight float64) *NoDoubleShiftsCriterion {
	return &NoDoubleShiftsCriterion{
		groupWeight:    groupWeight,
		affinityWeight: affinityWeight,
	}
}

func (c *NoDoubleShiftsCriterion) Name() string {
	return "NoDoubleShifts"
}

func (c *NoDoubleShiftsCriterion) PromoteVolunteerGroup(state *RotaState, group *VolunteerGroup) float64 {
	// No promotion logic for this criterion
	return 0
}

func (c *NoDoubleShiftsCriterion) IsShiftValid(state *RotaState, group *VolunteerGroup, shift *Shift) bool {
	// Check if the group is already allocated to an adjacent shift
	shiftIndex := shift.Index

	// Special case: if this is the first shift in the new rota (index 0),
	// check if the group was allocated to the last historical shift
	if shiftIndex == 0 && len(state.HistoricalShifts) > 0 {
		lastHistoricalShift := state.HistoricalShifts[len(state.HistoricalShifts)-1]
		// Check if this group was allocated to the last historical shift
		for _, allocatedGroup := range lastHistoricalShift.AllocatedGroups {
			if allocatedGroup.GroupKey == group.GroupKey {
				return false
			}
		}
	}

	// Check previous shift (index - 1) in the current rota
	if shiftIndex > 0 {
		prevShiftIndex := shiftIndex - 1
		if group.IsAllocated(prevShiftIndex) {
			return false
		}
	}

	// Check next shift (index + 1)
	if shiftIndex < len(state.Shifts)-1 {
		nextShiftIndex := shiftIndex + 1
		if group.IsAllocated(nextShiftIndex) {
			return false
		}
	}

	return true
}

func (c *NoDoubleShiftsCriterion) CalculateShiftAffinity(state *RotaState, group *VolunteerGroup, shift *Shift) float64 {
	// Calculate how many valid shift options would remain after this allocation
	// We want to prefer shifts that preserve more options for future allocations

	shiftIndex := shift.Index
	totalShifts := len(state.Shifts)

	// Check if group was allocated to the last historical shift
	wasAllocatedToLastHistoricalShift := false
	if len(state.HistoricalShifts) > 0 {
		lastHistoricalShift := state.HistoricalShifts[len(state.HistoricalShifts)-1]
		for _, allocatedGroup := range lastHistoricalShift.AllocatedGroups {
			if allocatedGroup.GroupKey == group.GroupKey {
				wasAllocatedToLastHistoricalShift = true
				break
			}
		}
	}

	// Count currently valid shifts for this group (excluding the current shift)
	currentlyValidCount := 0
	for i := 0; i < totalShifts; i++ {
		if i == shiftIndex {
			continue // Don't count the shift we're considering
		}

		// Check if this shift is available and not yet allocated
		if !group.IsAvailable(i) || group.IsAllocated(i) {
			continue
		}

		// Check if it would be valid according to double shifts rule
		// For shift 0, check against last historical shift
		if i == 0 && wasAllocatedToLastHistoricalShift {
			continue // Would be a double shift with historical
		}
		if i > 0 && group.IsAllocated(i-1) {
			continue // Would be a double shift
		}
		if i < totalShifts-1 && group.IsAllocated(i+1) {
			continue // Would be a double shift
		}

		currentlyValidCount++
	}

	// Count how many valid shifts would remain if we allocate to this shift
	remainingValidCount := 0
	for i := 0; i < totalShifts; i++ {
		if i == shiftIndex {
			continue // Don't count the shift we're allocating to
		}

		// Check if this shift is available and not yet allocated
		if !group.IsAvailable(i) || group.IsAllocated(i) {
			continue
		}

		// Check if it would be valid after allocating to shiftIndex
		// A shift becomes invalid if it's adjacent to the shift we're allocating
		if i == shiftIndex-1 || i == shiftIndex+1 {
			continue // Would become invalid after this allocation
		}

		// Check if it would be valid according to existing allocations
		// For shift 0, check against last historical shift
		if i == 0 && wasAllocatedToLastHistoricalShift {
			continue
		}
		if i > 0 && group.IsAllocated(i-1) {
			continue
		}
		if i < totalShifts-1 && group.IsAllocated(i+1) {
			continue
		}

		remainingValidCount++
	}

	// If there are no currently valid shifts, return 0
	if currentlyValidCount == 0 {
		return 0
	}

	// Calculate affinity as the proportion of valid shifts that would remain plus the allocated shift
	// Higher affinity when more options are preserved
	// Examples:
	//   - 5 valid shifts currently, 3 would remain → 3/5 = 0.6 (good)
	//   - 5 valid shifts currently, 2 would remain → 2/5 = 0.4 (worse)
	//   - 3 valid shifts currently, 0 would remain → 0/3 = 0.0 (bad - would strand the group)
	affinity := float64(remainingValidCount) / float64(currentlyValidCount)

	return affinity
}

func (c *NoDoubleShiftsCriterion) GroupWeight() float64 {
	return c.groupWeight
}

func (c *NoDoubleShiftsCriterion) AffinityWeight() float64 {
	return c.affinityWeight
}

func (c *NoDoubleShiftsCriterion) ValidateRotaState(state *RotaState) []ShiftValidationError {
	var errors []ShiftValidationError

	// Check each shift for double shifts
	for i := 0; i < len(state.Shifts); i++ {
		shift := state.Shifts[i]

		// Build a map of groups allocated to this shift
		currentGroups := make(map[string]bool)
		for _, group := range shift.AllocatedGroups {
			currentGroups[group.GroupKey] = true
		}

		// Check against previous shift
		if i > 0 {
			prevShift := state.Shifts[i-1]
			for _, prevGroup := range prevShift.AllocatedGroups {
				if currentGroups[prevGroup.GroupKey] {
					errors = append(errors, ShiftValidationError{
						ShiftIndex:    shift.Index,
						ShiftDate:     shift.Date,
						CriterionName: c.Name(),
						Description:   fmt.Sprintf("Group '%s' is allocated to adjacent shifts %d and %d", prevGroup.GroupKey, i-1, i),
					})
				}
			}
		}

		// Check against historical last shift (only for first shift in new rota)
		if i == 0 && len(state.HistoricalShifts) > 0 {
			lastHistoricalShift := state.HistoricalShifts[len(state.HistoricalShifts)-1]
			for _, historicalGroup := range lastHistoricalShift.AllocatedGroups {
				if currentGroups[historicalGroup.GroupKey] {
					errors = append(errors, ShiftValidationError{
						ShiftIndex:    shift.Index,
						ShiftDate:     shift.Date,
						CriterionName: c.Name(),
						Description:   fmt.Sprintf("Group '%s' is allocated to last historical shift and first shift of new rota (double shift across rota boundary)", historicalGroup.GroupKey),
					})
				}
			}
		}
	}

	return errors
}
