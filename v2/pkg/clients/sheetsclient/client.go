package sheetsclient

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
	token   *oauth2.Token
	ctx     context.Context
}

// NewClient creates a new Sheets client using OAuth credentials and performs OAuth flow if needed
// Requests all necessary scopes upfront (sheets, forms, gmail) so the token can be shared across clients
// Tokens are persisted to disk for the given environment
func NewClient(ctx context.Context, oauthCfg *config.OAuthClientConfig, env string) (*Client, error) {
	// Get OAuth config with all required scopes for the application
	oauthConfig, err := utils.GetOAuthConfig(oauthCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get oauth config: %w", err)
	}

	// Get token (will perform OAuth flow if needed, tokens are persisted to disk)
	token, err := utils.GetTokenWithFlow(ctx, oauthConfig, env)
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
		token:   token,
		ctx:     ctx,
	}, nil
}

// Service returns the underlying sheets service for direct API access
func (c *Client) Service() *sheets.Service {
	return c.service
}

// Token returns the OAuth token used by this client
func (c *Client) Token() *oauth2.Token {
	return c.token
}

// GetValues reads values from a spreadsheet range
func (c *Client) GetValues(spreadsheetID, sheetRange string) ([][]interface{}, error) {
	resp, err := c.service.Spreadsheets.Values.Get(spreadsheetID, sheetRange).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to get values: %w", err)
	}

	return resp.Values, nil
}

// AppendRows appends rows to the end of a sheet
func (c *Client) AppendRows(spreadsheetID, sheetRange string, values [][]interface{}) error {
	valueRange := &sheets.ValueRange{
		Values: values,
	}

	_, err := c.service.Spreadsheets.Values.Append(spreadsheetID, sheetRange, valueRange).
		ValueInputOption("RAW").
		Do()
	if err != nil {
		return fmt.Errorf("failed to append rows: %w", err)
	}

	return nil
}

// CreateSheet creates a new sheet/tab in the spreadsheet
func (c *Client) CreateSheet(spreadsheetID, sheetTitle string) (int64, error) {
	req := &sheets.Request{
		AddSheet: &sheets.AddSheetRequest{
			Properties: &sheets.SheetProperties{
				Title: sheetTitle,
			},
		},
	}

	batchUpdateRequest := &sheets.BatchUpdateSpreadsheetRequest{
		Requests: []*sheets.Request{req},
	}

	resp, err := c.service.Spreadsheets.BatchUpdate(spreadsheetID, batchUpdateRequest).Do()
	if err != nil {
		return 0, fmt.Errorf("failed to create sheet: %w", err)
	}

	if len(resp.Replies) == 0 || resp.Replies[0].AddSheet == nil {
		return 0, fmt.Errorf("unexpected response from create sheet")
	}

	sheetID := resp.Replies[0].AddSheet.Properties.SheetId
	return sheetID, nil
}
