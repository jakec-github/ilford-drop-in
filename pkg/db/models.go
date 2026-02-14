package db

// Rotation represents a database rotation record
type Rotation struct {
	ID                string `ssql_header:"id" ssql_type:"uuid"`
	Start             string `ssql_header:"start" ssql_type:"date"`
	ShiftCount        int    `ssql_header:"shift_count" ssql_type:"int"`
	AllocatedDatetime string // TIMESTAMPTZ from postgres, empty string if NULL
}

// AvailabilityRequest represents a database availability request record
type AvailabilityRequest struct {
	ID          string `ssql_header:"id" ssql_type:"uuid"`
	RotaID      string `ssql_header:"rota_id" ssql_type:"uuid"`
	ShiftDate   string `ssql_header:"shift_date" ssql_type:"date"`
	VolunteerID string `ssql_header:"volunteer_id" ssql_type:"text"`
	FormID      string `ssql_header:"form_id" ssql_type:"text"`
	FormURL     string `ssql_header:"form_url" ssql_type:"text"`
	FormSent    bool   `ssql_header:"form_sent" ssql_type:"bool"`
}

// Allocation represents a database allocation record
type Allocation struct {
	ID          string `ssql_header:"id" ssql_type:"uuid"`
	RotaID      string `ssql_header:"rota_id" ssql_type:"uuid"`
	ShiftDate   string `ssql_header:"shift_date" ssql_type:"date"`
	Role        string `ssql_header:"role" ssql_type:"text"`
	VolunteerID string `ssql_header:"volunteer_id" ssql_type:"text"`
	CustomEntry string `ssql_header:"custom_entry" ssql_type:"text"`
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
