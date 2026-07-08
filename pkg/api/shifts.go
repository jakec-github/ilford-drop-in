package api

import (
	"net/http"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

type shiftResponse struct {
	Date      string             `json:"date"`
	Start     string             `json:"start"`
	End       string             `json:"end"`
	Closed    bool               `json:"closed"`
	Assignees []assigneeResponse `json:"assignees"`
}

type assigneeResponse struct {
	VolunteerID string `json:"volunteerId,omitempty"`
	CustomEntry string `json:"customEntry,omitempty"`
	Name        string `json:"name"`
	Role        string `json:"role,omitempty"`
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
		start, end, err := h.cfg.ShiftTimes(shift.Date)
		if err != nil {
			h.writeServiceError(w, err)
			return
		}

		assignees := make([]assigneeResponse, 0, len(shift.Assignees))
		for _, a := range shift.Assignees {
			assignees = append(assignees, assigneeResponse{
				VolunteerID: a.VolunteerID,
				CustomEntry: a.CustomEntry,
				Name:        a.Name,
				Role:        a.Role,
			})
		}

		resp.Shifts = append(resp.Shifts, shiftResponse{
			Date:      shift.Date,
			Start:     start.Format(time.RFC3339),
			End:       end.Format(time.RFC3339),
			Closed:    shift.Closed,
			Assignees: assignees,
		})
	}

	h.writeJSON(w, http.StatusOK, resp)
}
