package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// availabilityShiftResponse is one of the rota's shifts. Closed shifts are
// listed rather than omitted so a volunteer can see the drop-in is not running
// on that date, instead of wondering why it is missing.
type availabilityShiftResponse struct {
	ID     string `json:"id"`
	Date   string `json:"date"`
	Closed bool   `json:"closed"`
}

// availabilityEntryResponse is one volunteer's place in a round. The link is
// returned in full because copying it is how an admin distributes one out of
// band, and the fallback for a volunteer whose email bounces.
type availabilityEntryResponse struct {
	VolunteerID   string `json:"volunteerId"`
	VolunteerName string `json:"volunteerName"`
	Link          string `json:"link"`
	// Absent until their link has been emailed. Minting and sending are separate
	// operations, so "has a link nobody sent them" is an ordinary state — and it
	// is the one a round send acts on.
	SentAt            string   `json:"sentAt,omitempty"`
	Replied           bool     `json:"replied"`
	SubmittedAt       string   `json:"submittedAt,omitempty"`
	AvailableShiftIDs []string `json:"availableShiftIds"`
	CoveredBy         []string `json:"coveredBy,omitempty"`
}

// availabilityGroupResponse is a round at the grain allocation happens at. The
// group's availability is the group rule already applied, so no client has to
// re-derive it — the logic lives in one place (ADR 0004).
type availabilityGroupResponse struct {
	Key               string                      `json:"key"`
	Name              string                      `json:"name"`
	Replied           bool                        `json:"replied"`
	AvailableShiftIDs []string                    `json:"availableShiftIds"`
	Members           []availabilityEntryResponse `json:"members"`
}

// availabilityCoverageResponse is one shift's staffing picture: what it still
// needs, who is available for it, and whether it has a lead. A closed shift
// carries zeroes — it is not a shift that is short of people.
type availabilityCoverageResponse struct {
	ID          string `json:"id"`
	Date        string `json:"date"`
	Closed      bool   `json:"closed"`
	Needed      int    `json:"needed"`
	Pinned      int    `json:"pinned"`
	Available   int    `json:"available"`
	Delta       int    `json:"delta"`
	HasTeamLead bool   `json:"hasTeamLead"`
}

type availabilityRoundResponse struct {
	RotaID    string                         `json:"rotaId"`
	Start     string                         `json:"start"`
	End       string                         `json:"end"`
	Allocated bool                           `json:"allocated"`
	Shifts    []availabilityCoverageResponse `json:"shifts"`
	Groups    []availabilityGroupResponse    `json:"groups"`
}

// availabilityFormResponse is the volunteer's own view. It carries no ids that
// are not already implied by their link.
type availabilityFormResponse struct {
	VolunteerName    string                      `json:"volunteerName"`
	GroupMembers     []string                    `json:"groupMembers"`
	Shifts           []availabilityShiftResponse `json:"shifts"`
	SelectedShiftIDs []string                    `json:"selectedShiftIds"`
	Submitted        bool                        `json:"submitted"`
	SubmittedAt      string                      `json:"submittedAt,omitempty"`
}

type mintRoundRequest struct {
	RotaID string `json:"rotaId"`
}

type submitAvailabilityRequest struct {
	ShiftIDs []string `json:"shiftIds"`
}

// handleMintAvailabilityRound creates an availability request, with its own
// link, for every active volunteer on a rota. Admin-gated, and idempotent:
// running it again after the roster changes tops the round up without
// re-tokening anyone, so links already distributed keep working.
func (h *Handler) handleMintAvailabilityRound(w http.ResponseWriter, r *http.Request) {
	var req mintRoundRequest
	// An empty body means the latest rota, which is what the admin screen sends.
	if r.ContentLength != 0 {
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
	}

	round, err := services.MintAvailabilityRound(r.Context(), h.store, h.volunteers, h.cfg, h.logger, req.RotaID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	h.writeJSON(w, http.StatusCreated, toRoundResponse(round, r))
}

// handleGetAvailabilityRound reports where a round has got to: who was asked,
// their link, and who has answered. Admin-gated — it exposes every volunteer's
// bearer link.
func (h *Handler) handleGetAvailabilityRound(w http.ResponseWriter, r *http.Request) {
	round, err := services.GetAvailabilityRound(r.Context(), h.store, h.volunteers, h.cfg, h.logger, r.URL.Query().Get("rotaId"))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, toRoundResponse(round, r))
}

