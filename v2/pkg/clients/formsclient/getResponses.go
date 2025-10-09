package formsclient

import (
	"fmt"
	"time"
)

// FormResponse represents a parsed form response
type FormResponse struct {
	VolunteerID      string
	VolunteerName    string
	Email            string
	HasResponded     bool
	AvailableForAll  bool
	UnavailableDates []string
	AvailableDates   []string
}

// GetFormResponse fetches and parses a form response
// Returns the parsed response data, including which dates the volunteer is available for
func (c *Client) GetFormResponse(formID string, volunteerName string, shiftDates []time.Time) (*FormResponse, error) {
	responses, err := c.service.Forms.Responses.List(formID).PageSize(1).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to list form responses: %w", err)
	}

	// Check if there are any responses
	if len(responses.Responses) == 0 {
		return &FormResponse{
			VolunteerName: volunteerName,
			HasResponded:  false,
		}, nil
	}

	// Parse the first (and should be only) response
	response := responses.Responses[0]

	// Get respondent's email if available
	email := ""
	if response.RespondentEmail != "" {
		email = response.RespondentEmail
	}

	// Parse answers
	allAnswers := response.Answers
	if len(allAnswers) == 0 {
		return &FormResponse{
			VolunteerName: volunteerName,
			Email:         email,
			HasResponded:  true,
			AvailableForAll: false,
		}, nil
	}

	// Convert answers map to slice for easier processing
	answerList := make([]any, 0)
	for _, answer := range allAnswers {
		answerList = append(answerList, answer)
	}

	// Format shift dates for comparison
	shiftDateStrings := make([]string, len(shiftDates))
	for i, date := range shiftDates {
		shiftDateStrings[i] = date.Format("Mon Jan 2 2006")
	}

	// Check the first question: "Are you available for all dates?"
	availableForAll := false
	unavailableDates := []string{}

	// If there's only one answer, it means they answered "Yes" to the first question
	// (because answering "Yes" submits the form immediately)
	if len(answerList) == 1 {
		availableForAll = true
	} else {
		// They answered "No" to the first question and provided unavailable dates
		// The second answer contains the checkbox selections
		if len(answerList) >= 2 {
			// Get the second answer (unavailable dates)
			// We need to extract the text answers from the second question
			for _, answer := range allAnswers {
				if answer.TextAnswers != nil && answer.TextAnswers.Answers != nil {
					for _, textAnswer := range answer.TextAnswers.Answers {
						// Only add if it's not the Yes/No answer
						if textAnswer.Value != "Yes" && textAnswer.Value != "No" {
							unavailableDates = append(unavailableDates, textAnswer.Value)
						}
					}
				}
			}
		}
	}

	// Calculate available dates
	availableDates := make([]string, 0)
	if availableForAll {
		availableDates = shiftDateStrings
	} else {
		// Create a map for fast lookup
		unavailableMap := make(map[string]bool)
		for _, date := range unavailableDates {
			unavailableMap[date] = true
		}

		// Filter out unavailable dates
		for _, shiftDate := range shiftDateStrings {
			if !unavailableMap[shiftDate] {
				availableDates = append(availableDates, shiftDate)
			}
		}
	}

	return &FormResponse{
		VolunteerName:    volunteerName,
		Email:            email,
		HasResponded:     true,
		AvailableForAll:  availableForAll,
		UnavailableDates: unavailableDates,
		AvailableDates:   availableDates,
	}, nil
}
