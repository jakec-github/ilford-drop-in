package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

type shiftResponse struct {
	// ID is how a client addresses one shift to change it. Dates are the
	// external language everywhere else, but identity is the UUID (ADR 0001).
	ID   string `json:"id"`
	Date string `json:"date"`
	// Start and End are the moments the shift runs between, read from the
	// drop-in's settings. Empty when an admin has not set the shift times yet:
	// the date is still known, and a rota that says which day but not which
	// hour is better than one that will not load. Incomplete settings block
	// allocation and nothing else (ADR 0006).
	Start     string             `json:"start"`
	End       string             `json:"end"`
	Closed    bool               `json:"closed"`
	Allocated bool               `json:"allocated"`
	Assignees []assigneeResponse `json:"assignees"`
}

type assigneeResponse struct {
	VolunteerID string `json:"volunteerId,omitempty"`
	CustomEntry string `json:"customEntry,omitempty"`
	Name        string `json:"name"`
	Role        string `json:"role,omitempty"`
	Group       string `json:"group,omitempty"`
}

type listShiftsResponse struct {
	Shifts []shiftResponse `json:"shifts"`
}

func (h *Handler) handleListShifts(w http.ResponseWriter, r *http.Request) {
	params := services.ListShiftsParams{
		From: r.URL.Query().Get("from"),
		To:   r.URL.Query().Get("to"),
	}

	shifts, err := services.ListShifts(r.Context(), h.store, h.volunteers, h.cfg, params, h.logger)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	// Read once for the whole listing rather than per shift: it is one row, and
	// every shift in a rota runs at the same time of day.
	defaults, err := services.RotaDefaults(r.Context(), h.store)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	resp := listShiftsResponse{Shifts: make([]shiftResponse, 0, len(shifts))}
	for _, shift := range shifts {
		var start, end string
		if defaults.HasShiftTimes() {
			startAt, endAt, err := defaults.ShiftTimes(shift.Date)
			if err != nil {
				h.writeServiceError(w, err)
				return
			}
			start, end = startAt.Format(time.RFC3339), endAt.Format(time.RFC3339)
		}

		assignees := make([]assigneeResponse, 0, len(shift.Assignees))
		for _, a := range shift.Assignees {
			assignees = append(assignees, assigneeResponse{
				VolunteerID: a.VolunteerID,
				CustomEntry: a.CustomEntry,
				Name:        a.Name,
				Role:        a.Role,
				Group:       a.Group,
			})
		}

		resp.Shifts = append(resp.Shifts, shiftResponse{
			ID:        shift.ID,
			Date:      shift.Date,
			Start:     start,
			End:       end,
			Closed:    shift.Closed,
			Allocated: shift.Allocated,
			Assignees: assignees,
		})
	}

	h.writeJSON(w, http.StatusOK, resp)
}

// updateShiftRequest is the shape of a per-Shift edit. Closed is a pointer so
// "leave it alone" and "open it" are distinguishable — which matters now the
// body has one field and will matter more when times and Shape join it.
type updateShiftRequest struct {
	Closed *bool `json:"closed"`
}

// shiftClosureResponse is what a close or reopen answers with: the shift's id,
// the date it names, and the state it is now in. Not the full shift view — the
// client re-reads the rota anyway, and a change should not have to assemble a
// projection that needs the roster.
type shiftClosureResponse struct {
	ID     string `json:"id"`
	Date   string `json:"date"`
	Closed bool   `json:"closed"`
}

// handleUpdateShift changes one Shift. Today the only editable field is closed;
// a shift's times and Shape land here as they become fields of their own.
// Refusals — an unknown shift, or a rota already allocated — map to 404/409 via
// writeServiceError.
func (h *Handler) handleUpdateShift(w http.ResponseWriter, r *http.Request) {
	var req updateShiftRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Closed == nil {
		h.writeError(w, http.StatusBadRequest, "closed is required")
		return
	}

	closure, err := services.SetShiftClosed(r.Context(), h.store, r.PathValue("id"), *req.Closed, h.logger)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, shiftClosureResponse{
		ID:     closure.ID,
		Date:   closure.Date,
		Closed: closure.Closed,
	})
}
