package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validWebOAuth = `{
  "web": {
    "client_id": "test-client-id.apps.googleusercontent.com",
    "project_id": "test-project",
    "auth_uri": "https://accounts.google.com/o/oauth2/auth",
    "token_uri": "https://oauth2.googleapis.com/token",
    "auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
    "client_secret": "test-secret",
    "redirect_uris": ["http://localhost:5173/auth/callback"]
  }
}`

func TestLoadOAuthClientWebFromPath_ValidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauthClientWeb.json")
	require.NoError(t, os.WriteFile(path, []byte(validWebOAuth), 0644))

	cfg, err := LoadOAuthClientWebFromPath(path)
	require.NoError(t, err)

	assert.Equal(t, "test-client-id.apps.googleusercontent.com", cfg.Web.ClientID)
	assert.Equal(t, "test-secret", cfg.Web.ClientSecret)
	require.Len(t, cfg.Web.RedirectURIs, 1)
	assert.Equal(t, "http://localhost:5173/auth/callback", cfg.Web.RedirectURIs[0])
}

func TestLoadOAuthClientWebFromPath_MissingClientSecret(t *testing.T) {
	missing := `{
  "web": {
    "client_id": "test-client-id",
    "project_id": "test-project",
    "auth_uri": "https://accounts.google.com/o/oauth2/auth",
    "token_uri": "https://oauth2.googleapis.com/token",
    "auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
    "redirect_uris": ["http://localhost:5173/auth/callback"]
  }
}`
	path := filepath.Join(t.TempDir(), "oauthClientWeb.json")
	require.NoError(t, os.WriteFile(path, []byte(missing), 0644))

	_, err := LoadOAuthClientWebFromPath(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestLoadOAuthClientWebFromPath_RejectsInstalledKey(t *testing.T) {
	// An installed-app JSON has no "web" key, so validation must reject it.
	installed := `{
  "installed": {
    "client_id": "test-client-id",
    "client_secret": "test-secret",
    "redirect_uris": ["http://localhost"]
  }
}`
	path := filepath.Join(t.TempDir(), "oauthClientWeb.json")
	require.NoError(t, os.WriteFile(path, []byte(installed), 0644))

	_, err := LoadOAuthClientWebFromPath(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestLoadOAuthClientWebFromPath_InvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauthClientWeb.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"web": {`), 0644))

	_, err := LoadOAuthClientWebFromPath(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse web oauth client file")
}

func TestLoadOAuthClientWebFromPath_FileNotFound(t *testing.T) {
	_, err := LoadOAuthClientWebFromPath("/nonexistent/oauthClientWeb.json")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read web oauth client file")
}
