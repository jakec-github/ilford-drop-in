package db

import "time"

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
	// Closed is a date the drop-in does not run — a holiday closure. Set by
	// hand while the rota is unallocated and false at mint; there is no stored
	// list of known closure dates (issue #132, amending ADR 0001).
	Closed bool
}

// AvailabilityRequest is a tokenised availability request: one volunteer's
// invitation to answer for one rota, addressed by an unguessable link rather
// than a login. It is rota-scoped — one per volunteer per rota — and carries no
// shift date.
//
// It ran as availability_request_v2 alongside the Google Forms table through the
// expand phase (migration 011) and took the plain name back when the contract
// migration dropped it (012).
//
// SentAt is empty until a send stamps it: minting and sending are separate
// operations, so a volunteer holding a link nobody has sent them is an ordinary
// state.
type AvailabilityRequest struct {
	ID          string // UUID
	RotaID      string // UUID
	VolunteerID string
	Token       string
	SentAt      string // TIMESTAMPTZ, empty string if NULL
}

// Answers a volunteer may give for a shift. Only positives are stored — an
// absent row is a no — so there is no NO to record. PREFERRED has no consumer
// yet (ADR 0004).
const (
	AnswerYes       = "YES"
	AnswerPreferred = "PREFERRED"
)

// ShiftAnswer is one positive within a generation: a shift the volunteer said
// yes to, and how emphatically.
type ShiftAnswer struct {
	ShiftID string // UUID
	Answer  string
}

// AvailabilityGeneration is one complete submission. Answers holds every shift
// that submission said yes to, and is empty for a volunteer who submitted
// nothing — a state the Forms encoding could not express, and which reads
// differently from never having replied (no generation at all).
type AvailabilityGeneration struct {
	RequestID   string // UUID of the availability_request row
	ResponseID  string // UUID
	SubmittedAt time.Time
	Answers     []ShiftAnswer
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
