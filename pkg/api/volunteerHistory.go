package api

import (
	"net/http"
	"strings"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// historyRotaCount is how many past rotas the History tab looks back over:
// enough to see a habit, few enough to read across a desk (issue #260).
const historyRotaCount = 8

type historyRotaResponse struct {
	ID    string `json:"id"`
	Start string `json:"start"`
	End   string `json:"end"`
}

// historyEntryResponse is one volunteer on one past rota. Availability is the
// band, not the bare count, because where the lines fall is a rule the server
// owns; available and openShifts are carried so a screen can say why.
type historyEntryResponse struct {
	Availability string `json:"availability"`
	Available    int    `json:"available"`
	OpenShifts   int    `json:"openShifts"`
	Given        int    `json:"given"`
	Attended     int    `json:"attended"`
}

type historyVolunteerResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	FullName string `json:"fullName"`
	Active   bool   `json:"active"`
	// History is one entry per rota, in the order of the response's rotations.
	History []historyEntryResponse `json:"history"`
}

type volunteerHistoryResponse struct {
	Rotations  []historyRotaResponse      `json:"rotations"`
	Volunteers []historyVolunteerResponse `json:"volunteers"`
}

// handleVolunteerHistory reports each volunteer's record across the last
// allocated rotas: how much they offered, how much they were given and how
// much they ended up on.
func (h *Handler) handleVolunteerHistory(w http.ResponseWriter, r *http.Request) {
	result, err := services.VolunteerHistory(r.Context(), h.store, h.volunteers, h.org, h.logger, historyRotaCount)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	resp := volunteerHistoryResponse{
		Rotations:  make([]historyRotaResponse, 0, len(result.Rotations)),
		Volunteers: make([]historyVolunteerResponse, 0, len(result.Volunteers)),
	}
	for _, rota := range result.Rotations {
		resp.Rotations = append(resp.Rotations, historyRotaResponse{ID: rota.ID, Start: rota.Start, End: rota.End})
	}
	for _, v := range result.Volunteers {
		entry := historyVolunteerResponse{
			ID:       v.ID,
			Name:     v.DisplayName,
			FullName: strings.TrimSpace(v.FirstName + " " + v.LastName),
			Active:   strings.EqualFold(v.Status, "Active"),
			History:  make([]historyEntryResponse, 0, len(result.Rotations)),
		}
		for _, rota := range result.Rotations {
			e := result.Entries[v.ID][rota.ID]
			entry.History = append(entry.History, historyEntryResponse{
				Availability: string(e.Band),
				Available:    e.Available,
				OpenShifts:   e.OpenShifts,
				Given:        e.Given,
				Attended:     e.Attended,
			})
		}
		resp.Volunteers = append(resp.Volunteers, entry)
	}
	h.writeJSON(w, http.StatusOK, resp)
}
