package formsclient

import (
	"fmt"
	"time"

	"google.golang.org/api/forms/v1"
)

// CreateAvailabilityForm creates a Google Form for a volunteer to indicate their availability
// Uses a 2-step form: first asks if available for all dates, then conditionally asks for unavailable dates
func (c *Client) CreateAvailabilityForm(
	volunteerName string,
	shiftDates []time.Time,
) (*AvailabilityFormResult, error) {
	// Format shift dates for display
	shiftDateStrings := make([]string, len(shiftDates))
	for i, date := range shiftDates {
		shiftDateStrings[i] = date.Format("Mon Jan 2 2006")
	}

	// Build the form title
	formTitle := fmt.Sprintf("Availability Request - %s", volunteerName)

	// Step 1: Create the form with just the title
	form := &forms.Form{
		Info: &forms.Info{
			Title:         formTitle,
			DocumentTitle: formTitle,
		},
	}

	createdForm, err := c.service.Forms.Create(form).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to create form: %w", err)
	}

	// Step 2: Add items using batchUpdate
	// Create checkbox options for each shift date
	checkboxOptions := make([]*forms.Option, len(shiftDateStrings))
	for i, dateStr := range shiftDateStrings {
		checkboxOptions[i] = &forms.Option{
			Value: dateStr,
		}
	}

	// Build batch update request to add items
	// Items are added to the end of the form by using Index with ForceSendFields
	batchUpdateRequest := &forms.BatchUpdateFormRequest{
		Requests: []*forms.Request{
			// Add Page 1: Are you available for all dates?
			{
				CreateItem: &forms.CreateItemRequest{
					Item: &forms.Item{
						Title: "Are you available for all dates?",
						QuestionItem: &forms.QuestionItem{
							Question: &forms.Question{
								Required: true,
								ChoiceQuestion: &forms.ChoiceQuestion{
									Type: "RADIO",
									Options: []*forms.Option{
										{Value: "Yes", GoToAction: "SUBMIT_FORM"},
										{Value: "No", GoToAction: "NEXT_SECTION"},
									},
								},
							},
						},
					},
					Location: &forms.Location{
						Index:           0,
						ForceSendFields: []string{"Index"},
					},
				},
			},
			// Add page break
			{
				CreateItem: &forms.CreateItemRequest{
					Item: &forms.Item{
						PageBreakItem: &forms.PageBreakItem{},
					},
					Location: &forms.Location{
						Index:           1,
						ForceSendFields: []string{"Index"},
					},
				},
			},
			// Add Page 2: Which dates are you unavailable?
			{
				CreateItem: &forms.CreateItemRequest{
					Item: &forms.Item{
						Title:       "Which shifts are you UNAVAILABLE for?",
						Description: "Select all dates you CANNOT volunteer",
						QuestionItem: &forms.QuestionItem{
							Question: &forms.Question{
								Required: false, // Not required - they might be available for all
								ChoiceQuestion: &forms.ChoiceQuestion{
									Type:    "CHECKBOX",
									Options: checkboxOptions,
								},
							},
						},
					},
					Location: &forms.Location{
						Index:           2,
						ForceSendFields: []string{"Index"},
					},
				},
			},
		},
	}

	_, err = c.service.Forms.BatchUpdate(createdForm.FormId, batchUpdateRequest).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to add items to form: %w", err)
	}

	return &AvailabilityFormResult{
		FormID:       createdForm.FormId,
		ResponderURI: createdForm.ResponderUri,
	}, nil
}
