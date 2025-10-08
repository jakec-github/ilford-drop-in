package formsclient

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/api/forms/v1"
	"google.golang.org/api/option"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/utils"
)

// Client wraps the Google Forms API client
type Client struct {
	service *forms.Service
	ctx     context.Context
}

// AvailabilityFormResult contains the created form details
type AvailabilityFormResult struct {
	FormID       string
	ResponderURI string // The URL volunteers use to fill out the form
}

// NewClient creates a new Forms client using an existing OAuth token
// The token should already contain all necessary scopes (forms, sheets, gmail)
func NewClient(ctx context.Context, oauthCfg *config.OAuthClientConfig, token *oauth2.Token) (*Client, error) {
	// Get OAuth config with all required scopes for the application
	oauthConfig, err := utils.GetOAuthConfig(oauthCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get oauth config: %w", err)
	}

	httpClient := oauthConfig.Client(ctx, token)

	service, err := forms.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("failed to create forms service: %w", err)
	}

	return &Client{
		service: service,
		ctx:     ctx,
	}, nil
}

// CreateAvailabilityForm creates a Google Form for a volunteer to indicate their availability
// Uses a 2-step form: first asks if available for all dates, then conditionally asks for unavailable dates
func (c *Client) CreateAvailabilityForm(
	volunteerName string,
	rotaID string,
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
