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

	// MaleCount is the number of male volunteers in this group
	MaleCount int
}

// Role is a job on a Shift, mirroring model.Role. The allocator keeps its own
// copy of the domain types it needs (as it does for Volunteer) so the solver
// contract does not drag the whole domain package along.
type Role struct {
	// Name is what the roster, the pins and the solver all match on.
	Name string
	// Priority orders Seat filling, lowest first. A Role has no ceiling of its
	// own: how many Seats it has on a Shift is that Shift's Shape (issue #185).
	Priority int
}

// Seat is one place in a Shift's Shape: Count Seats for this Role.
type Seat struct {
	Role  string
	Count int
}

// A Shift's Shape is no longer derived. It used to be computed from a single
// size and the Roles' ceilings — every capped Role asked for exactly its
// ceiling, and the uncapped one took the size — which is the only Shape that
// arithmetic could ever produce. The Seats are stated now, in the Rota Defaults
// an admin edits on the Settings screen (issue #129, ADR 0006), and they reach
// this package already resolved.

// Preallocation pins one volunteer, or one custom entry, to a Role on a Shift
// before the solve. Exactly one of VolunteerID and Custom is set.
type Preallocation struct {
	VolunteerID string
	Custom      string
	Role        string
}

// Assignment is one filled Seat on a solved Shift. It is what a
// Preallocation becomes once the solve is done, alongside the Seats the
// solver filled itself: exactly one of Volunteer and Custom is set.
type Assignment struct {
	// Volunteer in the Seat, nil for a custom (free-text) entry.
	Volunteer *Volunteer
	// Custom is the free-text entry in the Seat, empty for a volunteer.
	Custom string
	// Role of the Seat. Never empty — a Seat is always some Role's.
	Role string
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
	Roles    []string
	GroupKey string
}

// Shift represents a single shift that needs to be filled
type Shift struct {
	// Date of the shift
	Date string

	// Index in the Shifts array (for quick reference)
	Index int

	// Shape is the Seats this shift asks for, in the order they are filled. It
	// is an input: InitShifts carries it through from the shift's own spec, and
	// the solver contract sends it.
	Shape []Seat

	// AllocatedGroups tracks which volunteer groups have been assigned
	AllocatedGroups []*VolunteerGroup

	// Assignments are the filled Seats on a *solved* shift, set by
	// CpsatOutputToShifts. Pins going into a solve travel in Preallocations
	// instead.
	Assignments []Assignment

	// MaleCount is the number of male volunteers allocated to this shift via AllocatedGroups
	// Does not include pre-allocated volunteers
	MaleCount int

	// AvailableGroups contains volunteer groups that expressed availability for this shift
	// (populated during initialisation)
	AvailableGroups []*VolunteerGroup

	// Closed indicates this shift is closed (no allocations should be made)
	// Closed shifts appear in the rota but remain empty
	Closed bool

	// Preallocations are the pins this shift starts with, each naming the Role
	// it fills. Volunteers, custom entries and leads all travel together: they
	// are the same thing, a Seat spoken for before the solve.
	Preallocations []Preallocation
}

// IsAvailable returns true if the group is available for the given shift
func (vg *VolunteerGroup) IsAvailable(shiftIndex int) bool {
	return slices.Contains(vg.AvailableShiftIndices, shiftIndex)
}
