package formsclient

import (
	"context"
	"fmt"

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
