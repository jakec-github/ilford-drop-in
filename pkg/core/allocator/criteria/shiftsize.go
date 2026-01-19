package criteria

import (
	"fmt"

	rotageneration "github.com/jakechorley/ilford-drop-in/pkg/core/allocator"
)

// ShiftSizeCriterion prevents overfilling of shifts and optimizes for unpopular shifts.
//
// Validity:
//   - Returns false if allocating the group would cause the shift to exceed its size
//   - Team leads don't count toward shift size, so they're excluded from the calculation
//
// Affinity:
//   - Increases affinity for shifts with more remaining capacity (unpopular shifts)
//   - Only considers ordinary volunteers (non-team leads) in the group
//   - Returns 0 if the group has no ordinary volunteers (team lead only)
//   - In resource-constrained scenarios (when a complete rota is not possible),
//     adds a deficit ratio to spread volunteers more evenly across shifts
type ShiftSizeCriterion struct {
	groupWeight    float64
	affinityWeight float64
}

// NewShiftSizeCriterion creates a new ShiftSizeCriterion with the given weights
func NewShiftSizeCriterion(groupWeight, affinityWeight float64) *ShiftSizeCriterion {
	return &ShiftSizeCriterion{
		groupWeight:    groupWeight,
		affinityWeight: affinityWeight,
	}
}

func (c *ShiftSizeCriterion) Name() string {
	return "ShiftSize"
}

func (c *ShiftSizeCriterion) PromoteVolunteerGroup(state *rotageneration.RotaState, group *rotageneration.VolunteerGroup) float64 {
	// No promotion logic for this criterion
	return 0
}

func (c *ShiftSizeCriterion) IsShiftValid(state *rotageneration.RotaState, group *rotageneration.VolunteerGroup, shift *rotageneration.Shift) bool {
	// Count ordinary volunteers in the group (exclude team leads)
	ordinaryVolunteerCount := group.OrdinaryVolunteerCount()

	// Calculate how many spots are available in the shift
	remainingCapacity := shift.RemainingCapacity()

	// Invalid if adding this group's ordinary volunteers would exceed shift size
	return remainingCapacity >= ordinaryVolunteerCount
}

func (c *ShiftSizeCriterion) CalculateShiftAffinity(state *rotageneration.RotaState, group *rotageneration.VolunteerGroup, shift *rotageneration.Shift) float64 {
	// Count ordinary volunteers in the group (exclude team leads)
	ordinaryVolunteerCount := group.OrdinaryVolunteerCount()

	// If no ordinary volunteers in the group, no affinity contribution
	if ordinaryVolunteerCount == 0 {
		return 0
	}

	// Calculate remaining capacity and remaining available volunteers
	remainingCapacity := shift.RemainingCapacity()
	remainingAvailableVolunteers := shift.RemainingAvailableVolunteers(state)

	// Avoid division by zero
	if remainingAvailableVolunteers == 0 {
		return 0
	}

	// Calculate base urgency: remainingCapacity / remainingAvailableVolunteers
	// Higher urgency when there's more capacity to fill relative to available volunteers
	// Examples:
	//   - Shift needs 5, has 10 available volunteers → 5/10 = 0.5 (moderate)
	//   - Shift needs 5, has 5 available volunteers → 5/5 = 1.0 (urgent!)
	//   - Shift needs 1, has 10 available volunteers → 1/10 = 0.1 (low priority)
	//   - Shift needs 3, has 6 available volunteers (from 3 groups of 2) → 3/6 = 0.5
	urgency := float64(remainingCapacity) / float64(remainingAvailableVolunteers)

	// Clamp urgency to [0, 1] range
	if urgency > 1.0 {
		urgency = 1.0
	}
	if urgency < 0 {
		urgency = 0
	}

	// In resource-constrained scenarios, we want to spread volunteers more evenly
	// rather than filling some shifts completely while leaving others understaffed.
	// Once a shift reaches its "fair share" (expected fill), affinity drops to zero.
	if state.IsResourceConstrained() && shift.Size > 0 {
		expectedFill := state.ExpectedFillPerShift()
		currentFill := float64(shift.CurrentSize())

		// If shift has reached or exceeded its fair share, return 0 affinity
		// This expresses "no preference" from size perspective - similar to being full
		// Other criteria can still contribute positive affinity if needed
		if currentFill >= expectedFill {
			return 0
		}

		// For shifts below expected fill, boost based on how far below they are
		// deficitRatio: how far below expected fill is this shift?
		// 1.0 = empty, 0.0 = at expected fill
		deficitRatio := (expectedFill - currentFill) / expectedFill

		// Multiply urgency by (1 + deficitRatio) to boost emptier shifts
		affinity := urgency * (1.0 + deficitRatio)

		// Clamp to [0, 1] range
		if affinity > 1.0 {
			affinity = 1.0
		}

		return affinity
	}

	return urgency
}

func (c *ShiftSizeCriterion) GroupWeight() float64 {
	return c.groupWeight
}

func (c *ShiftSizeCriterion) AffinityWeight() float64 {
	return c.affinityWeight
}

func (c *ShiftSizeCriterion) ValidateRotaState(state *rotageneration.RotaState) []rotageneration.ShiftValidationError {
	var errors []rotageneration.ShiftValidationError

	for _, shift := range state.Shifts {
		// Skip closed shifts (they're allowed to be empty)
		if shift.Closed {
			continue
		}

		currentSize := shift.CurrentSize()
		if currentSize < shift.Size {
			errors = append(errors, rotageneration.ShiftValidationError{
				ShiftIndex:    shift.Index,
				ShiftDate:     shift.Date,
				CriterionName: c.Name(),
				Description:   fmt.Sprintf("Shift is underfilled: has %d volunteers but size is %d", currentSize, shift.Size),
			})
		} else if currentSize > shift.Size {
			errors = append(errors, rotageneration.ShiftValidationError{
				ShiftIndex:    shift.Index,
				ShiftDate:     shift.Date,
				CriterionName: c.Name(),
				Description:   fmt.Sprintf("Shift is overfilled: has %d volunteers but size is %d", currentSize, shift.Size),
			})
		}
	}

	return errors
}
