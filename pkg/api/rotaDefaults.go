package api

import (
	"encoding/json"
	"net/http"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// rotaDefaultsResponse is the settings record: what an Organiser has decided about
// how the Organisation as a whole runs: the Shift pattern, the default Shape and
// the Allocation Rules.
//
// Empty means an Organiser has not set it, which is the ordinary first state of a
// deployment rather than an error — so the client has a state to render, not
// only a value.
//
// Timezone is the one an Organiser chose, or the default when they have not chosen.
// The fallback is resolved here rather than in the client so there is one answer
// to what zone the Organisation runs in.
type rotaDefaultsResponse struct {
	// ShiftPattern is the RRULE lines as an Organiser wrote them, and
	// PatternStartFrom the date they count from, "2006-01-02" (ADR 0013).
	ShiftPattern     string `json:"shiftPattern"`
	PatternStartFrom string `json:"patternStartFrom"`
	// ShiftLengthMinutes is how long each Shift runs; zero is unset.
	ShiftLengthMinutes int    `json:"shiftLengthMinutes"`
	ShiftTimezone      string `json:"shiftTimezone"`
	// DefaultShape is what every Shift asks for, in the order its Seats are
	// filled. Never null: a Shape nobody has stated is an empty list, which is a
	// state to render rather than an absence to guard against.
	DefaultShape []seatResponse `json:"defaultShape"`
	// AllocationRules are the rules an Organiser has added, in the order they
	// were added, and RuleLibrary is every kind there is to add. The library
	// travels with the rules so the client holds no copy of it: the list in Go
	// is the only one (ADR 0012).
	AllocationRules []allocationRuleResponse `json:"allocationRules"`
	RuleLibrary     []ruleKindResponse       `json:"ruleLibrary"`
}

// ruleKindResponse is one Rule Kind as the screen offers it.
type ruleKindResponse struct {
	ID          string `json:"id"`
	Family      string `json:"family"`
	Label       string `json:"label"`
	Description string `json:"description"`
	// ValueLabel names the one value a rule of this kind carries, empty for a
	// kind that carries none; MinValue and MaxValue bound it, inclusive.
	ValueLabel   string `json:"valueLabel,omitempty"`
	MinValue     int    `json:"minValue,omitempty"`
	MaxValue     int    `json:"maxValue,omitempty"`
	DefaultValue int    `json:"defaultValue,omitempty"`
	Input        string `json:"input,omitempty"`
	// OptionLabel names the one choice a rule of this kind carries, empty for
	// a kind that carries none; Options is every choice it accepts.
	OptionLabel string   `json:"optionLabel,omitempty"`
	Options     []string `json:"options,omitempty"`
}

// scopeBody is a Scope on the wire, both ways. The lists are never null on the
// way out, so the client has one shape to read.
type scopeBody struct {
	Include []string `json:"include"`
	Exclude []string `json:"exclude"`
}

// allocationRuleResponse is one stored rule.
//
// Ignored marks a rule this build cannot apply — a kind it does not have, or
// values that no longer validate. It is listed rather than hidden so an
// Organiser can see why it is not applying and take it off; the solve leaves it
// out either way (ADR 0006).
type allocationRuleResponse struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Value   int       `json:"value"`
	Option  string    `json:"option"`
	Scope   scopeBody `json:"scope"`
	Note    string    `json:"note"`
	Ignored bool      `json:"ignored"`
}

// addAllocationRuleRequest is a rule from the library with its values filled in.
type addAllocationRuleRequest struct {
	Kind   string    `json:"kind"`
	Value  int       `json:"value"`
	Option string    `json:"option"`
	Scope  scopeBody `json:"scope"`
	Note   string    `json:"note"`
}

// editAllocationRuleRequest is what can change about a rule in place. Its kind
// is what it is: a different kind is a different rule.
type editAllocationRuleRequest struct {
	Value  int       `json:"value"`
	Option string    `json:"option"`
	Scope  scopeBody `json:"scope"`
	Note   string    `json:"note"`
}

// seatResponse is one line of a Shape: this many of this Role.
//
// The Role travels as both an id and a name because they answer different
// questions — the id is what an edit names and what the row holds, the name is
// what an Organiser recognises and what the colour is keyed on elsewhere.
type seatResponse struct {
	RoleID string `json:"roleId"`
	Role   string `json:"role"`
	Count  int    `json:"count"`
}

// shiftPatternRequest is the Shift pattern section of the settings screen,
// stated whole. A blank patternStartFrom means today, and a blank
// shiftTimezone the default.
type shiftPatternRequest struct {
	ShiftPattern       string `json:"shiftPattern"`
	PatternStartFrom   string `json:"patternStartFrom"`
	ShiftTimezone      string `json:"shiftTimezone"`
	ShiftLengthMinutes int    `json:"shiftLengthMinutes"`
}

