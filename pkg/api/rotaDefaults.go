package api

import (
	"encoding/json"
	"net/http"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// rotaDefaultsResponse is the settings record: what an admin has decided about
// how the drop-in as a whole runs. It holds the shift times and the default
// Shape; the allocation toggles join them here in a later ticket.
//
// Times are the same "19:30" the database and the form field spell, not
// timestamps: a time of day is not a moment until it is read against a date.
// Empty means an admin has not set it, which is the ordinary first state of a
// deployment rather than an error — so the client has a state to render, not
// only a value.
//
// Timezone is the one an admin chose, or the default when they have not chosen.
// The fallback is resolved here rather than in the client so there is one answer
// to what zone the drop-in runs in.
type rotaDefaultsResponse struct {
	ShiftStartTime string `json:"shiftStartTime"`
	ShiftEndTime   string `json:"shiftEndTime"`
	ShiftTimezone  string `json:"shiftTimezone"`
	// DefaultShape is what every Shift asks for, in the order its Seats are
	// filled. Never null: a Shape nobody has stated is an empty list, which is a
	// state to render rather than an absence to guard against.
	DefaultShape []seatResponse `json:"defaultShape"`
	// AllocationSettings is which optional allocator rules apply, and
	// SwitchableConstraints is which rules there are to apply. Both, because
	// the screen renders one list of toggles and cannot draw it from the
	// answers alone — a rule nobody has answered still has to appear, switched
	// off. Sending the registry also means the client holds no copy of it: the
	// list in Go is the only one (ADR 0006).
	AllocationSettings    allocationSettingsResponse `json:"allocationSettings"`
	SwitchableConstraints []switchableConstraint     `json:"switchableConstraints"`
}

// switchableConstraint is one optional allocator rule as the screen needs it:
// the name it is answered under, and the words to put beside the switch.
type switchableConstraint struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
	// ValueLabel names the extra answer this rule needs, empty for the rules
	// that need none. Only max_frequency has one.
	ValueLabel string `json:"valueLabel,omitempty"`
}

// allocationSettingsResponse is an admin's answers.
//
// Enabled carries an entry for every rule in the registry, including the ones
// nobody has answered — the rule that an unanswered constraint is off is
// settled here rather than in the client, so there is one place it is stated.
type allocationSettingsResponse struct {
	Enabled      map[string]bool `json:"enabled"`
	MaxFrequency float64         `json:"maxFrequency"`
}

// allocationSettingsRequest is the allocation-settings section of the settings
// screen, stated whole for the same reason the shift times are: the screen
// shows every rule at once, and a partial write could not express switching one
// off.
type allocationSettingsRequest struct {
	Enabled      map[string]bool `json:"enabled"`
	MaxFrequency float64         `json:"maxFrequency"`
}

// seatResponse is one line of a Shape: this many of this Role.
//
// The Role travels as both an id and a name because they answer different
// questions — the id is what an edit names and what the row holds, the name is
// what an admin recognises and what the colour is keyed on elsewhere.
type seatResponse struct {
	RoleID string `json:"roleId"`
	Role   string `json:"role"`
	Count  int    `json:"count"`
}

// shiftTimesRequest is the shift-time section of the settings screen. All three
// fields are stated together, because a time of day means nothing without the
// zone it is read in; timezone may be left out, and means the default.
type shiftTimesRequest struct {
	ShiftStartTime string `json:"shiftStartTime"`
	ShiftEndTime   string `json:"shiftEndTime"`
	ShiftTimezone  string `json:"shiftTimezone"`
}

// defaultShapeRequest is the Shape section of the settings screen, stated
// whole. A Role the Shape no longer asks for is a Role missing from `seats` —
// there is no other way to say it, and no way to say it one Seat at a time.
type defaultShapeRequest struct {
	Seats []seatRequest `json:"seats"`
}

// seatRequest is one Seat an admin is asking for. Zero is not a value it can
// take: a Role asked for nought times is a Role the Shape does not name, and
// the server says so rather than quietly dropping it.
type seatRequest struct {
	RoleID string `json:"roleId"`
	Count  int    `json:"count"`
}

// handleGetRotaDefaults reports the settings record.
//
// Admin-only, unlike the Roles listing beside it on the same screen. Roles are
// public because the rota is coloured by them and a logged-out visitor would
// otherwise read a colourless rota; nothing a visitor sees needs this. The
// times themselves are not a secret — GET /api/shifts has always carried them —
// but this is the settings screen's own resource, and the sections joining it
// are an admin's business.
func (h *Handler) handleGetRotaDefaults(w http.ResponseWriter, r *http.Request) {
	defaults, err := services.RotaDefaults(r.Context(), h.store)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	shape, err := services.DefaultShape(r.Context(), h.store)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, toRotaDefaultsResponse(defaults, shape))
}

