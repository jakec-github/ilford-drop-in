package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/jakechorley/ilford-drop-in/internal/config"
)

const (
	AuthPort       = 3000
	authTimeout    = 5 * time.Minute
	callbackPath   = "/oauth/callback"
	tokenDirName   = ".ilford-drop-in/tokens"
	tokenFilePerms = 0600 // Read/write for owner only
	tokenDirPerms  = 0700 // Read/write/execute for owner only
	tokenInfoURL   = "https://oauth2.googleapis.com/tokeninfo"
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

// requiredScopes returns all scopes required by the application
func requiredScopes() []string {
	return []string{
		ScopeSheets,
		ScopeFormsBody,
		ScopeFormsResponsesReadonly,
		ScopeGmailSend,
	}
}

// GetOAuthConfig creates an OAuth2 config from the OAuth client configuration
// Requests all necessary scopes for the application upfront (sheets, forms, gmail)
func GetOAuthConfig(oauthCfg *config.OAuthClientConfig) (*oauth2.Config, error) {
	oauthConfigJSON, err := json.Marshal(oauthCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal oauth config: %w", err)
	}

	scopes := requiredScopes()

	googleConfig, err := google.ConfigFromJSON(oauthConfigJSON, scopes...)
	if err != nil {
		return nil, fmt.Errorf("failed to create google config: %w", err)
	}

	// Override redirect URI to use our local server
	googleConfig.RedirectURL = fmt.Sprintf("http://localhost:%d%s", AuthPort, callbackPath)

	return googleConfig, nil
}

// validateTokenScopes checks that the token has all required scopes by calling Google's tokeninfo endpoint
// Returns an error listing any missing scopes if validation fails
func validateTokenScopes(ctx context.Context, token *oauth2.Token) error {
	req, err := http.NewRequestWithContext(ctx, "GET", tokenInfoURL+"?access_token="+token.AccessToken, nil)
	if err != nil {
		return fmt.Errorf("failed to create tokeninfo request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call tokeninfo endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("tokeninfo request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenInfo struct {
		Scope string `json:"scope"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenInfo); err != nil {
		return fmt.Errorf("failed to decode tokeninfo response: %w", err)
	}

	grantedScopes := strings.Split(tokenInfo.Scope, " ")
	var missingScopes []string
	for _, required := range requiredScopes() {
		if !slices.Contains(grantedScopes, required) {
			missingScopes = append(missingScopes, required)
		}
	}

	if len(missingScopes) > 0 {
		return fmt.Errorf("token is missing required scopes: %v\nPlease ensure all permissions are granted during the OAuth flow", missingScopes)
	}

	return nil
}

// GetTokenWithFlow performs the full OAuth flow including user authorization
// This function is thread-safe and ensures only one OAuth flow runs at a time
// Tokens are persisted to disk for the given environment and automatically refreshed when expired
func GetTokenWithFlow(ctx context.Context, oauthConfig *oauth2.Config, env string) (*oauth2.Token, error) {
	tokenCacheMu.Lock()
	defer tokenCacheMu.Unlock()

	// Check if token is already cached in memory and valid
	if tokenCache != nil && tokenCache.Valid() {
		return tokenCache, nil
	}

	// Try to load token from file
	fileToken, err := LoadTokenFromFile(env)
	if err != nil {
		fmt.Printf("Warning: failed to load token from file: %v\n", err)
	}

	if fileToken != nil {
		// Check if token is valid or can be refreshed
		if fileToken.Valid() {
			// Validate that the cached token has all required scopes
			if err := validateTokenScopes(ctx, fileToken); err != nil {
				fmt.Printf("Cached token is missing required scopes: %v\n", err)
				fmt.Println("Deleting invalid token and starting new OAuth flow...")
				DeleteTokenFile(env)
			} else {
				// Token is still valid with all required scopes, cache and return it
				tokenCache = fileToken
				return fileToken, nil
			}
		} else if fileToken.RefreshToken != "" {
			// Token expired but might have refresh token
			tokenSource := oauthConfig.TokenSource(ctx, fileToken)
			refreshedToken, err := tokenSource.Token()
			if err == nil && refreshedToken.AccessToken != fileToken.AccessToken {
				// Token was successfully refreshed, now validate scopes
				if err := validateTokenScopes(ctx, refreshedToken); err != nil {
					fmt.Printf("Refreshed token is missing required scopes: %v\n", err)
					fmt.Println("Deleting invalid token and starting new OAuth flow...")
					DeleteTokenFile(env)
				} else {
					fmt.Println("Token refreshed successfully")

					// Save refreshed token to file
					if err := SaveTokenToFile(env, refreshedToken); err != nil {
						fmt.Printf("Warning: failed to save refreshed token: %v\n", err)
						// Continue anyway - token is still valid in memory
					}

					// Cache and return refreshed token
					tokenCache = refreshedToken
					return refreshedToken, nil
				}
			}
		}
	}

	// No valid cached token - perform OAuth flow
	fmt.Println("No valid token found - starting OAuth flow")

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

	// Validate that the token has all required scopes before saving
	if err := validateTokenScopes(ctx, token); err != nil {
		return nil, fmt.Errorf("token validation failed: %w", err)
	}

	// Save token to file
	if err := SaveTokenToFile(env, token); err != nil {
		fmt.Printf("Warning: failed to save token to file: %v\n", err)
		// Continue anyway - token is still valid in memory
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

// getTokenFilePath returns the path to the token file for the given environment
func getTokenFilePath(env string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	tokenDir := filepath.Join(homeDir, tokenDirName)
	return filepath.Join(tokenDir, fmt.Sprintf("token-%s.json", env)), nil
}

// ensureTokenDir creates the token directory if it doesn't exist
func ensureTokenDir() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	tokenDir := filepath.Join(homeDir, tokenDirName)
	if err := os.MkdirAll(tokenDir, tokenDirPerms); err != nil {
		return fmt.Errorf("failed to create token directory: %w", err)
	}

	return nil
}

// LoadTokenFromFile loads an OAuth token from the file system for the given environment
// Returns nil if the file doesn't exist (not an error - just means no cached token)
func LoadTokenFromFile(env string) (*oauth2.Token, error) {
	tokenPath, err := getTokenFilePath(env)
	if err != nil {
		return nil, err
	}

	// Check if file exists
	if _, err := os.Stat(tokenPath); os.IsNotExist(err) {
		return nil, nil // No token file exists yet
	}

	// Read token file
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read token file: %w", err)
	}

	// Parse token
	var token oauth2.Token
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("failed to parse token file: %w", err)
	}

	return &token, nil
}

// SaveTokenToFile saves an OAuth token to the file system for the given environment
func SaveTokenToFile(env string, token *oauth2.Token) error {
	// Ensure token directory exists
	if err := ensureTokenDir(); err != nil {
		return err
	}

	tokenPath, err := getTokenFilePath(env)
	if err != nil {
		return err
	}

	// Marshal token to JSON
	data, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	// Write token file with secure permissions
	if err := os.WriteFile(tokenPath, data, tokenFilePerms); err != nil {
		return fmt.Errorf("failed to write token file: %w", err)
	}

	return nil
}

// DeleteTokenFile deletes the token file for the given environment
func DeleteTokenFile(env string) error {
	tokenPath, err := getTokenFilePath(env)
	if err != nil {
		return err
	}

	if err := os.Remove(tokenPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete token file: %w", err)
	}

	return nil
}
