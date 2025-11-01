package criteria

import (
	"fmt"

	rotageneration "github.com/jakechorley/ilford-drop-in/pkg/core/allocator"
)

// MaleBalanceCriterion ensures each shift has at least one male volunteer and spreads male volunteers maximally.
//
// Validity:
//   - Returns false if the shift already has a male and this group would add another male
//     (when there are other shifts that need males more urgently)
//   - This is calculated by checking if assigning this group would be wasteful
//
// Affinity:
//   - Only applies to groups that contain male volunteers
//   - Increases affinity for shifts with fewer available male groups (unpopular for males)
//   - Returns 0 if the group doesn't contain any males
//
// Promotion:
//   - Promotes groups with male volunteers to ensure good distribution
type MaleBalanceCriterion struct {
	groupWeight    float64
	affinityWeight float64
}

// NewMaleBalanceCriterion creates a new MaleBalanceCriterion with the given weights
func NewMaleBalanceCriterion(groupWeight, affinityWeight float64) *MaleBalanceCriterion {
	return &MaleBalanceCriterion{
		groupWeight:    groupWeight,
		affinityWeight: affinityWeight,
	}
}

func (c *MaleBalanceCriterion) Name() string {
	return "MaleBalance"
}

func (c *MaleBalanceCriterion) PromoteVolunteerGroup(state *rotageneration.RotaState, group *rotageneration.VolunteerGroup) float64 {
	// Promote groups with male volunteers
	if group.MaleCount > 0 {
		return 1.0
	}
	return 0
}

func (c *MaleBalanceCriterion) IsShiftValid(state *rotageneration.RotaState, group *rotageneration.VolunteerGroup, shift *rotageneration.Shift) bool {
	// Invalid only if assigning this group would fill the shift with no males at all

	// If the group has males, it's always valid
	if group.MaleCount > 0 {
		return true
	}

	// If shift already has a male, it's valid
	if shift.MaleCount > 0 {
		return true
	}

	// If this group would not fill the team lead slot it cannot fill the shift
	if shift.TeamLead == nil && !group.HasTeamLead {
		return true
	}

	// Check if allocating this group (with no males) would fill the shift
	// Only count ordinary volunteers (team leads don't count toward shift size)
	ordinaryVolunteerCount := group.OrdinaryVolunteerCount()

	remainingCapacity := shift.RemainingCapacity()

	// If adding this group would fill the shift and there are no males yet, invalid
	wouldFillShift := ordinaryVolunteerCount >= remainingCapacity

	return !wouldFillShift
}

func (c *MaleBalanceCriterion) CalculateShiftAffinity(state *rotageneration.RotaState, group *rotageneration.VolunteerGroup, shift *rotageneration.Shift) float64 {
	// Only calculate affinity for groups with males
	if group.MaleCount == 0 {
		return 0
	}

	// Count how many male volunteers are still available for this shift
	remainingMaleVolunteers := shift.RemainingAvailableMaleVolunteers(state)

	// Calculate need based on current male count
	// If shift has no males yet, need is 1
	// If shift has males, need decreases
	need := 1.0
	if shift.MaleCount > 0 {
		// Reduce need based on how many males are already allocated
		// Each male reduces the need by 0.5, clamped to minimum of 0.1
		need = 1.0 - (float64(shift.MaleCount) * 0.5)
		if need < 0.1 {
			need = 0.1
		}
	}

	// Calculate affinity: need / number of available male volunteers
	// remainingMaleVolunteers will never be less than 1 but we make extra sure not to divide by zero
	// Higher affinity when:
	// - Shift has fewer males already (higher need)
	// - Fewer male volunteers are available (unpopular shift for males)
	//
	// Examples:
	//   - Shift has 0 males, 10 available male volunteers → 1.0/10 = 0.1 (low priority)
	//   - Shift has 0 males, 2 available male volunteers → 1.0/2 = 0.5 (moderate)
	//   - Shift has 0 males, 1 available male volunteer → 1.0/1 = 1.0 (urgent!)
	//   - Shift has 1 male, 5 available male volunteers → 0.5/5 = 0.1 (low priority)
	affinity := need / max(float64(remainingMaleVolunteers), 1)

	return affinity
}

func (c *MaleBalanceCriterion) GroupWeight() float64 {
	return c.groupWeight
}

func (c *MaleBalanceCriterion) AffinityWeight() float64 {
	return c.affinityWeight
}

func (c *MaleBalanceCriterion) ValidateRotaState(state *rotageneration.RotaState) []rotageneration.ShiftValidationError {
	var errors []rotageneration.ShiftValidationError

	for _, shift := range state.Shifts {
		// Skip closed shifts (they don't need male volunteers)
		if shift.Closed {
			continue
		}

		// Check if shift has at least one male volunteer
		if shift.MaleCount == 0 {
			errors = append(errors, rotageneration.ShiftValidationError{
				ShiftIndex:    shift.Index,
				ShiftDate:     shift.Date,
				CriterionName: c.Name(),
				Description:   fmt.Sprintf("Shift has no male volunteers (has %d males)", shift.MaleCount),
			})
		}
	}

	return errors
}
