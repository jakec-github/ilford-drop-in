package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/patterntranslator"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
	"github.com/jakechorley/ilford-drop-in/pkg/core/shiftpattern"
)

// Why the natural editor is unavailable, as the switch says on hover.
const (
	noTranslatorReason = "Plain-English patterns are not set up on this server"
	notOptedInReason   = "Plain-English patterns are not turned on for this organisation"
	// noPatternReason is why an Override's matches cannot be described yet:
	// a matcher picks out the pattern's Shifts, and there are none to pick.
	noPatternReason = "Set the shift pattern first"
)

// couldNotTranslate is what an Organiser reads when the model could not be
// asked — down, slow, or over the workspace's spend limit.
const couldNotTranslate = "Couldn't translate just now"

var tooLong = fmt.Sprintf("Keep the description to %d characters", patterntranslator.MaxText)

// SetTranslator gives the handler a translator, which makes the natural editor
// available to an Organisation that has opted in.
func (h *Handler) SetTranslator(t patterntranslator.Translator) {
	h.translator = t
}

type naturalEditorResponse struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// naturalEditor needs both a translator on the server and the Organisation's
// opt-in: the key is the server's, the choice to send descriptions to a model
// the Organisation's.
func (h *Handler) naturalEditor() naturalEditorResponse {
	switch {
	case h.translator == nil:
		return naturalEditorResponse{Reason: noTranslatorReason}
	case !h.org.NaturalEditor:
		return naturalEditorResponse{Reason: notOptedInReason}
	}
	return naturalEditorResponse{Available: true}
}

// overrideNaturalEditor is the natural editor for what an Override matches,
// which also needs a saved Shift pattern: the model is told its entries, so it
// can tell a day with one Shift from a day with several (ADR 0015).
func (h *Handler) overrideNaturalEditor(defaults model.RotaDefaults) naturalEditorResponse {
	if editor := h.naturalEditor(); !editor.Available {
		return editor
	}
	if defaults.ShiftPattern == "" {
		return naturalEditorResponse{Reason: noPatternReason}
	}
	return naturalEditorResponse{Available: true}
}

// translateRequest is a description to translate, of either kind.
type translateRequest struct {
	Text string `json:"text"`
}

// translateShiftPatternResponse is either a pattern, with what each of its
// entries says, or every issue that stopped one being written.
type translateShiftPatternResponse struct {
	Status  string                   `json:"status"`
	Pattern string                   `json:"pattern,omitempty"`
	Entries []patternEntryResponse   `json:"entries,omitempty"`
	Issues  []translateIssueResponse `json:"issues,omitempty"`
}

type translateIssueResponse struct {
	Kind     string   `json:"kind"`
	Message  string   `json:"message"`
	Examples []string `json:"examples"`
}

// translateOverrideRuleResponse is either an Override's rule, with what it
// says and whether it matches a time or whole dates, or every issue that
// stopped one being written.
type translateOverrideRuleResponse struct {
	Status      string                   `json:"status"`
	RRule       string                   `json:"rrule,omitempty"`
	Granularity string                   `json:"granularity,omitempty"`
	Description string                   `json:"description,omitempty"`
	Issues      []translateIssueResponse `json:"issues,omitempty"`
}

// handleTranslateShiftPattern turns a description into a Shift pattern. It
// never saves: the card saves what comes back through its own save, as if the
// Organiser had typed the pattern.
func (h *Handler) handleTranslateShiftPattern(w http.ResponseWriter, r *http.Request) {
	defaults, ok := h.loadDefaults(w, r)
	if !ok {
		return
	}
	text, ok := h.textToTranslate(w, r, h.naturalEditor())
	if !ok {
		return
	}
	zone := defaults.Timezone()

	ctx, cancel := context.WithTimeout(r.Context(), patterntranslator.Timeout)
	defer cancel()
	result, err := h.translator.Translate(ctx, patterntranslator.Request{
		Text: text,
		Zone: zone,
		Now:  time.Now(),
	})
	if err != nil {
		h.writeTranslateError(w, err, "A shift pattern could not be translated")
		return
	}

	if result.Pattern == "" {
		h.writeJSON(w, http.StatusOK, translateShiftPatternResponse{Status: "issues", Issues: toIssueResponses(result.Issues)})
		return
	}
	reading, _ := shiftpattern.Read(result.Pattern, zone)
	h.writeJSON(w, http.StatusOK, translateShiftPatternResponse{
		Status:  "ok",
		Pattern: result.Pattern,
		Entries: toPatternEntryResponses(reading.Entries),
	})
}

