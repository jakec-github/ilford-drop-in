package rotageneration

import "fmt"

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

func (c *ShiftSizeCriterion) PromoteVolunteerGroup(state *RotaState, group *VolunteerGroup) float64 {
	// No promotion logic for this criterion
	return 0
}

func (c *ShiftSizeCriterion) IsShiftValid(state *RotaState, group *VolunteerGroup, shift *Shift) bool {
	// Count ordinary volunteers in the group (exclude team leads)
	ordinaryVolunteerCount := 0
	for _, member := range group.Members {
		if !member.IsTeamLead {
			ordinaryVolunteerCount++
		}
	}

	// Calculate how many spots are available in the shift
	remainingCapacity := shift.RemainingCapacity()

	// Invalid if adding this group's ordinary volunteers would exceed shift size
	return  remainingCapacity >= ordinaryVolunteerCount
}

func (c *ShiftSizeCriterion) CalculateShiftAffinity(state *RotaState, group *VolunteerGroup, shift *Shift) float64 {
	// Count ordinary volunteers in the group (exclude team leads)
	ordinaryVolunteerCount := 0
	for _, member := range group.Members {
		if !member.IsTeamLead {
			ordinaryVolunteerCount++
		}
	}

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

	// Calculate affinity as: remainingCapacity / remainingAvailableVolunteers
	// Higher affinity when there's more capacity to fill relative to available volunteers
	// Examples:
	//   - Shift needs 5, has 10 available volunteers → 5/10 = 0.5 (moderate)
	//   - Shift needs 5, has 5 available volunteers → 5/5 = 1.0 (urgent!)
	//   - Shift needs 1, has 10 available volunteers → 1/10 = 0.1 (low priority)
	//   - Shift needs 3, has 6 available volunteers (from 3 groups of 2) → 3/6 = 0.5
	affinity := float64(remainingCapacity) / float64(remainingAvailableVolunteers)

	// Clamp to [0, 1] range
	if affinity > 1.0 {
		affinity = 1.0
	}
	if affinity < 0 {
		affinity = 0
	}

	return affinity
}

func (c *ShiftSizeCriterion) GroupWeight() float64 {
	return c.groupWeight
}

func (c *ShiftSizeCriterion) AffinityWeight() float64 {
	return c.affinityWeight
}

func (c *ShiftSizeCriterion) ValidateRotaState(state *RotaState) []ShiftValidationError {
	var errors []ShiftValidationError

	for _, shift := range state.Shifts {
		currentSize := shift.CurrentSize()
		if currentSize < shift.Size {
			errors = append(errors, ShiftValidationError{
				ShiftIndex:    shift.Index,
				ShiftDate:     shift.Date,
				CriterionName: c.Name(),
				Description:   fmt.Sprintf("Shift is underfilled: has %d volunteers but size is %d", currentSize, shift.Size),
			})
		} else if currentSize > shift.Size {
			errors = append(errors, ShiftValidationError{
				ShiftIndex:    shift.Index,
				ShiftDate:     shift.Date,
				CriterionName: c.Name(),
				Description:   fmt.Sprintf("Shift is overfilled: has %d volunteers but size is %d", currentSize, shift.Size),
			})
		}
	}

	return errors
}
