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

// Role is a job on a Shift, mirroring model.Role. The allocator keeps its own
// copy of the domain types it needs (as it does for Volunteer) so the solver
// contract does not drag the whole domain package along.
type Role struct {
	// Name is what the roster, the pins and the solver all match on.
	Name string
	// Max is the ceiling on this Role's Seats per Shift. Nil means uncapped.
	Max *int
	// Priority orders Seat filling, lowest first.
	Priority int
}

// Seat is one place in a Shift's Shape: Count Seats for this Role.
type Seat struct {
	Role  string
	Count int
}

// Preallocation pins one volunteer, or one custom entry, to a Role on a Shift
// before the solve. Exactly one of VolunteerID and Custom is set.
type Preallocation struct {
	VolunteerID string
	Custom      string
	Role        string
}

// Volunteer represents an individual volunteer
type Volunteer struct {
	ID          string
	FirstName   string
	LastName    string
	DisplayName string
	Gender      string
	// Roles are the jobs this volunteer holds, in priority order. Eligibility
	// for a Seat is holding its Role.
	Roles []string
	// IsTeamLead is the pre-Roles shorthand, kept while the solver still
	// designates a lead after the fact (#89 commit 9 removes it).
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

	// CustomPreallocations are the custom entries on a *solved* shift, set by
	// CpsatOutputToShifts. Pins going into a solve travel in Preallocations
	// instead. (#89 commit 9 replaces this with role-tagged assignments.)
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

	// Preallocations are the pins this shift starts with, each naming the Role
	// it fills. Volunteers, custom entries and leads all travel together: they
	// are the same thing, a Seat spoken for before the solve.
	Preallocations []Preallocation
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
