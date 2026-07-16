package allocator

import "slices"

// Gender constants
const (
	GenderMale = "Male"
)

// VolunteerState holds the volunteer groups built for an allocation problem.
type VolunteerState struct {
	// VolunteerGroups available for allocation
	VolunteerGroups []*VolunteerGroup
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

// CurrentSize returns the current number of ordinary volunteers allocated to this shift
// (team leads excluded, custom preallocations counted).
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

// IsAvailable returns true if the group is available for the given shift
func (vg *VolunteerGroup) IsAvailable(shiftIndex int) bool {
	return slices.Contains(vg.AvailableShiftIndices, shiftIndex)
}