// shiftPatternPreviewRequest is a pattern being written, and the dates to
// show its Shifts between: from inclusive, to exclusive, both "2006-01-02".
type shiftPatternPreviewRequest struct {
	ShiftPattern     string `json:"shiftPattern"`
	PatternStartFrom string `json:"patternStartFrom"`
	ShiftTimezone    string `json:"shiftTimezone"`
	From             string `json:"from"`
	To               string `json:"to"`
}

// shiftPatternPreviewResponse is what a pattern says and where its Shifts
// start. PatternStartFrom is the date it was read from, filled in when the
// request left it blank. Starts are wall-clock, "2006-01-02T15:04:05", and
// empty while any line has problems.
type shiftPatternPreviewResponse struct {
	PatternStartFrom string                `json:"patternStartFrom"`
	Lines            []patternLineResponse `json:"lines"`
	Starts           []string              `json:"starts"`
}

// patternLineResponse is one non-blank line of a pattern: its plain-English
// description, or its problems. Number counts blank lines too, so it is the
// line an Organiser would count to.
type patternLineResponse struct {
	Number      int      `json:"number"`
	Text        string   `json:"text"`
	Description string   `json:"description"`
	Problems    []string `json:"problems"`
}

// defaultShapeRequest is the Shape section of the settings screen, stated
// whole. A Role the Shape no longer asks for is a Role missing from `seats` —
// there is no other way to say it, and no way to say it one Seat at a time.
type defaultShapeRequest struct {
	Seats []seatRequest `json:"seats"`
}

// seatRequest is one Seat an Organiser is asking for. Zero is not a value it can
// take: a Role asked for nought times is a Role the Shape does not name, and
// the server says so rather than quietly dropping it.
type seatRequest struct {
	RoleID string `json:"roleId"`
	Count  int    `json:"count"`
}

// handleGetRotaDefaults reports the settings record.
//
// Organiser-only, unlike the Roles listing beside it on the same screen. Roles are
// public because the rota is coloured by them and a logged-out visitor would
// otherwise read a colourless rota; nothing a visitor sees needs this. The
// times themselves are not a secret — GET /api/shifts has always carried them —
// but this is the settings screen's own resource, and the sections joining it
// are an Organiser's business.
func (h *Handler) handleGetRotaDefaults(w http.ResponseWriter, r *http.Request) {
	defaults, err := services.RotaDefaults(r.Context(), h.store)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	shape, err := services.DefaultShape(r.Context(), h.store)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, toRotaDefaultsResponse(defaults, shape))
}

