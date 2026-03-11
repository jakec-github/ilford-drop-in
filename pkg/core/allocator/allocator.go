package allocator

import "slices"

// Allocator manages the rota generation process with configurable criteria
type Allocator struct {
	criteria []Criterion
	state    *RotaState
}

// AllocationConfig contains the configuration for creating a new Allocator
type AllocationConfig struct {
	// Criteria to apply during allocation (with their weights)
	Criteria []Criterion

	// MaxAllocationFrequency is the ratio of shifts to allocate (e.g., 0.5 = 50%, 0.33 = 33%)
	MaxAllocationFrequency float64

	// HistoricalShifts from previous rotas (for pattern analysis and fairness)
	HistoricalShifts []*Shift

	// Volunteers is the list of all volunteers
	Volunteers []Volunteer

	// Availability is the list of availability responses from volunteers
	Availability []VolunteerAvailability

	// ShiftDates is the list of dates for shifts in the current rota (e.g., "2024-01-01", "2024-01-08", ...)
	ShiftDates []string

	// DefaultShiftSize is the default number of volunteers per shift
	DefaultShiftSize int

	// Overrides allow customizing specific shifts
	Overrides []ShiftOverride

	// Built-in ranking weights for volunteer group prioritization
	// These control how groups are ranked before allocation

	// WeightCurrentRotaUrgency is the weight applied based on how much of the current rota's
	// allocation budget the group needs to use up. Higher values prioritize groups that need
	// to be allocated frequently in this rota to stay on track.
	WeightCurrentRotaUrgency float64

	// WeightOverallFrequencyFairness is the weight applied based on how many allocations
	// the group needs to reach their target frequency over time (historical + current).
	// Higher values prioritize fairness across all rotas.
	WeightOverallFrequencyFairness float64

	// WeightPromoteGroup is the weight applied to groups over individuals.
	// Higher values prioritise groups more strongly. Group size does not affect score.
	WeightPromoteGroup float64
}

// AllocationOutcome represents the result of a rota generation
type AllocationOutcome struct {
	// State is the final rota state after allocation
	State *RotaState

	// Status is the overall validity of the rota.
	// RotaStatusValid means no errors; RotaStatusIncomplete means only incomplete errors
	// (fixable by adding volunteers); RotaStatusInvalid means at least one invalid error
	// (a volunteer must be removed).
	Status RotaStatus

	// Success is true when Status == RotaStatusValid (no validation errors).
	Success bool

	// UnderutilizedGroups contains groups that had remaining availability but weren't fully allocated
	UnderutilizedGroups []*VolunteerGroup

	// ValidationErrors contains any validation errors found in the final state
	ValidationErrors []ShiftValidationError
}

// Allocate runs the main allocation loop to generate the rota
func Allocate(config AllocationConfig) (*AllocationOutcome, error) {

	// Initialise allocator
	allocator, err := InitAllocation(config)
	if err != nil {
		return nil, err
	}

	// Apply preallocations before main allocation loop
	// This allocates volunteers specified in overrides regardless of availability
	err = allocator.ApplyPreallocations(allocator.state)
	if err != nil {
		return nil, err
	}

	volunteers := allocator.state.VolunteerState

	// Exhaust groups that are already at max frequency after preallocations
	maxAllocationCount := allocator.state.MaxAllocationCount()
	groupsToKeep := make([]*VolunteerGroup, 0)
	for _, group := range volunteers.VolunteerGroups {
		allocationCount := len(group.AllocatedShiftIndices)
		availabilityCount := len(group.AvailableShiftIndices)
		if allocationCount >= min(availabilityCount, maxAllocationCount) {
			// Group is at max, exhaust it
			allocator.exhaustGroup(group)
		} else {
			groupsToKeep = append(groupsToKeep, group)
		}
	}
	volunteers.VolunteerGroups = groupsToKeep

	// Main allocation loop: each iteration selects the highest-ranked active group
	// based on the current rota state (winner-stays-on). Scores are re-computed fresh
	// before every selection, so each decision reflects the latest picture of the rota.
	for {
		if len(volunteers.VolunteerGroups) == 0 {
			break
		}

		// Select the group with the highest current ranking score
		group := allocator.selectNextGroup()

		// Find best shift for this group
		bestShift := allocator.findBestShift(group)

		// If no valid shift found, exhaust and remove from active pool
		if bestShift == nil {
			allocator.exhaustGroup(group)
			allocator.removeFromActive(group)
			continue
		}

		// Allocate group to shift; remove from active pool if now exhausted
		if allocator.allocateGroupToShift(group, bestShift) {
			allocator.removeFromActive(group)
		}

		// Check if all shifts are full
		if allocator.allShiftsFull() {
			break
		}
	}

	// Build outcome report
	return allocator.buildOutcome(), nil
}

// findBestShift finds the shift with highest affinity for the given group
func (a *Allocator) findBestShift(group *VolunteerGroup) *Shift {
	var bestShift *Shift
	var bestAffinity float64

	for _, shift := range a.state.Shifts {
		// Skip closed shifts (no allocations allowed)
		if shift.Closed {
			continue
		}

		// Skip full shifts
		if shift.IsFull() {
			continue
		}

		// Skip invalid shifts
		if !IsShiftValidForGroup(a.state, group, shift, a.criteria) {
			continue
		}

		affinity := CalculateShiftAffinity(a.state, group, shift, a.criteria)

		if affinity > bestAffinity {
			bestAffinity = affinity
			bestShift = shift
		}
	}

	return bestShift
}

