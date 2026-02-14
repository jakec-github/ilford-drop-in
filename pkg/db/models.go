package db

// Rotation represents a database rotation record
type Rotation struct {
	ID                string
	Start             string // DATE
	ShiftCount        int
	AllocatedDatetime string // TIMESTAMPTZ, empty string if NULL
}

// AvailabilityRequest represents a database availability request record
type AvailabilityRequest struct {
	ID          string
	RotaID      string
	ShiftDate   string // DATE
	VolunteerID string
	FormID      string
	FormURL     string
	FormSent    bool
}

// Allocation represents a database allocation record
type Allocation struct {
	ID          string
	RotaID      string
	ShiftDate   string // DATE
	Role        string
	VolunteerID string
	CustomEntry string
}

// Cover represents a database cover record (audit trail for rota changes)
type Cover struct {
	ID        string // UUID
	CreatedAt string // TIMESTAMPTZ
	Reason    string
	UserEmail string
}

// Alteration represents a database alteration record (individual change to a shift)
type Alteration struct {
	ID          string // UUID
	ShiftDate   string // DATE
	RotaID      string // UUID
	Direction   string // "add" or "remove"
	VolunteerID string // nullable
	CustomValue string // nullable
	CoverID     string // UUID
	SetTime     string // TIMESTAMPTZ
}
