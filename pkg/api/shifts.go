package api

import (
	"encoding/json"
	"net/http"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
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
	Start  string `json:"start"`
	End    string `json:"end"`
	Closed bool   `json:"closed"`
	// Shape is what this shift asks for, in the order its Seats are filled. It
	// is the shift's own copy rather than the settings' one, so two shifts of a
	// rota may differ — which is what per-shift editing is for (issue #138).
	// Never null: a shift nobody has stated a Shape for asks for nobody, which
	// is an empty list rather than an absence to guard against.
	//
	// Public, like the roles it names: what a shift is asking for is the same
	// kind of fact as when it runs, and the rota page renders it for whoever is
	// looking. Editing it is admin-only.
	Shape     []seatResponse     `json:"shape"`
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
			Shape:     toSeatResponses(shift.Shape),
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

// shiftShapeRequest is what one Shift asks for, stated whole. A Role missing
// from `seats` is a Role that Shift no longer asks for — there is no other way
// to say it, and no way to say it one Seat at a time, which is why this is a
// PUT of the Shape rather than a PATCH of the Shift beside the fields above.
type shiftShapeRequest struct {
	Seats []seatRequest `json:"seats"`
}

// shiftShapeResponse is the Shape as it now stands, in the order its Seats are
// filled. Not the whole shift: the client re-reads the rota anyway, and the one
// thing this endpoint changed is the one thing worth answering with.
type shiftShapeResponse struct {
	Shape []seatResponse `json:"shape"`
}

// handleSaveShiftShape rewrites what one Shift asks for. Refusals map to
// 400/404/409 via writeServiceError — a Seat past a Role's ceiling, an unknown
// shift, an allocated rota whose Shapes are fixed, or a Preallocation the new
// Shape would leave without a Seat.
func (h *Handler) handleSaveShiftShape(w http.ResponseWriter, r *http.Request) {
	var req shiftShapeRequest
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

	shape, err := services.SaveShiftShape(r.Context(), h.store, r.PathValue("id"), seats, h.logger)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, shiftShapeResponse{Shape: toSeatResponses(shape)})
}

// toSeatResponses renders a Shape as the Seats a client reads. Never null, so a
// Shape asking for nobody is an empty list on the wire rather than a null a
// caller has to guard against.
func toSeatResponses(shape model.Shape) []seatResponse {
	seats := make([]seatResponse, 0, len(shape))
	for _, seat := range shape {
		seats = append(seats, seatResponse{
			RoleID: seat.Role.ID,
			Role:   seat.Role.Name,
			Count:  seat.Count,
		})
	}
	return seats
}
