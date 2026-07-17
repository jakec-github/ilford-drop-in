package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validServiceAccount is a minimal, structurally valid service account key. The
// private_key is a throwaway placeholder — the loader validates identity fields
// and retains the raw bytes, it does not verify the key cryptographically.
const validServiceAccount = `{
  "type": "service_account",
  "project_id": "test-project",
  "private_key_id": "abc123",
  "private_key": "-----BEGIN PRIVATE KEY-----\nfake\n-----END PRIVATE KEY-----\n",
  "client_email": "sheets-reader@test-project.iam.gserviceaccount.com",
  "client_id": "1234567890",
  "token_uri": "https://oauth2.googleapis.com/token"
}`

func TestLoadServiceAccountFromPath_ValidKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serviceAccount.json")
	require.NoError(t, os.WriteFile(path, []byte(validServiceAccount), 0600))

	key, err := LoadServiceAccountFromPath(path)
	require.NoError(t, err)

	assert.Equal(t, "service_account", key.Type)
	assert.Equal(t, "sheets-reader@test-project.iam.gserviceaccount.com", key.ClientEmail)
	assert.Equal(t, "test-project", key.ProjectID)
	assert.JSONEq(t, validServiceAccount, string(key.JSON), "the raw key must be retained verbatim for the Google client")
}

func TestLoadServiceAccountFromPath_RejectsNonServiceAccount(t *testing.T) {
	// An OAuth-client or user-credential JSON has a different type; the loader
	// must reject it rather than hand it to the Sheets client.
	wrongType := `{
  "type": "authorized_user",
  "project_id": "test-project",
  "client_email": "user@example.com"
}`
	path := filepath.Join(t.TempDir(), "serviceAccount.json")
	require.NoError(t, os.WriteFile(path, []byte(wrongType), 0600))

	_, err := LoadServiceAccountFromPath(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestLoadServiceAccountFromPath_MissingClientEmail(t *testing.T) {
	missing := `{
  "type": "service_account",
  "project_id": "test-project"
}`
	path := filepath.Join(t.TempDir(), "serviceAccount.json")
	require.NoError(t, os.WriteFile(path, []byte(missing), 0600))

	_, err := LoadServiceAccountFromPath(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestLoadServiceAccountFromPath_InvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serviceAccount.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"type":`), 0600))

	_, err := LoadServiceAccountFromPath(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse service account file")
}

func TestLoadServiceAccountFromPath_FileNotFound(t *testing.T) {
	_, err := LoadServiceAccountFromPath("/nonexistent/serviceAccount.json")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read service account file")
}
