package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"gopkg.in/yaml.v3"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services/utils"
)

// Preallocation pins one volunteer, or one custom entry, to a Role on every
// date its override matches. Every preallocation names a Role — that is what
// lets a custom entry such as "St John's team" hold something other than an
// ordinary Seat.
type Preallocation struct {
	// VolunteerID pins a volunteer from the roster. Exactly one of VolunteerID
	// and Custom is set.
	VolunteerID string `yaml:"volunteerID,omitempty"`
	// Custom pins a free-text entry — a group or an outside body, with no roster
	// record and no availability.
	Custom string `yaml:"custom,omitempty"`
	// Role is the name of the Role the pin fills. It must be one config names.
	Role string `yaml:"role"`
}

// RotaOverride defines overrides to apply when generating rotas
type RotaOverride struct {
	RRule          string          `yaml:"rrule" validate:"required"`
	ShiftSize      *int            `yaml:"shiftSize,omitempty" validate:"omitempty,min=1"`
	Closed         bool            `yaml:"closed,omitempty"`
	Preallocations []Preallocation `yaml:"preallocations,omitempty"`
}

// ServerConfig holds settings for the HTTP server
type ServerConfig struct {
	Port int `yaml:"port" validate:"required,min=1,max=65535"`
	// SessionSecret signs admin session cookies (HMAC). Keep it secret and stable;
	// rotating it invalidates all live sessions.
	SessionSecret string `yaml:"sessionSecret" validate:"required,min=16"`
	// AdminEmails is the allowlist of Google accounts permitted to log in as Admin.
	// Compared case-insensitively and re-checked on every request.
	AdminEmails []string `yaml:"adminEmails" validate:"required,min=1,dive,email"`
	// RedirectURI names which of the OAuth client's registered redirect URIs to
	// use for the login flow. Optional: when empty the server picks one by
	// locality. Set it where the default guess is wrong — chiefly a git worktree,
	// whose frontend runs on its own port (see docs/agents/worktrees.md).
	RedirectURI string `yaml:"redirectURI,omitempty" validate:"omitempty,uri"`
}

// DevEnv is the only environment the development stubs may run in. It is
// checked by name rather than by "not prod" so a new environment is
// credential-backed unless someone deliberately calls it dev.
const DevEnv = "dev"

// DevModeConfig turns on the credential-free development stubs: the roster is
// read from a CSV file instead of Google Sheets, and login mints an admin
// session for AdminEmail instead of redirecting to Google. Present only in
// drop_in_config.dev.yaml — see checkDevMode. Omit the block entirely for a
// normal, Google-backed server.
type DevModeConfig struct {
	// AdminEmail is the account login signs in as. It must also appear in
	// server.adminEmails, or the session it mints carries no authority.
	AdminEmail string `yaml:"adminEmail" validate:"required,email"`
	// VolunteersCSV is a CSV export of the volunteer sheet — same header row,
	// same columns — read at startup and on each admin sync. Relative paths
	// resolve from the server's working directory.
	VolunteersCSV string `yaml:"volunteersCSV" validate:"required"`
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
	// Roles are the jobs volunteers hold. Config is authoritative for which
	// Roles exist: the roster, pins and the solver all resolve against this
	// list. Validated in Validate, not by tags — the rules are cross-field.
	Roles []model.Role `yaml:"roles"`
	// RequiresMale demands that every open Shift either has a male allocated or
	// leaves a Seat open, so one can be added by hand afterwards. Today's
	// behaviour is unconditional; the flag exists so it can be turned off rather
	// than because anyone will.
	RequiresMale     bool           `yaml:"requiresMale"`
	DefaultShiftSize int            `yaml:"defaultShiftSize" validate:"required,min=1"`
	ShiftStartTime   string         `yaml:"shiftStartTime" validate:"required,datetime=15:04"`
	ShiftEndTime     string         `yaml:"shiftEndTime" validate:"required,datetime=15:04"`
	ShiftTimezone    string         `yaml:"shiftTimezone,omitempty" validate:"omitempty,timezone"`
	Server           *ServerConfig  `yaml:"server,omitempty"`
	DevMode          *DevModeConfig `yaml:"devMode,omitempty"`
}

// DefaultShiftTimezone is used when shiftTimezone is not set in the config
const DefaultShiftTimezone = "Europe/London"

