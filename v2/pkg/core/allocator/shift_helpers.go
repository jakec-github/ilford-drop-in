package allocator

// IsShiftValidForGroup checks if a volunteer group can be allocated to a shift.
//
// Returns false if:
//   - The group is not available for this shift
//   - The group has already been allocated to this shift
//   - Any criterion's IsShiftValid hook returns false (constraint violation)
//
// Otherwise returns true.
func IsShiftValidForGroup(state *RotaState, group *VolunteerGroup, shift *Shift, criteria []Criterion) bool {
	// Check if group is available for this shift
	if !group.IsAvailable(shift.Index) {
		return false
	}

	// Check if group has already been allocated to this shift
	if group.IsAllocated(shift.Index) {
		return false
	}

	// Run all validity checks - if any return false, this shift is invalid
	for _, criterion := range criteria {
		if !criterion.IsShiftValid(state, group, shift) {
			return false
		}
	}

	return true
}

// CalculateShiftAffinity computes the affinity score between a volunteer group and a shift.
// Higher scores indicate better matches - the group should be allocated to higher-affinity shifts first.
//
// Returns 0 if the shift is not valid for the group (see IsShiftValidForGroup).
// Otherwise returns the sum of all criteria affinity scores (weighted).
func CalculateShiftAffinity(state *RotaState, group *VolunteerGroup, shift *Shift, criteria []Criterion) float64 {
	// Check validity first
	if !IsShiftValidForGroup(state, group, shift, criteria) {
		return 0
	}

	// Sum all affinity scores (weighted)
	totalAffinity := 0.0
	for _, criterion := range criteria {
		affinityValue := criterion.CalculateShiftAffinity(state, group, shift)
		weightedValue := affinityValue * criterion.AffinityWeight()
		totalAffinity += weightedValue
	}

	return totalAffinity
}
