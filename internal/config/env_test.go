package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validInstalledOAuth = `{
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

func TestEnvPath(t *testing.T) {
	assert.Equal(t, filepath.Join("env", "prod", "serviceAccount.json"), EnvPath("prod", "serviceAccount.json"))
	assert.Equal(t, filepath.Join("env", "test", "drop_in_config.yaml"), EnvPath("test", ConfigFile))
}

// prod and preprod both run on the droplet behind Caddy's TLS, so whatever
// prod does because it is hosted, preprod must do too.
func TestHosted(t *testing.T) {
	for _, env := range []string{"prod", "preprod"} {
		assert.True(t, Hosted(env), env)
	}
	for _, env := range []string{"dev", "test", ""} {
		assert.False(t, Hosted(env), env)
	}
}

// Each file an environment needs, and the loader that finds it by env.
var envFiles = []struct {
	name    string
	content string
	load    func(env string) error
}{
	{ConfigFile, minimalConfigYAML, func(env string) error { _, err := LoadWithEnv(env); return err }},
	{OAuthClientFile, validInstalledOAuth, func(env string) error { _, err := LoadOAuthClientWithEnv(env); return err }},
	{OAuthClientWebFile, validWebOAuth, func(env string) error { _, err := LoadOAuthClientWebWithEnv(env); return err }},
	{ServiceAccountFile, validServiceAccount, func(env string) error { _, err := LoadServiceAccountWithEnv(env); return err }},
}

func TestLoadersReadTheEnvDirectory(t *testing.T) {
	for _, f := range envFiles {
		t.Run(f.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			require.NoError(t, os.MkdirAll(filepath.Join("env", "prod"), 0o755))
			require.NoError(t, os.WriteFile(EnvPath("prod", f.name), []byte(f.content), 0o644))

			assert.NoError(t, f.load("prod"))
		})
	}
}

// The old layout — the env as a filename suffix at the root or in the home
// directory, or no suffix at all — is not looked at. A missing file names the
// one path that would have been read, so an operator mid-move knows where it goes.
func TestLoadersIgnoreTheOldLayoutAndNameTheExpectedPath(t *testing.T) {
	for _, f := range envFiles {
		t.Run(f.name, func(t *testing.T) {
			dir := t.TempDir()
			home := t.TempDir()
			t.Chdir(dir)
			t.Setenv("HOME", home)

			ext := filepath.Ext(f.name)
			suffixed := f.name[:len(f.name)-len(ext)] + ".prod" + ext
			for _, path := range []string{
				filepath.Join(dir, suffixed),
				filepath.Join(dir, f.name),
				filepath.Join(home, suffixed),
				filepath.Join(home, f.name),
			} {
				require.NoError(t, os.WriteFile(path, []byte(f.content), 0o644))
			}

			err := f.load("prod")
			require.Error(t, err)
			assert.Contains(t, err.Error(), filepath.Join("env", "prod", f.name))
		})
	}
}
