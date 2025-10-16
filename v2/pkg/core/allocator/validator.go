package rotageneration

import "fmt"

// ValidateRotaState validates the final rota state against core invariants and all provided criteria.
// Returns a slice of validation errors for any constraint violations.
// An empty slice indicates the rota is valid.
func ValidateRotaState(state *RotaState, criteria []Criterion) []ShiftValidationError {
	var errors []ShiftValidationError

	// First validate core invariants
	coreErrors := validateCoreInvariants(state)
	errors = append(errors, coreErrors...)

	// Then run validation for each criterion
	for _, criterion := range criteria {
		criterionErrors := criterion.ValidateRotaState(state)
		errors = append(errors, criterionErrors...)
	}

	return errors
}

// validateCoreInvariants checks fundamental requirements that must hold regardless of criteria.
// These are invariants that the allocation algorithm should never violate.
func validateCoreInvariants(state *RotaState) []ShiftValidationError {
	var errors []ShiftValidationError

	// 1. Check for over-allocation (groups allocated more than max frequency)
	errors = append(errors, checkOverAllocation(state)...)

	// 2. Check for duplicate allocations (same group allocated twice to one shift)
	errors = append(errors, checkDuplicateAllocations(state)...)

	// 3. Check for availability violations (groups allocated to shifts they're not available for)
	errors = append(errors, checkAvailabilityViolations(state)...)

	// 4. Check data consistency (indices, team lead field, male count field)
	errors = append(errors, checkDataConsistency(state)...)

	return errors
}

// checkOverAllocation verifies no group is allocated more than the maximum allocation count
func checkOverAllocation(state *RotaState) []ShiftValidationError {
	var errors []ShiftValidationError

	maxAllocationCount := state.MaxAllocationCount()
	for _, group := range state.VolunteerState.VolunteerGroups {
		allocatedCount := len(group.AllocatedShiftIndices)
		if allocatedCount > maxAllocationCount {
			errors = append(errors, ShiftValidationError{
				ShiftIndex:    -1, // Not specific to one shift
				ShiftDate:     "",
				CriterionName: "CoreInvariant",
				Description:   fmt.Sprintf("Group '%s' is allocated to %d shifts but max is %d (frequency ratio: %.2f)", group.GroupKey, allocatedCount, maxAllocationCount, state.MaxAllocationFrequency),
			})
		}
	}

	return errors
}

// checkDuplicateAllocations verifies no group appears multiple times in the same shift
func checkDuplicateAllocations(state *RotaState) []ShiftValidationError {
	var errors []ShiftValidationError

	for _, shift := range state.Shifts {
		seen := make(map[string]bool)
		for _, group := range shift.AllocatedGroups {
			if seen[group.GroupKey] {
				errors = append(errors, ShiftValidationError{
					ShiftIndex:    shift.Index,
					ShiftDate:     shift.Date,
					CriterionName: "CoreInvariant",
					Description:   fmt.Sprintf("Group '%s' is allocated multiple times to the same shift", group.GroupKey),
				})
			}
			seen[group.GroupKey] = true
		}
	}

	return errors
}

// checkAvailabilityViolations verifies groups are only allocated to shifts they're available for
func checkAvailabilityViolations(state *RotaState) []ShiftValidationError {
	var errors []ShiftValidationError

	for _, shift := range state.Shifts {
		for _, group := range shift.AllocatedGroups {
			if !group.IsAvailable(shift.Index) {
				errors = append(errors, ShiftValidationError{
					ShiftIndex:    shift.Index,
					ShiftDate:     shift.Date,
					CriterionName: "CoreInvariant",
					Description:   fmt.Sprintf("Group '%s' is allocated to shift but is not available for it", group.GroupKey),
				})
			}
		}
	}

	return errors
}

// checkDataConsistency verifies internal data structure consistency
func checkDataConsistency(state *RotaState) []ShiftValidationError {
	var errors []ShiftValidationError

	// Build a map of which shifts each group is actually allocated to
	actualAllocations := make(map[string]map[int]bool)
	for _, shift := range state.Shifts {
		for _, group := range shift.AllocatedGroups {
			if actualAllocations[group.GroupKey] == nil {
				actualAllocations[group.GroupKey] = make(map[int]bool)
			}
			actualAllocations[group.GroupKey][shift.Index] = true
		}
	}

	// Check that each group's AllocatedShiftIndices matches actual allocations
	for _, group := range state.VolunteerState.VolunteerGroups {
		actual := actualAllocations[group.GroupKey]
		if actual == nil {
			actual = make(map[int]bool)
		}

		// Check that all indices in AllocatedShiftIndices are actually allocated
		for _, idx := range group.AllocatedShiftIndices {
			if !actual[idx] {
				errors = append(errors, ShiftValidationError{
					ShiftIndex:    idx,
					ShiftDate:     "",
					CriterionName: "CoreInvariant",
					Description:   fmt.Sprintf("Group '%s' has shift %d in AllocatedShiftIndices but is not actually allocated to it", group.GroupKey, idx),
				})
			}
		}

		// Check that all actual allocations are in AllocatedShiftIndices
		declaredSet := make(map[int]bool)
		for _, idx := range group.AllocatedShiftIndices {
			declaredSet[idx] = true
		}
		for idx := range actual {
			if !declaredSet[idx] {
				errors = append(errors, ShiftValidationError{
					ShiftIndex:    idx,
					ShiftDate:     "",
					CriterionName: "CoreInvariant",
					Description:   fmt.Sprintf("Group '%s' is allocated to shift %d but doesn't have it in AllocatedShiftIndices", group.GroupKey, idx),
				})
			}
		}
	}

	// Check MaleCount field consistency for each shift
	for _, shift := range state.Shifts {
		// Check MaleCount matches sum of group MaleCounts (including team lead if present)
		actualMaleCount := 0
		for _, group := range shift.AllocatedGroups {
			actualMaleCount += group.MaleCount
		}
		// If there's a team lead allocated independently (not part of a group), count them if male
		if shift.TeamLead != nil && shift.TeamLead.Gender == GenderMale {
			// Check if this team lead is already counted in a group
			teamLeadInGroup := false
			for _, group := range shift.AllocatedGroups {
				if group.HasTeamLead {
					teamLeadInGroup = true
					break
				}
			}
			// Only add to count if not already in a group
			if !teamLeadInGroup {
				actualMaleCount++
			}
		}
		if shift.MaleCount != actualMaleCount {
			errors = append(errors, ShiftValidationError{
				ShiftIndex:    shift.Index,
				ShiftDate:     shift.Date,
				CriterionName: "CoreInvariant",
				Description:   fmt.Sprintf("Shift MaleCount field is %d but actual male count from groups is %d", shift.MaleCount, actualMaleCount),
			})
		}
	}

	return errors
}