// handleTranslateOverrideRule turns a description of which Shifts an Override
// matches into its rule, read against the saved pattern. It never saves: the
// card saves the rule through the Override's own save, as if it were typed,
// and a rule that matches nothing is saved like a typed one.
func (h *Handler) handleTranslateOverrideRule(w http.ResponseWriter, r *http.Request) {
	defaults, ok := h.loadDefaults(w, r)
	if !ok {
		return
	}
	text, ok := h.textToTranslate(w, r, h.overrideNaturalEditor(defaults))
	if !ok {
		return
	}
	zone := defaults.Timezone()

	reading, _ := shiftpattern.Read(defaults.ShiftPattern, zone)
	pattern := make([]string, 0, len(reading.Entries))
	for _, e := range reading.Entries {
		if e.Description != "" {
			pattern = append(pattern, e.Description)
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), patterntranslator.Timeout)
	defer cancel()
	result, err := h.translator.TranslateRule(ctx, patterntranslator.RuleRequest{
		Text:    text,
		Pattern: pattern,
		Zone:    zone,
		Now:     time.Now(),
	})
	if err != nil {
		h.writeTranslateError(w, err, "An override's rule could not be translated")
		return
	}

	if result.Rule == "" {
		h.writeJSON(w, http.StatusOK, translateOverrideRuleResponse{Status: "issues", Issues: toIssueResponses(result.Issues)})
		return
	}
	rule := services.ReadOverrideRule(result.Rule)
	h.writeJSON(w, http.StatusOK, translateOverrideRuleResponse{
		Status:      "ok",
		RRule:       result.Rule,
		Granularity: rule.Granularity,
		Description: rule.Description,
	})
}

func (h *Handler) loadDefaults(w http.ResponseWriter, r *http.Request) (model.RotaDefaults, bool) {
	defaults, err := services.RotaDefaults(r.Context(), h.store)
	if err != nil {
		h.writeServiceError(w, err)
		return model.RotaDefaults{}, false
	}
	return defaults, true
}

// textToTranslate is the description in the request, or false once the
// answer has been written: the editor is not available, the body is not one,
// or the description is too long to send.
func (h *Handler) textToTranslate(w http.ResponseWriter, r *http.Request, editor naturalEditorResponse) (string, bool) {
	if !editor.Available {
		h.writeError(w, http.StatusForbidden, editor.Reason)
		return "", false
	}
	var req translateRequest
	if err := decodeStrict(r, &req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return "", false
	}
	if utf8.RuneCountInString(req.Text) > patterntranslator.MaxText {
		h.writeError(w, http.StatusBadRequest, tooLong)
		return "", false
	}
	return req.Text, true
}

// writeTranslateError answers a translation the translator could not make.
func (h *Handler) writeTranslateError(w http.ResponseWriter, err error, what string) {
	if errors.Is(err, patterntranslator.ErrTooLong) {
		h.writeError(w, http.StatusBadRequest, tooLong)
		return
	}
	h.logger.Warn(what, zap.Error(err))
	h.writeError(w, http.StatusServiceUnavailable, couldNotTranslate)
}

func toIssueResponses(issues []patterntranslator.Issue) []translateIssueResponse {
	out := make([]translateIssueResponse, 0, len(issues))
	for _, issue := range issues {
		out = append(out, translateIssueResponse{
			Kind:     string(issue.Kind),
			Message:  issue.Message,
			Examples: emptyIfNil(issue.Examples),
		})
	}
	return out
}
