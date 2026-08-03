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

// NewClientFromToken creates a Gmail client from an access token alone, with no
// OAuth client configuration behind it.
//
// This is the web server's path. The admin has just granted gmail.send through
// incremental consent, so the token is already minted, short-lived, and carries
// no refresh token — a static token source is the honest shape for it. There is
// nothing to refresh with, and a client that quietly renewed itself would be a
// standing Google credential, which is precisely what asking for the scope at
// send time avoids.
func NewClientFromToken(ctx context.Context, token *oauth2.Token) (*Client, error) {
	service, err := gmail.NewService(ctx, option.WithTokenSource(oauth2.StaticTokenSource(token)))
	if err != nil {
		return nil, fmt.Errorf("failed to create gmail service: %w", err)
	}

	return &Client{
		service: service,
		ctx:     ctx,
	}, nil
}
