package criteria

import (
	rotageneration "github.com/jakechorley/ilford-drop-in/pkg/core/allocator"
)

// ShiftSpreadCriterion optimizes for shifts that are further from previous allocations.
//
// Validity:
//   - No validity constraints (always returns true)
//
// Affinity:
//   - Increases affinity for shifts that are further from already allocated shifts
//   - Considers both current rota allocations and historical shifts
//   - Higher affinity when the shift is more distant from previous allocations
//
// Promotion:
//   - No promotion logic for this criterion
type ShiftSpreadCriterion struct {
	affinityWeight float64
}

// NewShiftSpreadCriterion creates a new ShiftSpreadCriterion with the given affinity weight.
// Group weight is always 0 since this criterion does not promote groups.
func NewShiftSpreadCriterion(affinityWeight float64) *ShiftSpreadCriterion {
	return &ShiftSpreadCriterion{
		affinityWeight: affinityWeight,
	}
}

func (c *ShiftSpreadCriterion) Name() string {
	return "ShiftSpread"
}

func (c *ShiftSpreadCriterion) PromoteVolunteerGroup(state *rotageneration.RotaState, group *rotageneration.VolunteerGroup) float64 {
	// No promotion logic for this criterion
	return 0
}

func (c *ShiftSpreadCriterion) IsShiftValid(state *rotageneration.RotaState, group *rotageneration.VolunteerGroup, shift *rotageneration.Shift) bool {
	// No validity constraints - all shifts are valid
	return true
}

func (c *ShiftSpreadCriterion) CalculateShiftAffinity(state *rotageneration.RotaState, group *rotageneration.VolunteerGroup, shift *rotageneration.Shift) float64 {
	// Calculate the minimum distance to any already allocated shift
	// Higher affinity for shifts that are further away from previous allocations

	shiftIndex := shift.Index
	totalShifts := len(state.Shifts)

	// Get the last historical allocation index (if any)
	lastHistoricalIndex := c.getLastHistoricalIndex(state, group)

	// Calculate maximum possible distance
	// If there's a historical allocation, max distance is from that historical to the last shift in new rota
	// Otherwise, max distance is within the current rota only
	var maxDistance int
	if lastHistoricalIndex >= 0 {
		// Maximum distance from last historical allocation to last shift in new rota
		maxDistance = (len(state.HistoricalShifts) - lastHistoricalIndex - 1) + totalShifts
	} else {
		// No historical allocations - maximum distance is within current rota
		maxDistance = totalShifts - 1
	}

	if maxDistance == 0 {
		return 0.5 // Single shift or no distance possible
	}

	// Check distance to the last historical shift
	distanceFromHistorical := maxDistance
	if lastHistoricalIndex >= 0 {
		// Calculate distance from last historical to current shift
		// The distance is: (number of historical shifts - lastHistoricalIndex) + shiftIndex
		distanceFromHistorical = (len(state.HistoricalShifts) - lastHistoricalIndex - 1) + shiftIndex + 1
	}

	// Find the minimum distance to any allocated shift
	minDistance := distanceFromHistorical // Start with maximum possible distance

	// Check distance to allocated shifts in the current rota
	for _, allocatedIndex := range group.AllocatedShiftIndices {
		distance := shiftIndex - allocatedIndex
		if distance < 0 {
			distance = -distance
		}
		if distance < minDistance {
			minDistance = distance
		}
	}

	// Calculate affinity: larger distances get higher affinity
	// Examples:
	//   - minDistance = 5, maxDistance = 10 → 5/10 = 0.5
	//   - minDistance = 9, maxDistance = 10 → 9/10 = 0.9 (very spread out)
	//   - minDistance = 1, maxDistance = 10 → 1/10 = 0.1 (close to existing allocation)
	affinity := float64(minDistance) / float64(maxDistance)

	return affinity
}

// getLastHistoricalIndex returns the index of the last historical shift this group was allocated to.
// Returns -1 if the group was not allocated to any historical shift.
func (c *ShiftSpreadCriterion) getLastHistoricalIndex(state *rotageneration.RotaState, group *rotageneration.VolunteerGroup) int {
	// Iterate backwards through historical shifts to find the most recent allocation
	for i := len(state.HistoricalShifts) - 1; i >= 0; i-- {
		historicalShift := state.HistoricalShifts[i]
		for _, allocatedGroup := range historicalShift.AllocatedGroups {
			if allocatedGroup.GroupKey == group.GroupKey {
				return i
			}
		}
	}

	return -1
}

func (c *ShiftSpreadCriterion) GroupWeight() float64 {
	return 0
}

func (c *ShiftSpreadCriterion) AffinityWeight() float64 {
	return c.affinityWeight
}

func (c *ShiftSpreadCriterion) ValidateRotaState(state *rotageneration.RotaState) []rotageneration.ShiftValidationError {
	// No validity constraints - all shifts are valid
	var errors []rotageneration.ShiftValidationError
	return errors
}
