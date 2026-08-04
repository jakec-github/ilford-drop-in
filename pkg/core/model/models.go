package model

// Volunteer represents a service volunteer
type Volunteer struct {
	ID          string
	FirstName   string
	LastName    string
	DisplayName string // Computed by ComputeDisplayNames based on uniqueness
	// Roles are the jobs this volunteer will do, in priority order. Holding a
	// Role is what makes someone eligible for its Seats — there is no
	// open-to-all Role and no mapping from one Role to another.
	Roles    []string
	Status   string
	Gender   string
	Email    string
	GroupKey string // Empty string if no group
}

// Holds reports whether the volunteer may be allocated to the named Role.
// Matching is exact: the roster, the config and the solver speak one set of
// strings.
func (v Volunteer) Holds(role string) bool {
	for _, held := range v.Roles {
		if held == role {
			return true
		}
	}
	return false
}

// Rotation represents a rota rotation
type Rotation struct {
	ID         string
	Start      string // Date format
	ShiftCount int
}

// AvailabilityRequest represents a volunteer availability request
type AvailabilityRequest struct {
	ID          string
	RotaID      string
	ShiftDate   string
	VolunteerID string
	FormID      string
	FormURL     string
	FormSent    bool
}

// Allocation represents a shift allocation assignment
type Allocation struct {
	ID           string
	RotaID       string
	ShiftDate    string
	Role         string
	VolunteerID  string // nullable
	Preallocated string // nullable
}

// Cover represents a cover/swap audit trail record
type Cover struct {
	ID        string
	CreatedAt string
	Reason    string
	UserEmail string
}

// Alteration represents a single change to a shift
type Alteration struct {
	ID          string
	ShiftDate   string
	RotaID      string
	Direction   string // "add" or "remove"
	VolunteerID string // nullable
	CustomValue string // nullable
	CoverID     string
	SetTime     string
}
