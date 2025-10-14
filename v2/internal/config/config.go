package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-playground/validator/v10"
	"github.com/teambition/rrule-go"
	"gopkg.in/yaml.v3"
)

// RotaOverride defines overrides to apply when generating rotas
type RotaOverride struct {
	RRule          string   `yaml:"rrule" validate:"required"`
	PrefilledAllocations []string `yaml:"prefilledAllocations,omitempty"`
	ShiftSize      *int     `yaml:"shiftSize,omitempty" validate:"omitempty,min=1"`
}

// Config represents the application configuration
type Config struct {
	VolunteerSheetID     string         `yaml:"volunteerSheetID" validate:"required"`
	ServiceVolunteersTab string         `yaml:"serviceVolunteersTab" validate:"required"`
	RotaSheetID          string         `yaml:"rotaSheetID" validate:"required"`
	DatabaseSheetID      string         `yaml:"databaseSheetID" validate:"required"`
	RotaOverrides        []RotaOverride `yaml:"rotaOverrides,omitempty" validate:"dive"`
	GmailUserID          string         `yaml:"gmailUserID" validate:"required"`
	GmailSender          string         `yaml:"gmailSender,omitempty"`
}

var validate *validator.Validate

func init() {
	validate = validator.New()
}

// LoadWithEnv loads and validates the configuration with an environment suffix
// For example, env="test" will look for "drop_in_config.test.yaml"
func LoadWithEnv(env string) (*Config, error) {
	configPath, err := findConfigFile(env)
	if err != nil {
		return nil, fmt.Errorf("failed to find config file: %w", err)
	}

	return LoadFromPath(configPath)
}

// LoadFromPath loads and validates the configuration from a specific path
func LoadFromPath(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := Validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Validate validates the configuration struct and checks rrule syntax
func Validate(cfg *Config) error {
	// Run struct validation
	if err := validate.Struct(cfg); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	// Validate rrule syntax for each override
	for i, override := range cfg.RotaOverrides {
		if _, err := rrule.StrToRRule(override.RRule); err != nil {
			return fmt.Errorf("invalid rrule in rotaOverrides[%d]: %w", i, err)
		}
	}

	return nil
}

// findConfigFile searches for config file in current directory and home directory
// If env is provided, it adds it as an extension (e.g., "drop_in_config.test.yaml")
func findConfigFile(env string) (string, error) {
	configFileName := "drop_in_config.yaml"
	if env != "" {
		configFileName = "drop_in_config." + env + ".yaml"
	}

	// Check current directory
	if _, err := os.Stat(configFileName); err == nil {
		return configFileName, nil
	}

	// Check home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	homeConfigPath := filepath.Join(homeDir, configFileName)
	if _, err := os.Stat(homeConfigPath); err == nil {
		return homeConfigPath, nil
	}

	return "", fmt.Errorf("config file not found in current directory or home directory")
}
