package sheetsClient

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/utils"
)

// Client wraps the Google Sheets API client
type Client struct {
	service *sheets.Service
	ctx     context.Context
}

// NewClient creates a new Sheets client using OAuth credentials and performs OAuth flow if needed
func NewClient(ctx context.Context, oauthCfg *config.OAuthClientConfig) (*Client, error) {
	// Get OAuth config with sheets scope
	oauthConfig, err := utils.GetOAuthConfig(oauthCfg, []string{utils.ScopeSheets})
	if err != nil {
		return nil, fmt.Errorf("failed to get oauth config: %w", err)
	}

	// Get token (will perform OAuth flow if needed)
	token, err := utils.GetTokenWithFlow(ctx, oauthConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to get oauth token: %w", err)
	}

	// Create HTTP client with token
	httpClient := oauthConfig.Client(ctx, token)

	// Create sheets service
	service, err := sheets.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("failed to create sheets service: %w", err)
	}

	return &Client{
		service: service,
		ctx:     ctx,
	}, nil
}

// NewClientWithToken creates a new Sheets client using an existing token
func NewClientWithToken(ctx context.Context, oauthCfg *config.OAuthClientConfig, token *oauth2.Token) (*Client, error) {
	oauthConfig, err := utils.GetOAuthConfig(oauthCfg, []string{utils.ScopeSheets})
	if err != nil {
		return nil, fmt.Errorf("failed to get oauth config: %w", err)
	}

	httpClient := oauthConfig.Client(ctx, token)

	service, err := sheets.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("failed to create sheets service: %w", err)
	}

	return &Client{
		service: service,
		ctx:     ctx,
	}, nil
}

// Service returns the underlying sheets service for direct API access
func (c *Client) Service() *sheets.Service {
	return c.service
}

// GetValues reads values from a spreadsheet range
func (c *Client) GetValues(spreadsheetID, sheetRange string) ([][]interface{}, error) {
	resp, err := c.service.Spreadsheets.Values.Get(spreadsheetID, sheetRange).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to get values: %w", err)
	}

	return resp.Values, nil
}

// TODO: Add more methods for common operations:
// - UpdateValues(spreadsheetID, range, values) to write data
// - BatchGet(spreadsheetID, ranges) to read multiple ranges
// - BatchUpdate(spreadsheetID, requests) to perform multiple updates