// handleSaveShiftTimeDefaults writes the default shift start, end and timezone
// and answers with the settings as they now stand — including the timezone that
// was filled in for an admin who left it blank.
//
// PUT rather than PATCH: the three are one form, stated whole, and a partial
// write of them could express a start with no end, which describes nothing.
func (h *Handler) handleSaveShiftTimeDefaults(w http.ResponseWriter, r *http.Request) {
	var req shiftTimesRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	defaults, err := services.SaveShiftTimeDefaults(r.Context(), h.store, services.ShiftTimeParams{
		Start:    req.ShiftStartTime,
		End:      req.ShiftEndTime,
		Timezone: req.ShiftTimezone,
	}, h.logger)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	h.writeSettings(w, r, defaults)
}

// handleSaveDefaultShape writes the Shape every Shift starts from and answers
// with the settings as they now stand.
//
// PUT and whole, like the shift times beside it and for a stronger reason: a
// Role dropped from a Shape is a Role the Shape no longer asks for, which no
// partial write could express.
func (h *Handler) handleSaveDefaultShape(w http.ResponseWriter, r *http.Request) {
	var req defaultShapeRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	seats := make([]services.SeatParams, 0, len(req.Seats))
	for _, seat := range req.Seats {
		seats = append(seats, services.SeatParams{RoleID: seat.RoleID, Count: seat.Count})
	}

	if _, err := services.SaveDefaultShape(r.Context(), h.store, seats, h.logger); err != nil {
		h.writeServiceError(w, err)
		return
	}

	// The record is re-read rather than assembled from the save, because the
	// answer is the whole settings record and this endpoint only wrote a part
	// of it.
	defaults, err := services.RotaDefaults(r.Context(), h.store)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	h.writeSettings(w, r, defaults)
}

// writeSettings answers with the settings record whole, whichever section was
// just written. A section's save returns the record rather than the section, so
// a client holds one thing rather than stitching two answers together.
func (h *Handler) writeSettings(w http.ResponseWriter, r *http.Request, defaults model.RotaDefaults) {
	shape, err := services.DefaultShape(r.Context(), h.store)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, toRotaDefaultsResponse(defaults, shape))
}

func toRotaDefaultsResponse(defaults model.RotaDefaults, shape model.Shape) rotaDefaultsResponse {
	seats := make([]seatResponse, 0, len(shape))
	for _, seat := range shape {
		seats = append(seats, seatResponse{
			RoleID: seat.Role.ID,
			Role:   seat.Role.Name,
			Count:  seat.Count,
		})
	}

	constraints := make([]switchableConstraint, 0, len(model.SwitchableConstraints))
	for _, c := range model.SwitchableConstraints {
		constraints = append(constraints, switchableConstraint{
			Name:        c.Name,
			Label:       c.Label,
			Description: c.Description,
			ValueLabel:  c.ValueLabel,
		})
	}

	return rotaDefaultsResponse{
		ShiftStartTime:        defaults.ShiftStartTime,
		ShiftEndTime:          defaults.ShiftEndTime,
		ShiftTimezone:         defaults.Timezone(),
		DefaultShape:          seats,
		AllocationSettings:    toAllocationSettingsResponse(defaults.AllocationSettings),
		SwitchableConstraints: constraints,
	}
}

// toAllocationSettingsResponse states an answer for every rule that exists,
// so the client never has to know that a missing key means off.
func toAllocationSettingsResponse(settings model.AllocationSettings) allocationSettingsResponse {
	enabled := make(map[string]bool, len(model.SwitchableConstraints))
	for _, c := range model.SwitchableConstraints {
		enabled[c.Name] = settings.IsEnabled(c.Name)
	}

	return allocationSettingsResponse{Enabled: enabled, MaxFrequency: settings.MaxFrequency}
}

// handleSaveAllocationSettings writes which optional allocator rules apply and
// answers with the settings as they now stand — which is not always what was
// sent: an answer naming a rule this build does not have is dropped, and the
// reply is how a client working from an older list finds that out.
func (h *Handler) handleSaveAllocationSettings(w http.ResponseWriter, r *http.Request) {
	var req allocationSettingsRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	settings, err := services.SaveAllocationSettings(r.Context(), h.store, services.AllocationSettingsParams{
		Enabled:      req.Enabled,
		MaxFrequency: req.MaxFrequency,
	}, h.logger)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, toAllocationSettingsResponse(settings))
}
