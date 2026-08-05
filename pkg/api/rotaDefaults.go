package api

import (
	"encoding/json"
	"net/http"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// rotaDefaultsResponse is the settings record: what an admin has decided about
// how the drop-in as a whole runs. It holds the shift times today; the default
// Shape, the allocation toggles and the Standing Preallocations join it here in
// later tickets.
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
}

// rotaDefaultsRequest is the shift-time section of the settings screen. All
// three fields are stated together, because a time of day means nothing without
// the zone it is read in; timezone may be left out, and means the default.
type rotaDefaultsRequest struct {
	ShiftStartTime string `json:"shiftStartTime"`
	ShiftEndTime   string `json:"shiftEndTime"`
	ShiftTimezone  string `json:"shiftTimezone"`
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

	h.writeJSON(w, http.StatusOK, toRotaDefaultsResponse(defaults))
}

// handleSaveShiftTimeDefaults writes the default shift start, end and timezone
// and answers with the settings as they now stand — including the timezone that
// was filled in for an admin who left it blank.
//
// PUT rather than PATCH: the three are one form, stated whole, and a partial
// write of them could express a start with no end, which describes nothing.
func (h *Handler) handleSaveShiftTimeDefaults(w http.ResponseWriter, r *http.Request) {
	var req rotaDefaultsRequest
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

	h.writeJSON(w, http.StatusOK, toRotaDefaultsResponse(defaults))
}

func toRotaDefaultsResponse(defaults model.RotaDefaults) rotaDefaultsResponse {
	return rotaDefaultsResponse{
		ShiftStartTime: defaults.ShiftStartTime,
		ShiftEndTime:   defaults.ShiftEndTime,
		ShiftTimezone:  defaults.Timezone(),
	}
}
