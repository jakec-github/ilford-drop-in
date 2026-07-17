package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ServiceAccountKey is a Google service account key with read access to the
// volunteer sheet. The server authenticates as this account to populate the
// volunteer roster at startup and on an admin-triggered sync — no per-user
// token is involved. Only the fields needed to identify and sanity-check the
// key are parsed; the raw JSON is retained to hand to the Google client
// libraries, which parse the full key themselves.
type ServiceAccountKey struct {
	Type        string `json:"type" validate:"required,eq=service_account"`
	ClientEmail string `json:"client_email" validate:"required,email"`
	ProjectID   string `json:"project_id" validate:"required"`
	// JSON is the verbatim key file, passed to option.WithCredentialsJSON.
	JSON []byte `json:"-"`
}

// LoadServiceAccountWithEnv loads and validates the service account key with an
// environment suffix. For example, env="test" looks for "serviceAccount.test.json".
func LoadServiceAccountWithEnv(env string) (*ServiceAccountKey, error) {
	path, err := findServiceAccountFile(env)
	if err != nil {
		return nil, fmt.Errorf("failed to find service account file: %w", err)
	}

	return LoadServiceAccountFromPath(path)
}

// LoadServiceAccountFromPath loads and validates the service account key from a
// specific path, retaining the raw bytes for the Google client libraries.
func LoadServiceAccountFromPath(path string) (*ServiceAccountKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read service account file: %w", err)
	}

	var key ServiceAccountKey
	if err := json.Unmarshal(data, &key); err != nil {
		return nil, fmt.Errorf("failed to parse service account file: %w", err)
	}
	key.JSON = data

	if err := validate.Struct(&key); err != nil {
		return nil, fmt.Errorf("service account validation failed: %w", err)
	}

	return &key, nil
}

// findServiceAccountFile searches for the service account key in the current
// directory and the home directory. If env is provided it is added as an
// extension (e.g. "serviceAccount.test.json").
func findServiceAccountFile(env string) (string, error) {
	fileName := "serviceAccount.json"
	if env != "" {
		fileName = "serviceAccount." + env + ".json"
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

	return "", fmt.Errorf("service account file not found in current directory or home directory")
}
