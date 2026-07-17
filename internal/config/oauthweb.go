package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// OAuthClientWebConfig represents a Google "Web application" OAuth client.
// Web clients nest their fields under a "web" key rather than the "installed"
// key used by the installed-app client (see oauth.go), so this is a small
// parallel of OAuthClientConfig rather than a reuse.
type OAuthClientWebConfig struct {
	Web OAuthWeb `json:"web" validate:"required"`
}

// OAuthWeb represents the web section of a web OAuth client config.
type OAuthWeb struct {
	ClientID                string   `json:"client_id" validate:"required"`
	ProjectID               string   `json:"project_id" validate:"required"`
	AuthURI                 string   `json:"auth_uri" validate:"required,url"`
	TokenURI                string   `json:"token_uri" validate:"required,url"`
	AuthProviderX509CertURL string   `json:"auth_provider_x509_cert_url" validate:"required,url"`
	ClientSecret            string   `json:"client_secret" validate:"required"`
	RedirectURIs            []string `json:"redirect_uris" validate:"required,min=1,dive,uri"`
}

// LoadOAuthClientWebWithEnv loads and validates the web OAuth client configuration
// with an environment suffix. For example, env="test" looks for "oauthClientWeb.test.json".
func LoadOAuthClientWebWithEnv(env string) (*OAuthClientWebConfig, error) {
	path, err := findOAuthWebFile(env)
	if err != nil {
		return nil, fmt.Errorf("failed to find web oauth client file: %w", err)
	}

	return LoadOAuthClientWebFromPath(path)
}

// LoadOAuthClientWebFromPath loads and validates the web OAuth client configuration
// from a specific path.
func LoadOAuthClientWebFromPath(path string) (*OAuthClientWebConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read web oauth client file: %w", err)
	}

	var cfg OAuthClientWebConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse web oauth client file: %w", err)
	}

	if err := validate.Struct(&cfg); err != nil {
		return nil, fmt.Errorf("web oauth client validation failed: %w", err)
	}

	return &cfg, nil
}

// findOAuthWebFile searches for the web OAuth client file in the current directory
// and the home directory. If env is provided it is added as an extension
// (e.g. "oauthClientWeb.test.json").
func findOAuthWebFile(env string) (string, error) {
	fileName := "oauthClientWeb.json"
	if env != "" {
		fileName = "oauthClientWeb." + env + ".json"
	}

	if _, err := os.Stat(fileName); err == nil {
		return fileName, nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	homePath := filepath.Join(homeDir, fileName)
	if _, err := os.Stat(homePath); err == nil {
		return homePath, nil
	}

	return "", fmt.Errorf("web oauth client file not found in current directory or home directory")
}
