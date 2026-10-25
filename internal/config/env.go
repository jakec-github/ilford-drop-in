package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// The files an environment can need, each in that environment's own directory
// (see EnvPath). The directory says which environment a file is for, so the
// names carry no suffix.
const (
	ConfigFile         = "drop_in_config.yaml"
	OAuthClientWebFile = "oauthClientWeb.json"
	ServiceAccountFile = "serviceAccount.json"
)

// Hosted reports whether env runs on the droplet, in a container behind
// Caddy's TLS: prod, and preprod, which exists to behave like it. A hosted
// environment logs to stdout only, sets Secure cookies and expects a public
// redirect URI; the rest run on a laptop.
func Hosted(env string) bool {
	return env == "prod" || env == "preprod"
}

// EnvPath is where an environment's file lives: env/<env>/<name>, relative to
// the working directory. It is the only place a file is looked for — there is
// no fallback to the home directory or to the old root-level, suffixed names,
// so a file that is missing is missing from exactly this path.
func EnvPath(env, name string) string {
	return filepath.Join("env", env, name)
}

// findEnvFile returns EnvPath(env, name) if a file is there, and otherwise an
// error naming that path.
func findEnvFile(env, name string) (string, error) {
	path := EnvPath(env, name)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("%s not found", path)
	}
	return path, nil
}
