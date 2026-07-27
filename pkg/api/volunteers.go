package api

import (
	"net/http"
	"sort"
	"strings"
)

// volunteerResponse is one entry of the roster. The fields mirror the assignee
// fields GET /shifts already returns, so a picker and a rota chip describe the
// same person the same way, plus active — the roster includes volunteers who
// have left, and the caller decides what to do with them.
type volunteerResponse struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Role   string `json:"role,omitempty"`
	Group  string `json:"group,omitempty"`
	Active bool   `json:"active"`
}

type listVolunteersResponse struct {
	Volunteers []volunteerResponse `json:"volunteers"`
}

// handleListVolunteers returns the synced volunteer roster, sorted by name. It
// is admin-gated: unlike the assignee names on GET /shifts, this exposes
// volunteer ids, groups, and everyone not currently on a shift.
//
// Inactive volunteers are listed and flagged rather than filtered out — an
// admin editing a past or unusual shift may legitimately need someone who has
// since stopped, and a picker that silently omits people is harder to explain
// than one that greys them out.
func (h *Handler) handleListVolunteers(w http.ResponseWriter, r *http.Request) {
	volunteers, err := h.volunteers.ListVolunteers(h.cfg)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	resp := listVolunteersResponse{Volunteers: make([]volunteerResponse, 0, len(volunteers))}
	for _, v := range volunteers {
		resp.Volunteers = append(resp.Volunteers, volunteerResponse{
			ID:     v.ID,
			Name:   v.DisplayName,
			Role:   string(v.Role),
			Group:  v.GroupKey,
			Active: strings.EqualFold(v.Status, "Active"),
		})
	}

	// Sheet order is not meaningful; sort so the picker's ordering is stable.
	sort.Slice(resp.Volunteers, func(i, j int) bool {
		if resp.Volunteers[i].Name != resp.Volunteers[j].Name {
			return resp.Volunteers[i].Name < resp.Volunteers[j].Name
		}
		return resp.Volunteers[i].ID < resp.Volunteers[j].ID
	})

	h.writeJSON(w, http.StatusOK, resp)
}
