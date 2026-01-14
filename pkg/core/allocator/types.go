package allocator

import "slices"

// Gender constants
const (
	GenderMale = "Male"
)

// VolunteerState manages the volunteer groups and tracks exhaustion status
type VolunteerState struct {
	// VolunteerGroups available for allocation
	VolunteerGroups []*VolunteerGroup

	// ExhaustedVolunteerGroups tracks which groups are exhausted
	// Groups are exhausted when allocated to all valid shifts OR reached MaxAllocationFrequency
	// Updated by the main allocation loop
	ExhaustedVolunteerGroups map[*VolunteerGroup]bool
}

// RotaState represents the current state of the rota during generation
type RotaState struct {
	// Shifts being filled (includes both allocated and unallocated)
	Shifts []*Shift

	// VolunteerState manages volunteer groups and exhaustion tracking
	VolunteerState *VolunteerState

	// HistoricalShifts from previous rotas (read-only, for pattern analysis and fairness)
	HistoricalShifts []*Shift

	// MaxAllocationFrequency is the ratio of shifts to allocate (e.g., 0.5 = 50%, 0.33 = 33%)
	// The maximum allocation count is calculated as: floor(len(Shifts) * MaxAllocationFrequency)
	MaxAllocationFrequency float64

	// Built-in ranking weights
	WeightCurrentRotaUrgency       float64
	WeightOverallFrequencyFairness float64
	WeightPromoteGroup             float64
}

// MaxAllocationCount returns the maximum number of shifts a group can be allocated to
// based on the frequency ratio and the total number of shifts
func (rs *RotaState) MaxAllocationCount() int {
	return int(float64(len(rs.Shifts)) * rs.MaxAllocationFrequency)
}

// VolunteerGroup represents a group of volunteers that are allocated together
type VolunteerGroup struct {
	// GroupKey identifies the group (empty string for individual volunteers)
	GroupKey string

	// Members in this group
	Members []Volunteer

	// AvailableShiftIndices contains the indices of shifts this group is available for
	AvailableShiftIndices []int

	// AllocatedShiftIndices tracks which shifts this group has been allocated to
	AllocatedShiftIndices []int

	// HistoricalAllocationCount is the number of times this group was allocated in historical shifts
	// Used for fairness calculations and allocation balancing
	HistoricalAllocationCount int

	// HasTeamLead indicates if any member of this group is a team lead
	HasTeamLead bool

	// MaleCount is the number of male volunteers in this group
	MaleCount int
}

// Volunteer represents an individual volunteer
type Volunteer struct {
	ID          string
	FirstName   string
	LastName    string
	DisplayName string
	Gender      string
	IsTeamLead  bool
	GroupKey    string
}

// Shift represents a single shift that needs to be filled
type Shift struct {
	// Date of the shift
	Date string

	// Index in the Shifts array (for quick reference)
	Index int

	// Size is the target number of volunteers for this shift
	Size int

	// AllocatedGroups tracks which volunteer groups have been assigned
	AllocatedGroups []*VolunteerGroup

	// CustomPreallocations are volunteer IDs manually assigned before generation
	// These count toward Size but don't affect TeamLead or MaleCount
	CustomPreallocations []string

	// TeamLead is the team lead assigned to this shift (nil if none assigned)
	// Does not count toward Size
	TeamLead *Volunteer

	// MaleCount is the number of male volunteers allocated to this shift via AllocatedGroups
	// Does not include TeamLead or pre-allocated volunteers
	MaleCount int

	// AvailableGroups contains volunteer groups that expressed availability for this shift
	// (populated during initialization)
	AvailableGroups []*VolunteerGroup

	// Closed indicates this shift is closed (no allocations should be made)
	// Closed shifts appear in the rota but remain empty
	Closed bool

	// PreallocatedVolunteerIDs are volunteer IDs to preallocate (as ordinary volunteers)
	// These are processed during initialization and then allocated as proper groups
	PreallocatedVolunteerIDs []string

	// PreallocatedTeamLeadID is the volunteer ID to preallocate as team lead
	// This is processed during initialization and then allocated as a proper group
	PreallocatedTeamLeadID string
}

// CurrentSize returns the current number of volunteers allocated to this shift
func (s *Shift) CurrentSize() int {
	size := len(s.CustomPreallocations)
	for _, group := range s.AllocatedGroups {
		for _, member := range group.Members {
			if !member.IsTeamLead {
				size++
			}
		}
	}
	return size
}

