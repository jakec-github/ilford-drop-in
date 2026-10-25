package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// OAuthClientWebConfig represents a Google "Web application" OAuth client.
// Web clients nest their fields under a "web" key; a desktop client's JSON
// nests them under "installed", and is refused here.
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

// LoadOAuthClientWebWithEnv loads and validates an environment's web OAuth client
// configuration, from EnvPath(env, OAuthClientWebFile).
func LoadOAuthClientWebWithEnv(env string) (*OAuthClientWebConfig, error) {
	path, err := findEnvFile(env, OAuthClientWebFile)
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
