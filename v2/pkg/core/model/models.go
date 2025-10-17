package model

type Role string

const (
	RoleTeamLead  Role = "Team lead"
	RoleVolunteer Role = "Service volunteer"
)

func (r Role) IsValid() bool {
	return r == RoleTeamLead || r == RoleVolunteer
}

// Volunteer represents a service volunteer
type Volunteer struct {
	ID        string
	FirstName string
	LastName  string
	Role      Role
	Status    string
	Gender    string
	Email     string
	GroupKey  string // Empty string if no group
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

// Cover represents a volunteer cover/swap
type Cover struct {
	ID                  string
	RotaID              string
	ShiftDate           string
	CoveredVolunteerID  string // nullable
	CoveringVolunteerID string // nullable
}