// IsFull returns true if the shift has reached its desired size
func (s *Shift) IsFull() bool {
	return s.CurrentSize() >= s.Size
}

// RemainingCapacity returns the number of ordinary volunteers who should be assigned
func (s *Shift) RemainingCapacity() int {
	return max(s.Size-s.CurrentSize(), 0)
}

// buildAllocatedGroupSet creates a set of groups already allocated to this shift for fast lookup
func (s *Shift) buildAllocatedGroupSet() map[*VolunteerGroup]bool {
	allocatedSet := make(map[*VolunteerGroup]bool)
	for _, group := range s.AllocatedGroups {
		allocatedSet[group] = true
	}
	return allocatedSet
}

// RemainingAvailableVolunteers returns the count of ordinary volunteers (non-team leads)
// from groups that are:
//   - Available for this shift (in AvailableGroups)
//   - Not yet exhausted (not in state.VolunteerState.ExhaustedVolunteerGroups)
//   - Not yet allocated to this shift
//   - Small enough to fit in the remaining capacity
//
// This is used to calculate shift affinity - when there are fewer remaining
// available volunteers relative to remaining capacity, the shift becomes more urgent.
func (s *Shift) RemainingAvailableVolunteers(state *RotaState) int {
	count := 0

	// Calculate remaining capacity for this shift
	remainingCapacity := s.RemainingCapacity()

	// Build set of already allocated groups for fast lookup
	allocatedSet := s.buildAllocatedGroupSet()

	// Count ordinary volunteers from groups that are available, not exhausted,
	// not already allocated, and small enough to fit
	for _, group := range s.AvailableGroups {
		// Skip if exhausted
		if state.VolunteerState.ExhaustedVolunteerGroups[group] {
			continue
		}

		// Skip if already allocated to this shift
		if allocatedSet[group] {
			continue
		}

		// Count ordinary volunteers in this group (exclude team leads)
		ordinaryVolunteerCount := 0
		for _, member := range group.Members {
			if !member.IsTeamLead {
				ordinaryVolunteerCount++
			}
		}

		// Skip groups that are too large to fit in remaining capacity
		if ordinaryVolunteerCount > remainingCapacity {
			continue
		}

		count += ordinaryVolunteerCount
	}

	return count
}

// RemainingAvailableTeamLeads returns the count of team leads from groups that are:
//   - Available for this shift (in AvailableGroups)
//   - Not yet exhausted (not in state.VolunteerState.ExhaustedVolunteerGroups)
//   - Not yet allocated to this shift
//   - Contain a team lead
//
// This is used to calculate team lead affinity - when there are fewer remaining
// available team leads, shifts become more urgent for team lead allocation.
func (s *Shift) RemainingAvailableTeamLeads(state *RotaState) int {
	count := 0

	// Build set of already allocated groups for fast lookup
	allocatedSet := s.buildAllocatedGroupSet()

	// Count team leads from groups that are available, not exhausted, and not already allocated
	for _, group := range s.AvailableGroups {
		// Skip if exhausted
		if state.VolunteerState.ExhaustedVolunteerGroups[group] {
			continue
		}

		// Skip if already allocated to this shift
		if allocatedSet[group] {
			continue
		}

		// Count this group if it has a team lead
		if group.HasTeamLead {
			count++
		}
	}

	return count
}

// RemainingAvailableMaleVolunteers returns the count of male volunteers from groups that are:
//   - Available for this shift (in AvailableGroups)
//   - Not yet exhausted (not in state.VolunteerState.ExhaustedVolunteerGroups)
//   - Not yet allocated to this shift
//   - Small enough to fit in the remaining capacity
//
// This is used to calculate male balance affinity - when there are fewer remaining
// available male volunteers, shifts become more urgent for male allocation.
func (s *Shift) RemainingAvailableMaleVolunteers(state *RotaState) int {
	count := 0

	// Calculate remaining capacity for this shift
	remainingCapacity := s.RemainingCapacity()

	// Build set of already allocated groups for fast lookup
	allocatedSet := s.buildAllocatedGroupSet()

	// Count male volunteers from groups that are available, not exhausted, not allocated,
	// and small enough to fit
	for _, group := range s.AvailableGroups {
		// Skip if exhausted
		if state.VolunteerState.ExhaustedVolunteerGroups[group] {
			continue
		}

		// Skip if already allocated to this shift
		if allocatedSet[group] {
			continue
		}

		// Count ordinary volunteers in this group (exclude team leads)
		ordinaryVolunteerCount := 0
		for _, member := range group.Members {
			if !member.IsTeamLead {
				ordinaryVolunteerCount++
			}
		}

		// Skip groups that are too large to fit in remaining capacity
		if ordinaryVolunteerCount > remainingCapacity {
			continue
		}

		// Add the male count from this group
		count += group.MaleCount
	}

	return count
}

