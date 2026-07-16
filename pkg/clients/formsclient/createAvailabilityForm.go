package formsclient

import (
	"fmt"
	"strings"
	"time"

	"google.golang.org/api/forms/v1"
)

// CreateAvailabilityForm creates a Google Form for a volunteer to indicate their availability.
// Uses a 2-step form: first asks if available for all dates, then conditionally asks for
// unavailable dates. shiftDates are the open dates the volunteer is asked about; closedDates
// are dates the drop-in is not running and are shown, read-only, so volunteers are aware they
// have been excluded rather than forgotten. Callers must pass at least one open shift date.
func (c *Client) CreateAvailabilityForm(
	volunteerName string,
	shiftDates []time.Time,
	closedDates []time.Time,
) (*AvailabilityFormResult, error) {
	// Format shift dates for display
	shiftDateStrings := make([]string, len(shiftDates))
	for i, date := range shiftDates {
		shiftDateStrings[i] = date.Format("Mon Jan 2 2006")
	}

	// Build the form title
	formTitle := fmt.Sprintf("Availability - %s - %s to %s", volunteerName, shiftDateStrings[0], shiftDateStrings[len(shiftDateStrings)-1])

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

	// Build batch update request to add items.
	// Items are added to the end of the form by using Index with ForceSendFields.
	// Indices are assigned sequentially so an optional closed-dates notice can be
	// prepended without a form with no closed dates differing from before.
	requests := make([]*forms.Request, 0, 4)
	nextIndex := int64(0)
	addItem := func(item *forms.Item) {
		requests = append(requests, &forms.Request{
			CreateItem: &forms.CreateItemRequest{
				Item: item,
				Location: &forms.Location{
					Index:           nextIndex,
					ForceSendFields: []string{"Index"},
				},
			},
		})
		nextIndex++
	}

	// Read-only notice listing the closed dates, so volunteers can see the
	// drop-in is not running on them rather than assuming they were forgotten.
	if len(closedDates) > 0 {
		closedDateStrings := make([]string, len(closedDates))
		for i, date := range closedDates {
			closedDateStrings[i] = date.Format("Mon Jan 2 2006")
		}
		addItem(&forms.Item{
			Title:       "The drop-in is CLOSED on these dates",
			Description: "You are not being asked about these dates as the drop-in is not running:\n" + strings.Join(closedDateStrings, "\n"),
			TextItem:    &forms.TextItem{},
		})
	}

	// Page 1: Are you available for all dates?
	addItem(&forms.Item{
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
	})

	// Page break
	addItem(&forms.Item{
		PageBreakItem: &forms.PageBreakItem{},
	})

	// Page 2: Which dates are you unavailable?
	addItem(&forms.Item{
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
	})

	batchUpdateRequest := &forms.BatchUpdateFormRequest{
		Requests: requests,
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
