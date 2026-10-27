package api

import (
	"net/http"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// overrideRequest is an Override as an Organiser states it, whole: the rule,
// the date it counts from, and what it changes on the Shifts it matches.
//
// Shape is null to leave the default Shape, and a list — possibly empty, a
// Shift that asks for nobody — to replace it whole. A Role it leaves out is a
// Role it asks for none of.
//
// StartTime, "15:04", moves the Shifts it matches within their own day, and is
// null to leave the start the pattern generated. LengthMinutes is how long
// they run, and null for the pattern's length.
type overrideRequest struct {
	RRule         string               `json:"rrule"`
	StartFrom     string               `json:"startFrom"`
	StartTime     *string              `json:"startTime"`
	LengthMinutes *int                 `json:"lengthMinutes"`
	Shape         *[]seatRequest       `json:"shape"`
	Pins          []overridePinRequest `json:"pins"`
}

// overridePinRequest is one Pin: a Role by id, and exactly one of a volunteer
// or a custom entry.
type overridePinRequest struct {
	RoleID      string `json:"roleId"`
	VolunteerID string `json:"volunteerId,omitempty"`
	Custom      string `json:"custom,omitempty"`
}

type overrideOrderRequest struct {
	IDs []string `json:"ids"`
}

type overrideRuleRequest struct {
	RRule     string `json:"rrule"`
	StartFrom string `json:"startFrom"`
}

type listOverridesResponse struct {
	Overrides []overrideResponse `json:"overrides"`
}

// overrideResponse is one Override, with its rule read by the server's one
// evaluator: in plain English, or the problems a rule written by an older
// build has. Granularity is "time" for an Override that matches the Shift
// generated at the time it names, "date" for one that matches every Shift on
// its dates.
//
// StartTime, LengthMinutes and Shape are null where the Override leaves the
// pattern's start, its length, and the default Shape.
type overrideResponse struct {
	ID            string                `json:"id"`
	Number        int                   `json:"number"`
	RRule         string                `json:"rrule"`
	StartFrom     string                `json:"startFrom"`
	Granularity   string                `json:"granularity"`
	Description   string                `json:"description"`
	Problems      []string              `json:"problems"`
	StartTime     *string               `json:"startTime"`
	LengthMinutes *int                  `json:"lengthMinutes"`
	Shape         *[]seatResponse       `json:"shape"`
	Pins          []overridePinResponse `json:"pins"`
}

// overridePinResponse carries the Role and the person both ways, the id that
// an edit sends back and the name an Organiser recognises.
type overridePinResponse struct {
	ID          string `json:"id"`
	RoleID      string `json:"roleId"`
	Role        string `json:"role"`
	VolunteerID string `json:"volunteerId,omitempty"`
	Custom      string `json:"custom,omitempty"`
	Name        string `json:"name"`
}

// overrideRuleResponse is what a rule being written says, for the screen it is
// written on. Granularity is empty while it has problems.
type overrideRuleResponse struct {
	StartFrom   string   `json:"startFrom"`
	Granularity string   `json:"granularity"`
	Description string   `json:"description"`
	Problems    []string `json:"problems"`
}

func (h *Handler) handleListOverrides(w http.ResponseWriter, r *http.Request) {
	h.writeOverrides(w, r, http.StatusOK)
}

// handleAddOverride adds an Override at the end of the list, where it wins
// over every one before it.
func (h *Handler) handleAddOverride(w http.ResponseWriter, r *http.Request) {
	var req overrideRequest
	if err := decodeStrict(r, &req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if err := services.AddOverride(r.Context(), h.store, h.volunteers, h.org, toOverrideParams(req), h.logger); err != nil {
		h.writeServiceError(w, err)
		return
	}
	h.writeOverrides(w, r, http.StatusCreated)
}

// handleUpdateOverride rewrites an Override whole, in its place in the list.
// PUT and whole for the reason the Shape's save is: a Pin or a Seat dropped is
// something no partial write could say.
func (h *Handler) handleUpdateOverride(w http.ResponseWriter, r *http.Request) {
	var req overrideRequest
	if err := decodeStrict(r, &req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if err := services.UpdateOverride(r.Context(), h.store, h.volunteers, h.org, r.PathValue("id"), toOverrideParams(req), h.logger); err != nil {
		h.writeServiceError(w, err)
		return
	}
	h.writeOverrides(w, r, http.StatusOK)
}

// handleRemoveOverride deletes an Override. The Pins it has already made on
// defined rotas stay, as ordinary Pins.
func (h *Handler) handleRemoveOverride(w http.ResponseWriter, r *http.Request) {
	if err := services.RemoveOverride(r.Context(), h.store, r.PathValue("id"), h.logger); err != nil {
		h.writeServiceError(w, err)
		return
	}
	h.writeOverrides(w, r, http.StatusOK)
}

// handleReorderOverrides puts the Overrides in the order given, every one
// named once, the last winning.
func (h *Handler) handleReorderOverrides(w http.ResponseWriter, r *http.Request) {
	var req overrideOrderRequest
	if err := decodeStrict(r, &req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if err := services.ReorderOverrides(r.Context(), h.store, req.IDs, h.logger); err != nil {
		h.writeServiceError(w, err)
		return
	}
	h.writeOverrides(w, r, http.StatusOK)
}

// handleReadOverrideRule reads a rule that is still being written: what it
// says, what it matches, or what is wrong with it. POST because it carries a
// rule the server has never seen; it writes nothing.
//
// It is how the screen describes an Override without an evaluator of its own
// (ADR 0013).
func (h *Handler) handleReadOverrideRule(w http.ResponseWriter, r *http.Request) {
	var req overrideRuleRequest
	if err := decodeStrict(r, &req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	reading, err := services.ReadOverrideRule(r.Context(), h.store, req.RRule, req.StartFrom)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, overrideRuleResponse{
		StartFrom:   reading.StartFrom,
		Granularity: reading.Granularity,
		Description: reading.Description,
		Problems:    emptyIfNil(reading.Problems),
	})
}

// writeOverrides answers with the Overrides as they now stand.
func (h *Handler) writeOverrides(w http.ResponseWriter, r *http.Request, status int) {
	views, err := services.ListOverrides(r.Context(), h.store, h.volunteers, h.org, h.logger)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	resp := listOverridesResponse{Overrides: make([]overrideResponse, 0, len(views))}
	for _, v := range views {
		o := overrideResponse{
			ID:          v.ID,
			Number:      v.Number,
			RRule:       v.RRule,
			StartFrom:   v.StartFrom,
			Granularity: v.Granularity,
			Description: v.Description,
			Problems:    emptyIfNil(v.Problems),
			Pins:        make([]overridePinResponse, 0, len(v.Pins)),
		}
		if v.StartTime != "" {
			start := v.StartTime
			o.StartTime = &start
		}
		if v.LengthMinutes != 0 {
			length := v.LengthMinutes
			o.LengthMinutes = &length
		}
		if v.Shape != nil {
			seats := toSeatResponses(v.Shape)
			o.Shape = &seats
		}
		for _, p := range v.Pins {
			o.Pins = append(o.Pins, overridePinResponse{
				ID:          p.ID,
				RoleID:      p.RoleID,
				Role:        p.Role,
				VolunteerID: p.VolunteerID,
				Custom:      p.Custom,
				Name:        p.Name,
			})
		}
		resp.Overrides = append(resp.Overrides, o)
	}
	h.writeJSON(w, status, resp)
}

func toOverrideParams(req overrideRequest) services.OverrideParams {
	params := services.OverrideParams{RRule: req.RRule, StartFrom: req.StartFrom, LengthMinutes: req.LengthMinutes}
	if req.StartTime != nil {
		params.StartTime = *req.StartTime
	}
	if req.Shape != nil {
		params.Shape = make([]services.SeatParams, 0, len(*req.Shape))
		for _, seat := range *req.Shape {
			params.Shape = append(params.Shape, services.SeatParams{RoleID: seat.RoleID, Count: seat.Count})
		}
	}
	for _, p := range req.Pins {
		params.Pins = append(params.Pins, services.OverridePinParams{
			RoleID:      p.RoleID,
			VolunteerID: p.VolunteerID,
			Custom:      p.Custom,
		})
	}
	return params
}
