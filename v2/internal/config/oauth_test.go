package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateOAuthClient_ValidConfig(t *testing.T) {
	cfg := &OAuthClientConfig{
		Installed: OAuthInstalled{
			ClientID:                "test-client-id.apps.googleusercontent.com",
			ProjectID:               "test-project",
			AuthURI:                 "https://accounts.google.com/o/oauth2/auth",
			TokenURI:                "https://oauth2.googleapis.com/token",
			AuthProviderX509CertURL: "https://www.googleapis.com/oauth2/v1/certs",
			ClientSecret:            "test-secret",
			RedirectURIs:            []string{"http://localhost"},
		},
	}

	err := ValidateOAuthClient(cfg)
	assert.NoError(t, err)
}

func TestValidateOAuthClient_MissingClientID(t *testing.T) {
	cfg := &OAuthClientConfig{
		Installed: OAuthInstalled{
			ProjectID:               "test-project",
			AuthURI:                 "https://accounts.google.com/o/oauth2/auth",
			TokenURI:                "https://oauth2.googleapis.com/token",
			AuthProviderX509CertURL: "https://www.googleapis.com/oauth2/v1/certs",
			ClientSecret:            "test-secret",
			RedirectURIs:            []string{"http://localhost"},
		},
	}

	err := ValidateOAuthClient(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestValidateOAuthClient_InvalidURL(t *testing.T) {
	cfg := &OAuthClientConfig{
		Installed: OAuthInstalled{
			ClientID:                "test-client-id",
			ProjectID:               "test-project",
			AuthURI:                 "not-a-valid-url",
			TokenURI:                "https://oauth2.googleapis.com/token",
			AuthProviderX509CertURL: "https://www.googleapis.com/oauth2/v1/certs",
			ClientSecret:            "test-secret",
			RedirectURIs:            []string{"http://localhost"},
		},
	}

	err := ValidateOAuthClient(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestValidateOAuthClient_EmptyRedirectURIs(t *testing.T) {
	cfg := &OAuthClientConfig{
		Installed: OAuthInstalled{
			ClientID:                "test-client-id",
			ProjectID:               "test-project",
			AuthURI:                 "https://accounts.google.com/o/oauth2/auth",
			TokenURI:                "https://oauth2.googleapis.com/token",
			AuthProviderX509CertURL: "https://www.googleapis.com/oauth2/v1/certs",
			ClientSecret:            "test-secret",
			RedirectURIs:            []string{},
		},
	}

	err := ValidateOAuthClient(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestValidateOAuthClient_InvalidRedirectURI(t *testing.T) {
	cfg := &OAuthClientConfig{
		Installed: OAuthInstalled{
			ClientID:                "test-client-id",
			ProjectID:               "test-project",
			AuthURI:                 "https://accounts.google.com/o/oauth2/auth",
			TokenURI:                "https://oauth2.googleapis.com/token",
			AuthProviderX509CertURL: "https://www.googleapis.com/oauth2/v1/certs",
			ClientSecret:            "test-secret",
			RedirectURIs:            []string{"not a valid uri"},
		},
	}

	err := ValidateOAuthClient(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestLoadOAuthClientFromPath_ValidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	oauthPath := filepath.Join(tmpDir, "oauthClient.json")

	validOAuth := `{
  "installed": {
    "client_id": "test-client-id.apps.googleusercontent.com",
    "project_id": "test-project",
    "auth_uri": "https://accounts.google.com/o/oauth2/auth",
    "token_uri": "https://oauth2.googleapis.com/token",
    "auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
    "client_secret": "test-secret",
    "redirect_uris": ["http://localhost"]
  }
}`

	err := os.WriteFile(oauthPath, []byte(validOAuth), 0644)
	require.NoError(t, err)

	cfg, err := LoadOAuthClientFromPath(oauthPath)
	require.NoError(t, err)

	assert.Equal(t, "test-client-id.apps.googleusercontent.com", cfg.Installed.ClientID)
	assert.Equal(t, "test-project", cfg.Installed.ProjectID)
	assert.Equal(t, "https://accounts.google.com/o/oauth2/auth", cfg.Installed.AuthURI)
	assert.Equal(t, "https://oauth2.googleapis.com/token", cfg.Installed.TokenURI)
	assert.Equal(t, "https://www.googleapis.com/oauth2/v1/certs", cfg.Installed.AuthProviderX509CertURL)
	assert.Equal(t, "test-secret", cfg.Installed.ClientSecret)
	require.Len(t, cfg.Installed.RedirectURIs, 1)
	assert.Equal(t, "http://localhost", cfg.Installed.RedirectURIs[0])
}

func TestLoadOAuthClientFromPath_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	oauthPath := filepath.Join(tmpDir, "invalid_oauth.json")

	invalidJSON := `{
  "installed": {
    "client_id": "test"
    "project_id": "missing comma"
  }
}`

	err := os.WriteFile(oauthPath, []byte(invalidJSON), 0644)
	require.NoError(t, err)

	_, err = LoadOAuthClientFromPath(oauthPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse oauth client file")
}

func TestLoadOAuthClientFromPath_MissingRequired(t *testing.T) {
	tmpDir := t.TempDir()
	oauthPath := filepath.Join(tmpDir, "missing_field.json")

	missingField := `{
  "installed": {
    "client_id": "test-client-id",
    "project_id": "test-project",
    "auth_uri": "https://accounts.google.com/o/oauth2/auth",
    "token_uri": "https://oauth2.googleapis.com/token",
    "auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
    "redirect_uris": ["http://localhost"]
  }
}`

	err := os.WriteFile(oauthPath, []byte(missingField), 0644)
	require.NoError(t, err)

	_, err = LoadOAuthClientFromPath(oauthPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestLoadOAuthClientFromPath_FileNotFound(t *testing.T) {
	_, err := LoadOAuthClientFromPath("/nonexistent/path/oauthClient.json")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read oauth client file")
}

func TestLoadOAuthClientFromPath_MultipleRedirectURIs(t *testing.T) {
	tmpDir := t.TempDir()
	oauthPath := filepath.Join(tmpDir, "multiple_redirects.json")

	multipleRedirects := `{
  "installed": {
    "client_id": "test-client-id",
    "project_id": "test-project",
    "auth_uri": "https://accounts.google.com/o/oauth2/auth",
    "token_uri": "https://oauth2.googleapis.com/token",
    "auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
    "client_secret": "test-secret",
    "redirect_uris": [
      "http://localhost:8080",
      "http://localhost:3000",
      "urn:ietf:wg:oauth:2.0:oob"
    ]
  }
}`

	err := os.WriteFile(oauthPath, []byte(multipleRedirects), 0644)
	require.NoError(t, err)

	cfg, err := LoadOAuthClientFromPath(oauthPath)
	require.NoError(t, err)

	require.Len(t, cfg.Installed.RedirectURIs, 3)
	assert.Contains(t, cfg.Installed.RedirectURIs, "http://localhost:8080")
	assert.Contains(t, cfg.Installed.RedirectURIs, "http://localhost:3000")
	assert.Contains(t, cfg.Installed.RedirectURIs, "urn:ietf:wg:oauth:2.0:oob")
}