// handleSaveShiftPattern writes the Shift pattern section — the pattern, its
// Start from, the time zone and the length — and answers with the settings as
// they now stand, including the Start from and zone filled in for an Organiser
// who left them blank.
//
// PUT rather than PATCH: the four are one section, stated whole. A pattern
// means something only against the date it counts from.
func (h *Handler) handleSaveShiftPattern(w http.ResponseWriter, r *http.Request) {
	var req shiftPatternRequest
	if err := decodeStrict(r, &req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	_, err := services.SaveShiftPattern(r.Context(), h.store, services.ShiftPatternParams{
		Pattern:       req.ShiftPattern,
		StartFrom:     req.PatternStartFrom,
		Timezone:      req.ShiftTimezone,
		LengthMinutes: req.ShiftLengthMinutes,
	}, h.logger)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	// Re-read, as the Shape's save does: this wrote one section, and the
	// answer is the whole record.
	h.writeCurrentSettings(w, r)
}

// handleShiftPatternPreview reads a pattern that is still being written: what
// each line means, or what is wrong with it, and where its Shifts start between
// two dates. POST because it carries a pattern the server has never seen; it
// writes nothing.
//
// It is how the screen describes a pattern and draws its calendar without an
// evaluator of its own (ADR 0013): the starts drawn are the starts a rota
// would be minted with.
func (h *Handler) handleShiftPatternPreview(w http.ResponseWriter, r *http.Request) {
	var req shiftPatternPreviewRequest
	if err := decodeStrict(r, &req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	preview, err := services.PreviewShiftPattern(services.ShiftPatternPreviewParams{
		Pattern:   req.ShiftPattern,
		StartFrom: req.PatternStartFrom,
		Timezone:  req.ShiftTimezone,
		From:      req.From,
		To:        req.To,
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	lines := make([]patternLineResponse, 0, len(preview.Lines))
	for _, line := range preview.Lines {
		lines = append(lines, patternLineResponse{
			Number:      line.Number,
			Text:        line.Text,
			Description: line.Description,
			Problems:    emptyIfNil(line.Problems),
		})
	}
	h.writeJSON(w, http.StatusOK, shiftPatternPreviewResponse{
		PatternStartFrom: preview.StartFrom,
		Lines:            lines,
		Starts:           preview.Starts,
	})
}

// handleSaveDefaultShape writes the Shape every Shift starts from and answers
// with the settings as they now stand.
//
// PUT and whole, like the shift times beside it and for a stronger reason: a
// Role dropped from a Shape is a Role the Shape no longer asks for, which no
// partial write could express.
func (h *Handler) handleSaveDefaultShape(w http.ResponseWriter, r *http.Request) {
	var req defaultShapeRequest
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

	if _, err := services.SaveDefaultShape(r.Context(), h.store, seats, h.logger); err != nil {
		h.writeServiceError(w, err)
		return
	}

	// The record is re-read rather than assembled from the save, because the
	// answer is the whole settings record and this endpoint only wrote a part
	// of it.
	defaults, err := services.RotaDefaults(r.Context(), h.store)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	h.writeSettings(w, r, defaults)
}

// writeSettings answers with the settings record whole, whichever section was
// just written. A section's save returns the record rather than the section, so
// a client holds one thing rather than stitching two answers together.
func (h *Handler) writeSettings(w http.ResponseWriter, r *http.Request, defaults model.RotaDefaults) {
	shape, err := services.DefaultShape(r.Context(), h.store)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, toRotaDefaultsResponse(defaults, shape))
}

func toRotaDefaultsResponse(defaults model.RotaDefaults, shape model.Shape) rotaDefaultsResponse {
	library := make([]ruleKindResponse, 0, len(model.RuleLibrary))
	for _, k := range model.RuleLibrary {
		library = append(library, ruleKindResponse{
			ID:           k.ID,
			Family:       string(k.Family),
			Label:        k.Label,
			Description:  k.Description,
			ValueLabel:   k.ValueLabel,
			MinValue:     k.MinValue,
			MaxValue:     k.MaxValue,
			DefaultValue: k.DefaultValue,
			Input:        string(k.Input),
			OptionLabel:  k.OptionLabel,
			Options:      k.Options,
		})
	}

	rules := make([]allocationRuleResponse, 0, len(defaults.AllocationSettings.Rules))
	for _, r := range defaults.AllocationSettings.Rules {
		rules = append(rules, allocationRuleResponse{
			ID:     r.ID,
			Kind:   r.Kind,
			Value:  r.Value,
			Option: r.Option,
			Scope: scopeBody{
				Include: emptyIfNil(r.Scope.Include),
				Exclude: emptyIfNil(r.Scope.Exclude),
			},
			Note:    r.Note,
			Ignored: r.Validate() != nil,
		})
	}

	return rotaDefaultsResponse{
		ShiftPattern:       defaults.ShiftPattern,
		PatternStartFrom:   defaults.PatternStartFrom,
		ShiftLengthMinutes: defaults.ShiftLengthMinutes,
		ShiftTimezone:      defaults.Timezone(),
		DefaultShape:       toSeatResponses(shape),
		AllocationRules:    rules,
		RuleLibrary:        library,
	}
}

// decodeStrict reads a request body, refusing a field the endpoint does not
// take so a client's typo is a 400 rather than an answer quietly dropped.
func decodeStrict(r *http.Request, into any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(into)
}

// handleAddAllocationRule adds a rule from the library and answers with the
// settings record as it now stands.
func (h *Handler) handleAddAllocationRule(w http.ResponseWriter, r *http.Request) {
	var req addAllocationRuleRequest
	if err := decodeStrict(r, &req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if _, err := services.AddAllocationRule(r.Context(), h.store, model.AllocationRule{
		Kind:   req.Kind,
		Value:  req.Value,
		Option: req.Option,
		Scope:  model.Scope{Include: req.Scope.Include, Exclude: req.Scope.Exclude},
		Note:   req.Note,
	}, h.logger); err != nil {
		h.writeServiceError(w, err)
		return
	}

	h.writeCurrentSettings(w, r)
}

// handleEditAllocationRule changes a rule's value, choice, Scope and note in place.
func (h *Handler) handleEditAllocationRule(w http.ResponseWriter, r *http.Request) {
	var req editAllocationRuleRequest
	if err := decodeStrict(r, &req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if _, err := services.EditAllocationRule(r.Context(), h.store, r.PathValue("id"), model.AllocationRule{
		Value:  req.Value,
		Option: req.Option,
		Scope:  model.Scope{Include: req.Scope.Include, Exclude: req.Scope.Exclude},
		Note:   req.Note,
	}, h.logger); err != nil {
		h.writeServiceError(w, err)
		return
	}

	h.writeCurrentSettings(w, r)
}

// handleRemoveAllocationRule takes a rule off, whatever its kind.
func (h *Handler) handleRemoveAllocationRule(w http.ResponseWriter, r *http.Request) {
	if err := services.RemoveAllocationRule(r.Context(), h.store, r.PathValue("id"), h.logger); err != nil {
		h.writeServiceError(w, err)
		return
	}

	h.writeCurrentSettings(w, r)
}

// writeCurrentSettings reads the settings record back and answers with it.
func (h *Handler) writeCurrentSettings(w http.ResponseWriter, r *http.Request) {
	defaults, err := services.RotaDefaults(r.Context(), h.store)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	h.writeSettings(w, r, defaults)
}

// emptyIfNil gives a list that is never null on the wire.
func emptyIfNil(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}
