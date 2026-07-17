package db

// Rotation represents a database rotation record
type Rotation struct {
	ID                string
	Start             string // DATE
	End               string // DATE
	ShiftCount        int
	AllocatedDatetime string // TIMESTAMPTZ, empty string if NULL
}

// Shift represents a database shift record: a planned session of the drop-in on
// a specific date, minted by a rotation and existing independently of who is
// allocated to it.
type Shift struct {
	ID     string // UUID
	RotaID string // UUID
	Date   string // DATE
}

// AvailabilityRequest represents a database availability request record.
// A request is rota-scoped (one per volunteer per rota); it has no shift date.
type AvailabilityRequest struct {
	ID          string
	RotaID      string
	VolunteerID string
	FormID      string
	FormURL     string
	FormSent    bool
}

// Allocation represents a database allocation record. It is keyed solely by
// ShiftID; rota and date live on the referenced shift, never denormalised here
// (ADR 0001).
type Allocation struct {
	ID          string
	ShiftID     string // UUID
	Role        string
	VolunteerID string
	CustomEntry string
}

// ManualPreallocation represents a database manual preallocation record: a
// person pinned to a shift before allocation runs. Like Allocation it is keyed
// solely by ShiftID; rota and date live on the referenced shift (ADR 0001). It
// mirrors the allocation row shape — a volunteer pin sets VolunteerID, a custom
// entry sets CustomValue, and Role is "Team lead" or "Service volunteer".
type ManualPreallocation struct {
	ID          string // UUID
	ShiftID     string // UUID
	Role        string
	VolunteerID string // nullable
	CustomValue string // nullable
}

// Cover represents a database cover record (audit trail for rota changes)
type Cover struct {
	ID        string // UUID
	CreatedAt string // TIMESTAMPTZ
	Reason    string
	UserEmail string
}

// Alteration represents a database alteration record (individual change to a
// shift). Like Allocation it is keyed solely by ShiftID; rota and date live on
// the referenced shift (ADR 0001).
type Alteration struct {
	ID          string // UUID
	ShiftID     string // UUID
	Direction   string // "add" or "remove"
	VolunteerID string // nullable
	CustomValue string // nullable
	CoverID     string // UUID
	SetTime     string // TIMESTAMPTZ
	Role        string // nullable - role for "add" alterations
}
