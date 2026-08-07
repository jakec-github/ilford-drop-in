package api

import (
	"encoding/json"
	"net/http"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

type shiftResponse struct {
	// ID is how a client addresses one shift to change it. Dates are the
	// external language everywhere else, but identity is the UUID (ADR 0001).
	ID   string `json:"id"`
	Date string `json:"date"`
	// Start and End are when the shift runs, as the shift itself holds them:
	// local wall-clock times in the drop-in's own zone, "2026-01-11T19:30:00",
	// with no offset on the end of them.
	//
	// The absent offset is the answer rather than a missing one (ADR 0007). A
	// shift's start is a fact about Ilford, so the rota says 19:30 to everyone
	// reading it, including a volunteer reading it from another country — where
	// an instant would be rendered as their own evening, which is not when the
	// drop-in runs. Conversion to a moment belongs where it is actually needed,
	// and the one place that needs it is the calendar feed, which does it
	// server-side.
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

	resp := listShiftsResponse{Shifts: make([]shiftResponse, 0, len(shifts))}
	for _, shift := range shifts {
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
			Start:     shift.StartAt,
			End:       shift.EndAt,
			Closed:    shift.Closed,
			Allocated: shift.Allocated,
			Assignees: assignees,
		})
	}

	h.writeJSON(w, http.StatusOK, resp)
}

// updateShiftRequest is the shape of a per-Shift edit. Every field is optional
// and an absent one is left alone, so changing when a shift runs does not mean
// restating whether it runs at all. Closed is a pointer for that reason: it is
// what makes "open it" distinguishable from saying nothing about it.
//
// Start and End are spelled exactly as the listing spells them — local
// wall-clock time, no offset — which is also the value a browser's
// datetime-local field carries, seconds and all.
type updateShiftRequest struct {
	Closed *bool  `json:"closed"`
	Start  string `json:"start"`
	End    string `json:"end"`
}

// shiftUpdateResponse is what an edit answers with: the shift's id, when it now
// runs, and whether it is closed. Not the full shift view — the client re-reads
// the rota anyway, and a change should not have to assemble a projection that
// needs the roster.
type shiftUpdateResponse struct {
	ID     string `json:"id"`
	Date   string `json:"date"`
	Start  string `json:"start"`
	End    string `json:"end"`
	Closed bool   `json:"closed"`
}

// handleUpdateShift changes one Shift: whether it is closed, when it runs, or
// both. Refusals map to 400/404/409 via writeServiceError — an unknown shift, a
// closure against an already-allocated rota, or a start landing on a day
// another shift already holds.
func (h *Handler) handleUpdateShift(w http.ResponseWriter, r *http.Request) {
	var req updateShiftRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	shift, err := services.UpdateShift(r.Context(), h.store, r.PathValue("id"), services.UpdateShiftParams{
		Closed:  req.Closed,
		StartAt: req.Start,
		EndAt:   req.End,
	}, h.logger)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, shiftUpdateResponse{
		ID:     shift.ID,
		Date:   shift.Date,
		Start:  shift.StartAt,
		End:    shift.EndAt,
		Closed: shift.Closed,
	})
}
