package gmailclient

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/utils"
)

// Client wraps the Gmail API client
type Client struct {
	service      *gmail.Service
	ctx          context.Context
	lastSendTime time.Time
	sendMutex    sync.Mutex
}

// NewClient creates a new Gmail client using an existing OAuth token
// The token should already contain all necessary scopes (forms, sheets, gmail)
func NewClient(ctx context.Context, oauthCfg *config.OAuthClientConfig, token *oauth2.Token) (*Client, error) {
	// Get OAuth config with all required scopes for the application
	oauthConfig, err := utils.GetOAuthConfig(oauthCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get oauth config: %w", err)
	}

	httpClient := oauthConfig.Client(ctx, token)

	service, err := gmail.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("failed to create gmail service: %w", err)
	}

	return &Client{
		service: service,
		ctx:     ctx,
	}, nil
}