// RoleTable indexes the configured Roles for lookup by name and by priority.
// Everything that has to resolve a Role name — the roster, pins, the solver
// contract — goes through this rather than reading the slice. A nil Config
// yields the empty table, which answers every query with "no such Role" rather
// than panicking on a caller that holds no config.
func (c *Config) RoleTable() model.Roles {
	if c == nil {
		return model.Roles{}
	}
	return model.NewRoles(c.Roles)
}

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

	return LoadPathWithEnv(configPath, env)
}

// LoadPathWithEnv loads and validates the configuration at an explicit path,
// applying the environment-dependent rules that LoadFromPath cannot.
//
// It is the whole of what loading a config means, minus finding the file — so
// a config can be vetted before it is anywhere it would be found, which is what
// scripts/deploy-config.sh does before it ships one to the droplet. It opens
// nothing but the file: no database, no Google client.
func LoadPathWithEnv(path, env string) (*Config, error) {
	cfg, err := LoadFromPath(path)
	if err != nil {
		return nil, err
	}

	// The env is only known here, not in LoadFromPath, so this is where the dev
	// stubs can be pinned to the dev environment.
	if err := checkDevMode(cfg, env); err != nil {
		return nil, err
	}

	return cfg, nil
}

// checkDevMode rejects a devMode block outside the dev environment. The stubs
// replace Google identity with a session minted for a configured address, so
// enabling them anywhere real would hand admin to anyone who can reach
// /auth/login. Failing the load is deliberate: silently ignoring the block
// would leave an operator believing a gate they set is off when it is on.
func checkDevMode(cfg *Config, env string) error {
	if cfg.DevMode == nil || env == DevEnv {
		return nil
	}
	return fmt.Errorf("devMode is only permitted in the %q environment, not %q: remove the devMode block from the config or run with -env %s", DevEnv, env, DevEnv)
}

// LoadFromPath loads and validates the configuration from a specific path.
//
// A key the struct does not know is a warning, not an error — see unknownKeys
// for why the file is allowed to carry keys this build has never heard of.
func LoadFromPath(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&cfg); err != nil {
		// A Decoder reports an empty document as io.EOF, where Unmarshal used to
		// return a zero Config. Say what it is: a truncated copy is the likeliest
		// way to get here, and "EOF" does not suggest looking at the file's size.
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("config file %s is empty", path)
		}
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	for _, unknown := range unknownKeys(data) {
		slog.Warn("config key is not one this version of the app knows; ignoring it",
			"path", path, "key", unknown.Key, "line", unknown.Line)
	}

	if err := Validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// UnknownKey is a key in a config file that this build does not know, and the
// line it sits on. Line is 0 when yaml worded its message in a way this cannot
// read, in which case Key holds that message whole rather than losing it.
type UnknownKey struct {
	Key  string
	Line int
}

// String renders a key the way an operator reads it, not the way yaml phrases
// it: "databaseSheetID (line 59)".
func (u UnknownKey) String() string {
	if u.Line == 0 {
		return u.Key
	}
	return fmt.Sprintf("%s (line %d)", u.Key, u.Line)
}

// UnknownKeys reports the keys in the config file at path that this build does
// not know. Loading warns about them; this is for callers that want to show
// them, chiefly `cli validate-config`.
//
// A file that cannot be read has no keys to report and no error to raise here —
// loading it is what says so.
func UnknownKeys(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	keys := unknownKeys(data)
	described := make([]string, 0, len(keys))
	for _, key := range keys {
		described = append(described, key.String())
	}
	return described
}

// yamlUnknownKey matches the message yaml composes for a key the struct does
// not have — "line 58: field customPreallocations not found in type
// config.Config" — which is the only place the key and its line are exposed.
var yamlUnknownKey = regexp.MustCompile(`^line (\d+): field (\S+) not found in type`)

