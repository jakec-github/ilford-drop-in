package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/jakechorley/ilford-drop-in/internal/config"
)

const (
	AuthPort     = 3000
	authTimeout  = 5 * time.Minute
	callbackPath = "/oauth/callback"
)

var (
	tokenCache   *oauth2.Token
	tokenCacheMu sync.Mutex
)

// OAuth scopes for Google APIs
const (
	ScopeSheets                 = "https://www.googleapis.com/auth/spreadsheets"
	ScopeFormsBody              = "https://www.googleapis.com/auth/forms.body"
	ScopeFormsResponsesReadonly = "https://www.googleapis.com/auth/forms.responses.readonly"
	ScopeGmailSend              = "https://www.googleapis.com/auth/gmail.send"
)

// GetOAuthConfig creates an OAuth2 config from the OAuth client configuration
// Requests all necessary scopes for the application upfront (sheets, forms, gmail)
func GetOAuthConfig(oauthCfg *config.OAuthClientConfig) (*oauth2.Config, error) {
	oauthConfigJSON, err := json.Marshal(oauthCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal oauth config: %w", err)
	}

	scopes := []string{
		ScopeSheets,
		ScopeFormsBody,
		ScopeFormsResponsesReadonly,
		ScopeGmailSend,
	}

	googleConfig, err := google.ConfigFromJSON(oauthConfigJSON, scopes...)
	if err != nil {
		return nil, fmt.Errorf("failed to create google config: %w", err)
	}

	// Override redirect URI to use our local server
	googleConfig.RedirectURL = fmt.Sprintf("http://localhost:%d%s", AuthPort, callbackPath)

	return googleConfig, nil
}

// GetTokenWithFlow performs the full OAuth flow including user authorization
// This function is thread-safe and ensures only one OAuth flow runs at a time
func GetTokenWithFlow(ctx context.Context, oauthConfig *oauth2.Config) (*oauth2.Token, error) {
	tokenCacheMu.Lock()
	defer tokenCacheMu.Unlock()

	// Check if token is already cached and valid
	if tokenCache != nil && tokenCache.Valid() {
		return tokenCache, nil
	}

	// Generate auth URL
	authURL := oauthConfig.AuthCodeURL("state", oauth2.AccessTypeOffline)
	fmt.Printf("\nVisit this URL to authorize the application:\n%s\n\n", authURL)

	// Listen for OAuth callback
	code, err := listenForAuthCallback(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get authorization code: %w", err)
	}

	// Exchange code for token
	token, err := oauthConfig.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code for token: %w", err)
	}

	// Save token to memory cache
	tokenCache = token

	return token, nil
}

// listenForAuthCallback starts a local HTTP server and waits for the OAuth callback
func listenForAuthCallback(ctx context.Context) (string, error) {
	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)

	server := &http.Server{
		Addr: fmt.Sprintf(":%d", AuthPort),
	}

	http.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			errChan <- fmt.Errorf("no authorization code received")
			http.Error(w, "Authorization failed", http.StatusBadRequest)
			return
		}

		// Send success response to browser
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `
			<html>
				<head><title>Authorization Successful</title></head>
				<body>
					<h1>Authorization successful!</h1>
					<p>You can close this window and return to the application.</p>
				</body>
			</html>
		`)

		codeChan <- code
	})

	// Start server in background
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- fmt.Errorf("server error: %w", err)
		}
	}()

	// Wait for code or timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, authTimeout)
	defer cancel()

	var code string
	var authErr error

	select {
	case code = <-codeChan:
		// Success
	case authErr = <-errChan:
		// Error during auth
	case <-timeoutCtx.Done():
		authErr = fmt.Errorf("authorization timeout after %v", authTimeout)
	}

	// Shutdown server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	server.Shutdown(shutdownCtx)

	if authErr != nil {
		return "", authErr
	}

	return code, nil
}

// ClearToken clears the token from memory cache
func ClearToken() {
	tokenCacheMu.Lock()
	defer tokenCacheMu.Unlock()
	tokenCache = nil
}
