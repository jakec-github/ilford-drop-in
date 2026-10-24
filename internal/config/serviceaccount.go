package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// ServiceAccountKey is a Google service account key with read access to the
// volunteer sheet. The server authenticates as this account to populate the
// volunteer roster at startup and on an Organiser-triggered sync — no per-user
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

// LoadServiceAccountWithEnv loads and validates an environment's service account
// key, from EnvPath(env, ServiceAccountFile).
func LoadServiceAccountWithEnv(env string) (*ServiceAccountKey, error) {
	path, err := findEnvFile(env, ServiceAccountFile)
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
