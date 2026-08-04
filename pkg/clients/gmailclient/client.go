package gmailclient

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// Client wraps the Gmail API client
type Client struct {
	service      *gmail.Service
	ctx          context.Context
	lastSendTime time.Time
	sendMutex    sync.Mutex
}

// NewClientFromToken creates a Gmail client from an access token alone, with no
// OAuth client configuration behind it.
//
// This is now the only way to build one, and it is the web server's path. The
// admin has just granted gmail.send through incremental consent, so the token is
// already minted, short-lived, and carries no refresh token — a static token
// source is the honest shape for it. There is nothing to refresh with, and a
// client that quietly renewed itself would be a standing Google credential,
// which is precisely what asking for the scope at send time avoids.
//
// The CLI used to build one from its stored, all-scopes token. That went with
// the Google Forms availability flow (issue #80), and took gmail.send out of the
// CLI's grant with it.
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
