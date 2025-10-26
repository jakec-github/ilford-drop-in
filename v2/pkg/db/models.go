package db

// Rotation represents a database rotation record
type Rotation struct {
	ID         string `ssql_header:"id" ssql_type:"uuid"`
	Start      string `ssql_header:"start" ssql_type:"date"`
	ShiftCount int    `ssql_header:"shift_count" ssql_type:"int"`
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

// Cover represents a database cover record
type Cover struct {
	ID                  string `ssql_header:"id" ssql_type:"uuid"`
	RotaID              string `ssql_header:"rota_id" ssql_type:"uuid"`
	ShiftDate           string `ssql_header:"shift_date" ssql_type:"date"`
	CoveredVolunteerID  string `ssql_header:"covered_volunteer_id" ssql_type:"text"`
	CoveringVolunteerID string `ssql_header:"covering_volunteer_id" ssql_type:"text"`
}
