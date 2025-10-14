package rotageneration

// CalculateShiftAffinity computes the affinity score between a volunteer group and a shift.
// Higher scores indicate better matches - the group should be allocated to higher-affinity shifts first.
//
// Returns 0 if:
//   - The group is not available for this shift
//   - The group has already been allocated to this shift
//   - Any criterion's IsShiftValid hook returns false (constraint violation)
//
// Otherwise returns the sum of all criteria affinity scores (weighted).
func CalculateShiftAffinity(state *RotaState, group *VolunteerGroup, shift *Shift, criteria []Criterion) float64 {
	// Return 0 if group is not available for this shift
	if !group.IsAvailable(shift.Index) {
		return 0
	}

	// Return 0 if group has already been allocated to this shift
	if group.IsAllocated(shift.Index) {
		return 0
	}

	// Run all validity checks - if any return false, this shift is invalid
	for _, criterion := range criteria {
		if !criterion.IsShiftValid(state, group, shift) {
			return 0
		}
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