// IsAvailable returns true if the group is available for the given shift
func (vg *VolunteerGroup) IsAvailable(shiftIndex int) bool {
	return slices.Contains(vg.AvailableShiftIndices, shiftIndex)
}

// IsAllocated returns true if the group has already been allocated to the given shift
func (vg *VolunteerGroup) IsAllocated(shiftIndex int) bool {
	return slices.Contains(vg.AllocatedShiftIndices, shiftIndex)
}

// TotalAllocationCount returns the total number of allocations (historical + current rota)
func (vg *VolunteerGroup) TotalAllocationCount() int {
	return vg.HistoricalAllocationCount + len(vg.AllocatedShiftIndices)
}

// RemainingCapacity returns how many more allocations this group can accept
func (vg *VolunteerGroup) RemainingCapacity(maxFrequency int) int {
	remaining := maxFrequency - len(vg.AllocatedShiftIndices)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// OrdinaryVolunteerCount returns the count of non-team lead volunteers in this group
func (vg *VolunteerGroup) OrdinaryVolunteerCount() int {
	count := 0
	for _, member := range vg.Members {
		if !member.IsTeamLead {
			count++
		}
	}
	return count
}

// DesiredRemainingAllocations calculates how many more shifts this group should ideally be
// allocated to reach the target frequency over time.
//
// This function calculates the ideal number based purely on target frequency, without
// enforcing any caps. The cap should be enforced elsewhere (e.g., in criteria or constraints).
//
// Parameters:
//   - totalHistoricalShifts: total number of shifts in historical data
//   - totalCurrentShifts: total number of shifts in the current rota
//   - targetFrequency: desired allocation frequency (e.g., 0.5 = allocated to 50% of shifts)
//
// Returns the number of remaining allocations needed to reach targetFrequency.
// Can return 0 or negative if the group has met or exceeded the target.
//
// Example 1 - Group needs more allocations:
//   - Historical: 100 shifts, group allocated 20 times (20%)
//   - Current: 10 shifts, group allocated 2 times so far
//   - Target: 0.25 (25%)
//   - Total shifts after this rota: 110
//   - Target allocations: 110 * 0.25 = 27.5 → 27
//   - Current total allocations: 20 + 2 = 22
//   - Desired remaining: 27 - 22 = 5
//
// Example 2 - Group at target:
//   - Historical: 100 shifts, allocated 25 times (25%)
//   - Current: 10 shifts, allocated 2 times so far
//   - Target: 0.25 (25%)
//   - Total shifts: 110
//   - Target allocations: 110 * 0.25 = 27.5 → 27
//   - Current total: 25 + 2 = 27
//   - Desired remaining: 27 - 27 = 0
//
// Example 3 - Group above target:
//   - Historical: 100 shifts, allocated 30 times (30%)
//   - Current: 10 shifts, allocated 2 times so far
//   - Target: 0.25 (25%)
//   - Total shifts: 110
//   - Target allocations: 110 * 0.25 = 27.5 → 27
//   - Current total: 30 + 2 = 32
//   - Desired remaining: 27 - 32 = -5 (over-allocated)
func (vg *VolunteerGroup) DesiredRemainingAllocations(totalHistoricalShifts, totalCurrentShifts int, targetFrequency float64) int {
	// Calculate total shifts across historical and current rotas
	totalShifts := totalHistoricalShifts + totalCurrentShifts

	// Calculate target number of allocations based on frequency
	targetAllocations := int(float64(totalShifts) * targetFrequency)

	// Calculate current total allocations (historical + current rota so far)
	currentAllocations := vg.TotalAllocationCount()

	// Calculate how many more allocations are needed to reach target
	// Can be negative if already over-allocated
	remaining := targetAllocations - currentAllocations

	return remaining
}
