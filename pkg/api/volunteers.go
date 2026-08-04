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
//
// Name and FullName are both carried because they answer different questions.
// Name is the display name: the shortest form that stays unambiguous across the
// roster ("Aaliyah", "John E."), which is what a rota chip has room for. FullName
// is always first plus last, which is what an admin looking at the roster itself
// wants — a screen with room for it should not make an admin guess which John.
//
// Gender is the sheet's own text, uncoerced: the column is free text, so any
// mapping to a fixed set of values would be this endpoint guessing on the
// admin's behalf. The allocator's gender balancing does its own matching
// (allocator.GenderMale), and so does anything counting the roster.
// Roles are every Role the volunteer holds, in priority order — a volunteer
// holds a set, not one, and which of them they fill is decided per shift.
type volunteerResponse struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	FullName string   `json:"fullName"`
	Roles    []string `json:"roles"`
	Group    string   `json:"group,omitempty"`
	Gender   string   `json:"gender,omitempty"`
	Active   bool     `json:"active"`
}

type listVolunteersResponse struct {
	Volunteers []volunteerResponse `json:"volunteers"`
}

// heldRoles renders the Roles a volunteer holds, keeping [] rather than null
// for someone who holds none — a client should not have to tell the two apart.
func heldRoles(roles []string) []string {
	if roles == nil {
		return []string{}
	}
	return roles
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
			ID: v.ID,
			// TrimSpace, not a bare join: a volunteer with no surname on the
			// sheet would otherwise get a trailing space.
			Name:     v.DisplayName,
			FullName: strings.TrimSpace(v.FirstName + " " + v.LastName),
			Roles:    heldRoles(v.Roles),
			Group:    v.GroupKey,
			Gender:   v.Gender,
			Active:   strings.EqualFold(v.Status, "Active"),
		})
	}

	// Sheet order is not meaningful; sort so the ordering is stable. Keyed on the
	// full name rather than the display name, because a roster renders full names
	// and the two disagree: a unique "Emma" keeps its display name and would sort
	// ahead of "Emma Welder", leaving the rendered list looking unsorted.
	sort.Slice(resp.Volunteers, func(i, j int) bool {
		if resp.Volunteers[i].FullName != resp.Volunteers[j].FullName {
			return resp.Volunteers[i].FullName < resp.Volunteers[j].FullName
		}
		return resp.Volunteers[i].ID < resp.Volunteers[j].ID
	})

	h.writeJSON(w, http.StatusOK, resp)
}
