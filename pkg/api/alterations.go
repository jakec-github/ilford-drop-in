package api

import (
	"encoding/json"
	"net/http"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

type createAlterationRequest struct {
	Date      string `json:"date"`
	In        string `json:"in,omitempty"`
	Out       string `json:"out,omitempty"`
	InCustom  string `json:"inCustom,omitempty"`
	OutCustom string `json:"outCustom,omitempty"`
	SwapDate  string `json:"swapDate,omitempty"`
	Reason    string `json:"reason"`
	UserEmail string `json:"userEmail"`
}

type alterationResponse struct {
	ID          string `json:"id"`
	ShiftDate   string `json:"shiftDate"`
	Direction   string `json:"direction"`
	VolunteerID string `json:"volunteerId,omitempty"`
	CustomValue string `json:"customValue,omitempty"`
	Role        string `json:"role,omitempty"`
}

type createAlterationResponse struct {
	CoverID     string               `json:"coverId"`
	Alterations []alterationResponse `json:"alterations"`
}

func (h *Handler) handleCreateAlteration(w http.ResponseWriter, r *http.Request) {
	var req createAlterationRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	// The CLI derives the audit email from the OAuth token; the API has no
	// auth yet, so the caller must supply it
	if req.UserEmail == "" {
		h.writeError(w, http.StatusBadRequest, "userEmail is required")
		return
	}

	params := services.ChangeRotaParams{
		Date:      req.Date,
		In:        req.In,
		Out:       req.Out,
		InCustom:  req.InCustom,
		OutCustom: req.OutCustom,
		SwapDate:  req.SwapDate,
		Reason:    req.Reason,
		UserEmail: req.UserEmail,
	}

	result, err := services.ChangeRota(r.Context(), h.store, h.volunteers, h.cfg, params, h.logger)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	h.writeJSON(w, http.StatusCreated, createAlterationResponse{
		CoverID:     result.CoverID,
		Alterations: toAlterationResponses(result.Alterations, result.DatesByShiftID),
	})
}

// toAlterationResponses renders each alteration's shift date from the change
// result's shift-id->date map, keeping the JSON contract's shiftDate field
// while alterations themselves are keyed by shift id (ADR 0001).
func toAlterationResponses(alterations []db.Alteration, datesByShiftID map[string]string) []alterationResponse {
	responses := make([]alterationResponse, 0, len(alterations))
	for _, a := range alterations {
		responses = append(responses, alterationResponse{
			ID:          a.ID,
			ShiftDate:   datesByShiftID[a.ShiftID],
			Direction:   a.Direction,
			VolunteerID: a.VolunteerID,
			CustomValue: a.CustomValue,
			Role:        a.Role,
		})
	}
	return responses
}
