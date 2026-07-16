package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-playground/validator/v10"
	"gopkg.in/yaml.v3"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services/utils"
)

// RotaOverride defines overrides to apply when generating rotas
type RotaOverride struct {
	RRule                    string   `yaml:"rrule" validate:"required"`
	CustomPreallocations     []string `yaml:"customPreallocations,omitempty"`
	ShiftSize                *int     `yaml:"shiftSize,omitempty" validate:"omitempty,min=1"`
	Closed                   bool     `yaml:"closed,omitempty"`
	PreallocatedVolunteerIDs []string `yaml:"preallocatedVolunteerIDs,omitempty"`
	PreallocatedTeamLeadID   string   `yaml:"preallocatedTeamLeadID,omitempty"`
}

// ServerConfig holds settings for the HTTP server
type ServerConfig struct {
	Port int `yaml:"port" validate:"required,min=1,max=65535"`
}

// Config represents the application configuration
type Config struct {
	VolunteerSheetID       string         `yaml:"volunteerSheetID" validate:"required"`
	ServiceVolunteersTab   string         `yaml:"serviceVolunteersTab" validate:"required"`
	RotaSheetID            string         `yaml:"rotaSheetID" validate:"required"`
	DatabaseURL            string         `yaml:"databaseURL" validate:"required"`
	RotaOverrides          []RotaOverride `yaml:"rotaOverrides,omitempty" validate:"dive"`
	GmailUserID            string         `yaml:"gmailUserID" validate:"required"`
	GmailSender            string         `yaml:"gmailSender,omitempty"`
	MaxAllocationFrequency float64        `yaml:"maxAllocationFrequency" validate:"required,gt=0,lte=1"`
	DefaultShiftSize       int            `yaml:"defaultShiftSize" validate:"required,min=1"`
	ShiftStartTime         string         `yaml:"shiftStartTime" validate:"required,datetime=15:04"`
	ShiftEndTime           string         `yaml:"shiftEndTime" validate:"required,datetime=15:04"`
	ShiftTimezone          string         `yaml:"shiftTimezone,omitempty" validate:"omitempty,timezone"`
	Server                 *ServerConfig  `yaml:"server,omitempty"`
}

// DefaultShiftTimezone is used when shiftTimezone is not set in the config
const DefaultShiftTimezone = "Europe/London"

// ShiftTimes returns the absolute start and end times of the shift on the
// given date ("2006-01-02"), interpreted in the configured timezone.
func (c *Config) ShiftTimes(dateStr string) (start, end time.Time, err error) {
	tz := c.ShiftTimezone
	if tz == "" {
		tz = DefaultShiftTimezone
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("failed to load shift timezone %q: %w", tz, err)
	}

	start, err = time.ParseInLocation("2006-01-02 15:04", dateStr+" "+c.ShiftStartTime, loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("failed to parse shift start for %q: %w", dateStr, err)
	}

	end, err = time.ParseInLocation("2006-01-02 15:04", dateStr+" "+c.ShiftEndTime, loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("failed to parse shift end for %q: %w", dateStr, err)
	}

	return start, end, nil
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

	// Validate rrule syntax for each override, reusing the shared parser so
	// rrule parsing lives in exactly one place.
	for i, override := range cfg.RotaOverrides {
		if _, err := utils.ParseRRule(override.RRule); err != nil {
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
