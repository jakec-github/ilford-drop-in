package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// OAuthClientConfig represents the Google OAuth client configuration
type OAuthClientConfig struct {
	Installed OAuthInstalled `json:"installed" validate:"required"`
}

// OAuthInstalled represents the installed section of OAuth config
type OAuthInstalled struct {
	ClientID                string   `json:"client_id" validate:"required"`
	ProjectID               string   `json:"project_id" validate:"required"`
	AuthURI                 string   `json:"auth_uri" validate:"required,url"`
	TokenURI                string   `json:"token_uri" validate:"required,url"`
	AuthProviderX509CertURL string   `json:"auth_provider_x509_cert_url" validate:"required,url"`
	ClientSecret            string   `json:"client_secret" validate:"required"`
	RedirectURIs            []string `json:"redirect_uris" validate:"required,min=1,dive,uri"`
}

// LoadOAuthClientWithEnv loads and validates an environment's OAuth client
// configuration, from EnvPath(env, OAuthClientFile).
func LoadOAuthClientWithEnv(env string) (*OAuthClientConfig, error) {
	oauthPath, err := findEnvFile(env, OAuthClientFile)
	if err != nil {
		return nil, fmt.Errorf("failed to find oauth client file: %w", err)
	}

	return LoadOAuthClientFromPath(oauthPath)
}

// LoadOAuthClientFromPath loads and validates the OAuth client configuration from a specific path
func LoadOAuthClientFromPath(path string) (*OAuthClientConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read oauth client file: %w", err)
	}

	var oauthCfg OAuthClientConfig
	if err := json.Unmarshal(data, &oauthCfg); err != nil {
		return nil, fmt.Errorf("failed to parse oauth client file: %w", err)
	}

	if err := ValidateOAuthClient(&oauthCfg); err != nil {
		return nil, err
	}

	return &oauthCfg, nil
}

// ValidateOAuthClient validates the OAuth client configuration
func ValidateOAuthClient(cfg *OAuthClientConfig) error {
	if err := validate.Struct(cfg); err != nil {
		return fmt.Errorf("oauth client validation failed: %w", err)
	}

	return nil
}