// handleAvailabilityForm serves what is behind a volunteer's link. It is public
// and unauthenticated: the link is the identity, and volunteers never log in.
//
// The link a volunteer is emailed is the page, /availability/{token}, which the
// frontend owns; this is the payload the page fetches from under /api. One
// concept, two URLs, and no guessing from an Accept header which of them a
// caller meant.
func (h *Handler) handleAvailabilityForm(w http.ResponseWriter, r *http.Request) {
	form, err := services.GetAvailabilityForm(r.Context(), h.store, h.volunteers, h.cfg, h.logger, r.PathValue("token"))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, toFormResponse(form))
}

// handleSubmitAvailability records one complete generation for a link.
//
// shiftIds is the whole answer, not a delta: an absent shift is a no, so a
// client that sent only its changes would silently record unavailability
// (ADR 0004). Submitting again is allowed and appends a further generation;
// there is no idempotency key because latest-wins makes a duplicate harmless.
func (h *Handler) handleSubmitAvailability(w http.ResponseWriter, r *http.Request) {
	var req submitAvailabilityRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	form, err := services.SubmitAvailability(r.Context(), h.store, h.volunteers, h.cfg, h.logger, r.PathValue("token"), req.ShiftIDs)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, toFormResponse(form))
}

// availabilityLink builds the absolute URL a volunteer is given — the page, not
// the endpoint behind it. It is derived from the request rather than configured,
// so a round minted through a dev stack on localhost yields links that work
// there.
func availabilityLink(r *http.Request, token string) string {
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		scheme = forwarded
	}
	return scheme + "://" + r.Host + "/availability/" + token
}

func toRoundResponse(round *services.AvailabilityRound, r *http.Request) availabilityRoundResponse {
	resp := availabilityRoundResponse{
		RotaID:    round.RotaID,
		Start:     round.RotaStart,
		End:       round.RotaEnd,
		Allocated: round.Allocated,
		Shifts:    make([]availabilityCoverageResponse, 0, len(round.Shifts)),
		Groups:    make([]availabilityGroupResponse, 0, len(round.Groups)),
	}
	for _, s := range round.Shifts {
		resp.Shifts = append(resp.Shifts, availabilityCoverageResponse{
			ID:          s.ShiftID,
			Date:        s.Date,
			Closed:      s.Closed,
			Needed:      s.Needed,
			Pinned:      s.Pinned,
			Available:   s.Available,
			Delta:       s.Delta,
			HasTeamLead: s.HasTeamLead,
		})
	}
	for _, g := range round.Groups {
		group := availabilityGroupResponse{
			Key:               g.Key,
			Name:              g.Name,
			Replied:           g.Replied,
			AvailableShiftIDs: g.AvailableShiftIDs,
			Members:           make([]availabilityEntryResponse, 0, len(g.Members)),
		}
		for _, e := range g.Members {
			member := availabilityEntryResponse{
				VolunteerID:       e.VolunteerID,
				VolunteerName:     e.VolunteerName,
				Link:              availabilityLink(r, e.Token),
				SentAt:            e.SentAt,
				Replied:           e.Replied,
				AvailableShiftIDs: e.AvailableShiftIDs,
				CoveredBy:         e.CoveredBy,
			}
			if !e.SubmittedAt.IsZero() {
				member.SubmittedAt = e.SubmittedAt.UTC().Format(time.RFC3339)
			}
			group.Members = append(group.Members, member)
		}
		resp.Groups = append(resp.Groups, group)
	}
	return resp
}

func toFormResponse(form *services.AvailabilityForm) availabilityFormResponse {
	resp := availabilityFormResponse{
		VolunteerName:    form.VolunteerName,
		GroupMembers:     form.GroupMembers,
		Shifts:           toShiftResponses(form.Shifts),
		SelectedShiftIDs: form.SelectedShiftIDs,
		Submitted:        form.Submitted,
	}
	if !form.SubmittedAt.IsZero() {
		resp.SubmittedAt = form.SubmittedAt.UTC().Format(time.RFC3339)
	}
	return resp
}

func toShiftResponses(shifts []services.AvailabilityShift) []availabilityShiftResponse {
	out := make([]availabilityShiftResponse, 0, len(shifts))
	for _, s := range shifts {
		out = append(out, availabilityShiftResponse{ID: s.ID, Date: s.Date, Closed: s.Closed})
	}
	return out
}
