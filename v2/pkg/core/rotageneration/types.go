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

	// HistoricalFrequency is the number of times this group was allocated in historical shifts
	// Used for fairness calculations and allocation balancing
	HistoricalFrequency int

	// HasTeamLead indicates if any member of this group is a team lead
	HasTeamLead bool

	// HasMale indicates if any member of this group is male
	HasMale bool
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

	// DesiredSize is the target number of volunteers for this shift
	DesiredSize int

	// AllocatedGroups tracks which volunteer groups have been assigned
	AllocatedGroups []*VolunteerGroup

	// PreAllocatedVolunteers are volunteers manually assigned before generation
	// These count toward DesiredSize but don't prevent group allocation
	PreAllocatedVolunteers []string

	// HasTeamLead indicates if a team lead has been allocated to this shift
	HasTeamLead bool

	// HasMale indicates if a male volunteer has been allocated to this shift
	HasMale bool
}

// IsFull returns true if the shift has reached its desired size
func (s *Shift) IsFull() bool {
	currentSize := len(s.PreAllocatedVolunteers)
	for _, group := range s.AllocatedGroups {
		currentSize += len(group.Members)
	}
	return currentSize >= s.DesiredSize
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

// TotalFrequency returns the total number of allocations (historical + current rota)
func (vg *VolunteerGroup) TotalFrequency() int {
	return vg.HistoricalFrequency + len(vg.AllocatedShiftIndices)
}

// RemainingCapacity returns how many more allocations this group can accept
func (vg *VolunteerGroup) RemainingCapacity(maxFrequency int) int {
	remaining := maxFrequency - len(vg.AllocatedShiftIndices)
	if remaining < 0 {
		return 0
	}
	return remaining
}
