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

	// Create checkbox options for each shift date
	checkboxOptions := make([]*forms.Option, len(shiftDateStrings))
	for i, dateStr := range shiftDateStrings {
		checkboxOptions[i] = &forms.Option{
			Value: dateStr,
		}
	}

	// Build the form structure with two pages
	form := &forms.Form{
		Info: &forms.Info{
			Title:       formTitle,
			DocumentTitle: formTitle,
		},
		Items: []*forms.Item{
			// Page 1: Are you available for all dates?
			{
				Title:       "Are you available for all dates?",
				Description: "",
				ItemId:      "available_all_question",
				QuestionItem: &forms.QuestionItem{
					Question: &forms.Question{
						Required: true,
						ChoiceQuestion: &forms.ChoiceQuestion{
							Type: "RADIO",
							Options: []*forms.Option{
								{Value: "Yes"},
								{Value: "No"},
							},
						},
					},
				},
			},
			// Page break
			{
				ItemId: "page_break_1",
				PageBreakItem: &forms.PageBreakItem{},
			},
			// Page 2: Which dates are you unavailable? (conditional)
			{
				Title:       "Which shifts are you UNAVAILABLE for?",
				Description: "Select all dates you CANNOT volunteer",
				ItemId:      "unavailable_dates_question",
				QuestionItem: &forms.QuestionItem{
					Question: &forms.Question{
						Required: true,
						ChoiceQuestion: &forms.ChoiceQuestion{
							Type:    "CHECKBOX",
							Options: checkboxOptions,
						},
					},
				},
			},
		},
	}

	// Create the form
	createdForm, err := c.service.Forms.Create(form).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to create form: %w", err)
	}

	return &AvailabilityFormResult{
		FormID:       createdForm.FormId,
		ResponderURI: createdForm.ResponderUri,
	}, nil
}
