package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/patterntranslator"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
	"github.com/jakechorley/ilford-drop-in/pkg/core/shiftpattern"
)

// translateTimeout is the ceiling on one Generate, retries and all (ADR 0015).
const translateTimeout = 30 * time.Second

// Why the natural editor is unavailable, as the switch says on hover.
const (
	noTranslatorReason = "Plain-English patterns are not set up on this server"
	notOptedInReason   = "Plain-English patterns are not turned on for this organisation"
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

type translateShiftPatternRequest struct {
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

// handleTranslateShiftPattern turns a description into a Shift pattern. It
// never saves: the card saves what comes back through its own save, as if the
// Organiser had typed the pattern.
func (h *Handler) handleTranslateShiftPattern(w http.ResponseWriter, r *http.Request) {
	if editor := h.naturalEditor(); !editor.Available {
		h.writeError(w, http.StatusForbidden, editor.Reason)
		return
	}

	var req translateShiftPatternRequest
	if err := decodeStrict(r, &req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if utf8.RuneCountInString(req.Text) > patterntranslator.MaxText {
		h.writeError(w, http.StatusBadRequest, tooLong)
		return
	}

	defaults, err := services.RotaDefaults(r.Context(), h.store)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	zone := defaults.Timezone()

	ctx, cancel := context.WithTimeout(r.Context(), translateTimeout)
	defer cancel()
	result, err := h.translator.Translate(ctx, patterntranslator.Request{
		Text: req.Text,
		Zone: zone,
		Now:  time.Now(),
	})
	switch {
	case errors.Is(err, patterntranslator.ErrTooLong):
		h.writeError(w, http.StatusBadRequest, tooLong)
		return
	case err != nil:
		h.logger.Warn("A shift pattern could not be translated", zap.Error(err))
		h.writeError(w, http.StatusServiceUnavailable, couldNotTranslate)
		return
	}

	if result.Pattern == "" {
		issues := make([]translateIssueResponse, 0, len(result.Issues))
		for _, issue := range result.Issues {
			issues = append(issues, translateIssueResponse{
				Kind:     string(issue.Kind),
				Message:  issue.Message,
				Examples: emptyIfNil(issue.Examples),
			})
		}
		h.writeJSON(w, http.StatusOK, translateShiftPatternResponse{Status: "issues", Issues: issues})
		return
	}

	reading, _ := shiftpattern.Read(result.Pattern, zone)
	h.writeJSON(w, http.StatusOK, translateShiftPatternResponse{
		Status:  "ok",
		Pattern: result.Pattern,
		Entries: toPatternEntryResponses(reading.Entries),
	})
}
