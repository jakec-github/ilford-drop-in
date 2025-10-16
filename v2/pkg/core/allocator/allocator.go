package rotageneration

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
}

// AllocationOutcome represents the result of a rota generation
type AllocationOutcome struct {
	// State is the final rota state after allocation
	State *RotaState

	// Success indicates whether all shifts were successfully filled to their target size
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

	volunteers := allocator.state.VolunteerState

	// Main allocation loop
	for {
		// All groups have been exhausted and the rota cannot be completed
		if len(volunteers.VolunteerGroups) == 0 {
			break
		}

		// Pop first group
		group := volunteers.VolunteerGroups[0]
		volunteers.VolunteerGroups = volunteers.VolunteerGroups[1:]

		// Find best shift for this group
		bestShift := allocator.findBestShift(group)

		// If no valid shift found, mark group as exhausted
		if bestShift == nil {
			allocator.exhaustGroup(group)
			continue
		}

		// Allocate group to shift
		allocator.allocateGroupToShift(group, bestShift)

		// Check if group is now exhausted
		allocationCount := len(group.AllocatedShiftIndices)
		availabilityCount := len(group.AvailableShiftIndices)
		maxAllocationCount := allocator.state.MaxAllocationCount()
		if allocationCount == min(availabilityCount, maxAllocationCount) {
			allocator.exhaustGroup(group)
			continue
		}

		//Re-insert group at new ranking
		allocator.reinsertGroup(group)

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
func (a *Allocator) allocateGroupToShift(group *VolunteerGroup, shift *Shift) {
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
}

func (a *Allocator) exhaustGroup(group *VolunteerGroup) {
	a.state.VolunteerState.ExhaustedVolunteerGroups[group] = true
}

// allShiftsFull checks if all shifts have reached their target size
func (a *Allocator) allShiftsFull() bool {
	for _, shift := range a.state.Shifts {
		if !shift.IsFull() {
			return false
		}
	}
	return true
}

// reinsertGroup finds where to insert a group in the ranked list based on score
func (a *Allocator) reinsertGroup(group *VolunteerGroup) {
	score := calculateGroupRankingScore(a.state, group, a.criteria, a.state.MaxAllocationFrequency)

	volunteers := a.state.VolunteerState

	// Find insertion point - first position where our score is greater than comparison score
	insertIdx := len(volunteers.VolunteerGroups) // Default to end

	for i, comparisonGroup := range volunteers.VolunteerGroups {
		comparisonGroupScore := calculateGroupRankingScore(a.state, comparisonGroup, a.criteria, a.state.MaxAllocationFrequency)
		if score > comparisonGroupScore {
			insertIdx = i
			break // Found the first position - insert here
		}
	}

	// Insert group at the found position
	volunteers.VolunteerGroups = slices.Insert(volunteers.VolunteerGroups, insertIdx, group)
}

// buildOutcome creates the final allocation outcome report
func (a *Allocator) buildOutcome() *AllocationOutcome {
	// Initialize with empty slices (not nil) for easier consumption
	outcome := &AllocationOutcome{
		State:                a.state,
		UnderutilizedGroups:  []*VolunteerGroup{},
		ValidationErrors:     []ShiftValidationError{},
	}

	// Safety check
	if a.state == nil {
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

	// Success if all shifts filled and no validation errors
	outcome.Success = len(outcome.ValidationErrors) == 0

	return outcome
}
