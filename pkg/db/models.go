package db

import "time"

// Rotation represents a database rotation record
type Rotation struct {
	ID                string
	Start             string // DATE
	End               string // DATE
	ShiftCount        int
	AllocatedDatetime string // TIMESTAMPTZ, empty string if NULL
	// InputsChangedAt is when an allocator input last moved under this
	// Rotation, zero when none has. A time rather than a string, unlike the
	// dates above, because nothing renders it: it exists to be compared with
	// the stamp its Draft Rota Allocation captured, and the two being different
	// is what makes a draft dirty (issue #142).
	InputsChangedAt time.Time
}

// Shift represents a database shift record: a planned session of the drop-in on
// a specific date, minted by a rotation and existing independently of who is
// allocated to it.
type Shift struct {
	ID     string // UUID
	RotaID string // UUID
	// Date is the day the session runs, "2006-01-02". It is derived from
	// StartAt rather than stored (shiftDateExpr, ADR 0007), so it is an answer
	// this package gives and never one it takes: a writer states StartAt and
	// the date follows. Setting it on the way in is ignored.
	Date string
	// Closed is a date the drop-in does not run — a holiday closure. Set by
	// hand while the rota is unallocated and false at mint; there is no stored
	// list of known closure dates (issue #132, amending ADR 0001).
	Closed bool
	// StartAt and EndAt are when the session runs, spelled
	// "2006-01-02T15:04:05". They are local wall-clock times in the drop-in's
	// own zone — TIMESTAMP without time zone, carrying no offset, because a
	// Shift's start is a fact about Ilford rather than an instant on a global
	// timeline (ADR 0007).
	//
	// Every Shift has both. A Shift with no start would have no Date either,
	// which is what issue #135 made impossible when it dropped the stored copy.
	StartAt string // TIMESTAMP
	EndAt   string // TIMESTAMP
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

// DraftRotaAllocation is a Rotation's Draft Rota Allocation: the speculative
// rota solved from whatever availability, Shapes and pins existed at the time,
// replaced entire each time it is solved (ADR 0008). One per Rotation, and only
// ever for an unallocated one.
//
// It is the draft's outcome rather than its content — the Seats are
// DraftAllocation rows — because an infeasible solve and a rota nobody has
// solved yet both store no Seats, and an admin needs to tell those apart.
type DraftRotaAllocation struct {
	RotaID       string // UUID
	SolvedAt     time.Time
	Success      bool
	SolverStatus string
	// ObjectiveValue is what the solver scored the rota it found. It is
	// meaningless when Success is false.
	ObjectiveValue int
	// Diagnostics is the solver's own diagnostic bag, stored and returned as
	// the JSON it arrived as. This package never looks inside it: the shape
	// belongs to the allocator, which this layer does not import.
	Diagnostics []byte
	// InputsChangedAt is the Rotation's own stamp as it stood when this solve
	// began. It is dirtiness by comparison: a Rotation whose stamp has moved on
	// since has had an input change the draft has not seen (issue #142).
	InputsChangedAt time.Time
	// SeatsAsked and SeatsFilled are the solve's own report of what it faced:
	// every Seat of every open Shift's Shape, and the ones it staffed. Stored
	// rather than counted from the Seats, because the Shapes go on being edited
	// after the solve and would answer for a question this draft was never
	// asked.
	SeatsAsked  int
	SeatsFilled int
}

// DraftAllocation is one Seat of a Draft Rota Allocation: exactly the shape of
// an Allocation, because it becomes one when the rota is allocated. Like
// Allocation it is keyed solely by ShiftID; rota and date live on the
// referenced shift (ADR 0001).
type DraftAllocation struct {
	ID          string // UUID
	ShiftID     string // UUID
	Role        string
	VolunteerID string
	CustomEntry string
}

// Preallocation represents a database preallocation record: a person pinned to
// a shift before allocation runs. Like Allocation it is keyed solely by ShiftID;
// rota and date live on the referenced shift (ADR 0001). It mirrors the
// allocation row shape — a volunteer pin sets VolunteerID, a custom entry sets
// CustomValue, and RoleID names the Seat it fills.
//
// RoleID rather than a Role name, unlike Allocation and Alteration beside it
// (issue #195). Those two record what happened and keep the name they happened
// under; a pin is a promise about what the solver must still do, so it is a
// live question like a Shift's Shape and has to survive a rename the way one
// does.
//
// There is one kind of these however it came to exist (issue #131): a row an
// admin added by hand and a row a Standing Preallocation seeded at definition
// are the same thing, and either may be removed.
type Preallocation struct {
	ID          string // UUID
	ShiftID     string // UUID
	RoleID      string // UUID, references role(id)
	VolunteerID string // nullable
	CustomValue string // nullable
}

// StandingPreallocation is a Preallocation an admin expects to make every rota,
// held in the Rota Defaults and used to seed real ones when a Rotation is
// defined (issue #131). RRule says which of the rota's Shifts it lands on, the
// same recurrence-rule vocabulary the config's Rota Overrides used.
//
// RoleID rather than a Role name: these outlive any number of rotas and a Role
// may be renamed at any time, so the id is the only reference that survives it.
// The Preallocations it seeds hold the id too, for the same reason (issue #195).
type StandingPreallocation struct {
	ID          string // UUID
	RRule       string
	RoleID      string // UUID, references role(id)
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
