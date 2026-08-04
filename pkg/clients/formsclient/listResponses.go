package formsclient

import (
	"fmt"
	"time"

	"google.golang.org/api/forms/v1"
)

// SubmittedFormResponse is a parsed response together with the moment Forms
// records it was last submitted.
//
// Every other read of this client collapses a form to a single answer, because
// that is all a rota ever needed. The backfill (issue #80) needs the timestamps
// too: it writes each response as its own generation stamped at its original
// time, so latest-wins against a rota's allocated_datetime reproduces the answer
// that was in when the rota went out.
type SubmittedFormResponse struct {
	SubmittedAt time.Time
	Response    *FormResponse
}

// ListFormResponses fetches and parses every response a form holds, oldest
// first, discarding any response Forms cannot date — an undated response cannot
// be placed against a rota's cut-off, so guessing a timestamp for it would
// invent history rather than record it.
func (c *Client) ListFormResponses(formID string, volunteerName string, shiftDates []time.Time) ([]SubmittedFormResponse, error) {
	var raw []*forms.FormResponse
	call := c.service.Forms.Responses.List(formID)
	for {
		page, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("failed to list responses for form %s: %w", formID, err)
		}
		raw = append(raw, page.Responses...)
		if page.NextPageToken == "" {
			break
		}
		call = call.PageToken(page.NextPageToken)
	}

	responses := make([]SubmittedFormResponse, 0, len(raw))
	for _, response := range raw {
		submittedAt, ok := responseTime(response)
		if !ok {
			continue
		}
		responses = append(responses, SubmittedFormResponse{
			SubmittedAt: submittedAt,
			Response:    parseFormResponse(response, volunteerName, shiftDates),
		})
	}

	return responses, nil
}

// responseTime reads when a response was last submitted, falling back to when it
// was created. Both are RFC3339 strings on the API type.
func responseTime(response *forms.FormResponse) (time.Time, bool) {
	for _, stamp := range []string{response.LastSubmittedTime, response.CreateTime} {
		if stamp == "" {
			continue
		}
		if parsed, err := time.Parse(time.RFC3339, stamp); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}