// unknownKeys reports the keys in a config document that the Config struct does
// not know, each carrying the line number yaml puts in its message.
//
// They are warned about rather than rejected, because a config file outlives any
// one build of the server. The same file is read by whatever version is
// deployed — including the previous one during a rollback, and a branch cut
// before a key existed or after it was removed — so failing the load on a key
// mismatch turns an ordinary version skew into an outage. That is not
// hypothetical: strict decoding took the site down the day it shipped.
//
// The warning is still worth printing, because the other way to get here is a
// key that used to configure something and now configures nothing — which is
// how a live config lost every preallocation to a format change in silence
// (issue #100). A warning names it; only the operator can tell the two apart.
func unknownKeys(data []byte) []UnknownKey {
	var probe Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	err := decoder.Decode(&probe)
	if err == nil {
		return nil
	}

	// Anything that is not a TypeError already failed the real decode above, so
	// there is nothing here to report on top of it.
	var typeErr *yaml.TypeError
	if !errors.As(err, &typeErr) {
		return nil
	}

	var unknown []UnknownKey
	for _, message := range typeErr.Errors {
		// A TypeError also carries mismatches such as "cannot unmarshal !!str into
		// int". Those failed the real decode, so only the unknown-key messages are
		// this function's business.
		if !strings.Contains(message, "not found in type") {
			continue
		}

		match := yamlUnknownKey.FindStringSubmatch(message)
		if match == nil {
			unknown = append(unknown, UnknownKey{Key: message})
			continue
		}
		line, err := strconv.Atoi(match[1])
		if err != nil {
			unknown = append(unknown, UnknownKey{Key: message})
			continue
		}
		unknown = append(unknown, UnknownKey{Key: match[2], Line: line})
	}

	return unknown
}

// Validate validates the configuration struct and checks rrule syntax
func Validate(cfg *Config) error {
	// Run struct validation
	if err := validate.Struct(cfg); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	// The rules below are cross-field — uniqueness across a slice, a Role name
	// resolving against another part of the config — which validator.v10 tags
	// cannot express, so they run here.
	if err := validateRoles(cfg.Roles); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	roles := cfg.RoleTable()

	// Validate rrule syntax for each override, reusing the shared parser so
	// rrule parsing lives in exactly one place.
	for i, override := range cfg.RotaOverrides {
		if _, err := utils.ParseRRule(override.RRule); err != nil {
			return fmt.Errorf("invalid rrule in rotaOverrides[%d]: %w", i, err)
		}
		for j, pin := range override.Preallocations {
			if err := validatePreallocation(pin, roles); err != nil {
				return fmt.Errorf("invalid preallocation in rotaOverrides[%d].preallocations[%d]: %w", i, j, err)
			}
		}
	}

	return nil
}

// validateRoles enforces the rules that make the configured Roles usable as a
// lookup table: names and priorities identify a Role, so they must be unique,
// and a ceiling of zero would configure a Role nobody can ever fill.
//
// Exactly one Role may be uncapped, because a Shift's size is spent on that
// Role's Seats — with two, `shiftSize` would not say how many of each. Slice 2
// replaces `shiftSize` with a per-Shift Shape naming its own counts, and lifts
// this restriction with it.
func validateRoles(roles []model.Role) error {
	if len(roles) == 0 {
		return fmt.Errorf("at least one role must be configured")
	}

	seenNames := make(map[string]bool, len(roles))
	seenPriorities := make(map[int]bool, len(roles))
	var uncapped []string

	for i, role := range roles {
		if role.Name == "" {
			return fmt.Errorf("roles[%d] has no name", i)
		}
		if seenNames[role.Name] {
			return fmt.Errorf("roles[%d] repeats the name %q", i, role.Name)
		}
		seenNames[role.Name] = true

		if seenPriorities[role.Priority] {
			return fmt.Errorf("roles[%d] (%s) repeats priority %d", i, role.Name, role.Priority)
		}
		seenPriorities[role.Priority] = true

		if role.Capped() && *role.Max < 1 {
			return fmt.Errorf("roles[%d] (%s) has max %d; omit max for no ceiling", i, role.Name, *role.Max)
		}
		if !role.Capped() {
			uncapped = append(uncapped, role.Name)
		}
	}

	if len(uncapped) != 1 {
		return fmt.Errorf("exactly one role must be uncapped (no max), found %d: %v", len(uncapped), uncapped)
	}

	return nil
}

// validatePreallocation checks a config pin names one subject and a Role that
// exists. Whether the volunteer holds that Role is a roster question, checked
// where the roster is in scope.
func validatePreallocation(pin Preallocation, roles model.Roles) error {
	if (pin.VolunteerID == "") == (pin.Custom == "") {
		return fmt.Errorf("set exactly one of volunteerID and custom")
	}
	if pin.Role == "" {
		return fmt.Errorf("role is required")
	}
	if _, ok := roles.ByName(pin.Role); !ok {
		return fmt.Errorf("role %q is not a configured role", pin.Role)
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
