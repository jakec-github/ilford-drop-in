package rotageneration

import "slices"

// RotaState represents the current state of the rota during generation
type RotaState struct {
	// Shifts being filled (includes both allocated and unallocated)
	Shifts []*Shift

	// VolunteerGroups available for allocation
	VolunteerGroups []*VolunteerGroup

	// HistoricalShifts from previous rotas (read-only, for pattern analysis and fairness)
	HistoricalShifts []*Shift

	// MaxAllocationFrequency is the maximum number of shifts a group can be allocated
	MaxAllocationFrequency int
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
	ID         string
	FirstName  string
	LastName   string
	Gender     string
	IsTeamLead bool
	GroupKey   string
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

	// PreAllocatedVolunteers are volunteer IDs manually assigned before generation
	// These count toward Size but don't affect TeamLead or MaleCount
	PreAllocatedVolunteers []string

	// TeamLead is the team lead assigned to this shift (nil if none assigned)
	// Does not count toward Size
	TeamLead *Volunteer

	// MaleCount is the number of male volunteers allocated to this shift via AllocatedGroups
	// Does not include TeamLead or pre-allocated volunteers
	MaleCount int
}

// IsFull returns true if the shift has reached its desired size
func (s *Shift) IsFull() bool {
	currentSize := len(s.PreAllocatedVolunteers)
	for _, group := range s.AllocatedGroups {
		currentSize += len(group.Members)
	}
	return currentSize >= s.Size
}

// CurrentSize returns the current number of volunteers allocated to this shift
func (s *Shift) CurrentSize() int {
	size := len(s.PreAllocatedVolunteers)
	for _, group := range s.AllocatedGroups {
		size += len(group.Members)
	}
	return size
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