// allocateGroupToShift assigns a group to a shift and updates state
// Returns a boolean indicating whether this volunteer is exhausted
func (a *Allocator) allocateGroupToShift(group *VolunteerGroup, shift *Shift) bool {
	// Add group to shift's allocated groups
	shift.AllocatedGroups = append(shift.AllocatedGroups, group)

	// Add shift index to group's allocated shifts
	group.AllocatedShiftIndices = append(group.AllocatedShiftIndices, shift.Index)

	// Update shift metadata
	if group.HasTeamLead {
		// Find the team lead in the group
		for _, member := range group.Members {
			if member.IsTeamLead {
				shift.TeamLead = &member
				break
			}
		}
	}

	// Update male count
	shift.MaleCount += group.MaleCount

	// Check if group is now exhausted
	allocationCount := len(group.AllocatedShiftIndices)
	availabilityCount := len(group.AvailableShiftIndices)
	maxAllocationCount := a.state.MaxAllocationCount()
	if allocationCount == min(availabilityCount, maxAllocationCount) {
		a.exhaustGroup(group)
		return true
	}
	return false
}

func (a *Allocator) exhaustGroup(group *VolunteerGroup) {
	a.state.VolunteerState.ExhaustedVolunteerGroups[group] = true
}

// allShiftsFull checks if all shifts have reached their target size
// Closed shifts are always considered "full" since they don't need allocation
func (a *Allocator) allShiftsFull() bool {
	for _, shift := range a.state.Shifts {
		// Closed shifts don't need to be filled
		if shift.Closed {
			continue
		}

		if !shift.IsFull() {
			return false
		}
	}
	return true
}

// selectNextGroup returns the group with the highest current ranking score from the active pool.
// Scores are re-evaluated from the current rota state on every call, ensuring each allocation
// decision reflects the latest state (winner-stays-on).
func (a *Allocator) selectNextGroup() *VolunteerGroup {
	groups := a.state.VolunteerState.VolunteerGroups
	best := groups[0]
	bestScore := calculateGroupRankingScore(a.state, best, a.criteria, a.state.MaxAllocationFrequency)
	for _, group := range groups[1:] {
		score := calculateGroupRankingScore(a.state, group, a.criteria, a.state.MaxAllocationFrequency)
		if score > bestScore {
			bestScore = score
			best = group
		}
	}
	return best
}

// removeFromActive removes a group from the active VolunteerGroups pool.
func (a *Allocator) removeFromActive(group *VolunteerGroup) {
	volunteers := a.state.VolunteerState
	for i, g := range volunteers.VolunteerGroups {
		if g == group {
			volunteers.VolunteerGroups = slices.Delete(volunteers.VolunteerGroups, i, i+1)
			return
		}
	}
}

// buildOutcome creates the final allocation outcome report
func (a *Allocator) buildOutcome() *AllocationOutcome {
	// Initialize with empty slices (not nil) for easier consumption
	outcome := &AllocationOutcome{
		State:               a.state,
		UnderutilizedGroups: []*VolunteerGroup{},
		ValidationErrors:    []ShiftValidationError{},
	}

	// Safety check
	if a.state == nil {
		outcome.Status = RotaStatusInvalid
		outcome.Success = false
		return outcome
	}

	// Check for underutilized groups (check both active and exhausted groups)
	maxAllocationCount := a.state.MaxAllocationCount()

	// Check active groups
	for _, group := range a.state.VolunteerState.VolunteerGroups {
		allocatedCount := len(group.AllocatedShiftIndices)
		availableCount := len(group.AvailableShiftIndices)

		// Group is underutilized if:
		// - Has remaining availability
		// - Hasn't reached max allocation count
		// - Was allocated at least once (so we know it's viable)
		if allocatedCount < availableCount && allocatedCount < maxAllocationCount && allocatedCount > 0 {
			outcome.UnderutilizedGroups = append(outcome.UnderutilizedGroups, group)
		}
	}

	// Check exhausted groups too (they might be exhausted but underutilized)
	for group := range a.state.VolunteerState.ExhaustedVolunteerGroups {
		allocatedCount := len(group.AllocatedShiftIndices)
		availableCount := len(group.AvailableShiftIndices)

		if allocatedCount < availableCount && allocatedCount < maxAllocationCount && allocatedCount > 0 {
			outcome.UnderutilizedGroups = append(outcome.UnderutilizedGroups, group)
		}
	}

	// Run validation
	outcome.ValidationErrors = ValidateRotaState(a.state, a.criteria)

	outcome.Status = computeRotaStatus(outcome.ValidationErrors)
	outcome.Success = outcome.Status == RotaStatusValid

	return outcome
}

// computeRotaStatus derives the overall rota status from a slice of validation errors.
// No errors → Valid. Any INVALID error → Invalid. Otherwise → Incomplete.
func computeRotaStatus(errors []ShiftValidationError) RotaStatus {
	if len(errors) == 0 {
		return RotaStatusValid
	}
	for _, err := range errors {
		if err.Type == ValidationErrorTypeInvalid {
			return RotaStatusInvalid
		}
	}
	return RotaStatusIncomplete
}
