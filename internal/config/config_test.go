package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

func intPtr(i int) *int { return &i }

// validRoles is the pair of Roles S1 configures — one capped, one uncapped,
// which is the shape every valid config has to have.
func validRoles() []model.Role {
	return []model.Role{
		{Name: "Team lead", Max: intPtr(1), Priority: 1},
		{Name: "Service volunteer", Priority: 2},
	}
}

// baseConfig is a valid config the role tests vary one field of.
func baseConfig() *Config {
	return &Config{
		VolunteerSheetID:       "sheet123",
		ServiceVolunteersTab:   "Volunteers",
		RotaSheetID:            "rota456",
		DatabaseURL:            "postgres://localhost:5432/test",
		GmailUserID:            "user@example.com",
		MaxAllocationFrequency: 0.25,
		DefaultShiftSize:       2,
		ShiftStartTime:         "19:30",
		ShiftEndTime:           "21:30",
		Roles:                  validRoles(),
	}
}

// Config is authoritative for which Roles exist, so the list has to be usable
// as a lookup table: a name and a priority each identify one Role.
func TestValidate_Roles(t *testing.T) {
	tests := []struct {
		name    string
		roles   []model.Role
		wantErr string
	}{
		{
			name:    "no roles at all",
			roles:   nil,
			wantErr: "at least one role",
		},
		{
			name:    "unnamed role",
			roles:   []model.Role{{Max: intPtr(1), Priority: 1}, {Name: "Service volunteer", Priority: 2}},
			wantErr: "roles[0] has no name",
		},
		{
			name: "duplicate name",
			roles: []model.Role{
				{Name: "Team lead", Max: intPtr(1), Priority: 1},
				{Name: "Team lead", Priority: 2},
			},
			wantErr: `roles[1] repeats the name "Team lead"`,
		},
		{
			name: "duplicate priority",
			roles: []model.Role{
				{Name: "Team lead", Max: intPtr(1), Priority: 1},
				{Name: "Service volunteer", Priority: 1},
			},
			wantErr: "roles[1] (Service volunteer) repeats priority 1",
		},
		{
			name: "ceiling of zero",
			roles: []model.Role{
				{Name: "Team lead", Max: intPtr(0), Priority: 1},
				{Name: "Service volunteer", Priority: 2},
			},
			wantErr: "roles[0] (Team lead) has max 0",
		},
		{
			// A Shift's size is spent on the uncapped Role's Seats, so two of
			// them leaves shiftSize meaningless. Slice 2 lifts this.
			name: "two uncapped roles",
			roles: []model.Role{
				{Name: "Service volunteer", Priority: 1},
				{Name: "Hot food", Priority: 2},
			},
			wantErr: "exactly one role must be uncapped",
		},
		{
			name: "every role capped",
			roles: []model.Role{
				{Name: "Team lead", Max: intPtr(1), Priority: 1},
				{Name: "Hot food", Max: intPtr(2), Priority: 2},
			},
			wantErr: "exactly one role must be uncapped",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			cfg.Roles = tt.roles

			err := Validate(cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// Every preallocation names one subject and one configured Role — the check
// that stops a typo becoming a pin the solver silently drops.
func TestValidate_Preallocations(t *testing.T) {
	tests := []struct {
		name    string
		pin     Preallocation
		wantErr string
	}{
		{
			name: "volunteer pinned to a role",
			pin:  Preallocation{VolunteerID: "vol-1", Role: "Team lead"},
		},
		{
			name: "custom entry pinned to a role",
			pin:  Preallocation{Custom: "St John's team", Role: "Service volunteer"},
		},
		{
			name:    "neither volunteer nor custom",
			pin:     Preallocation{Role: "Team lead"},
			wantErr: "exactly one of volunteerID and custom",
		},
		{
			name:    "both volunteer and custom",
			pin:     Preallocation{VolunteerID: "vol-1", Custom: "St John's team", Role: "Team lead"},
			wantErr: "exactly one of volunteerID and custom",
		},
		{
			name:    "no role",
			pin:     Preallocation{VolunteerID: "vol-1"},
			wantErr: "role is required",
		},
		{
			name:    "role config does not name",
			pin:     Preallocation{VolunteerID: "vol-1", Role: "Hot food"},
			wantErr: `role "Hot food" is not a configured role`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			cfg.RotaOverrides = []RotaOverride{{
				RRule:          "FREQ=WEEKLY;BYDAY=SU",
				Preallocations: []Preallocation{tt.pin},
			}}

			err := Validate(cfg)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Contains(t, err.Error(), "rotaOverrides[0].preallocations[0]")
		})
	}
}

func TestRoleTable(t *testing.T) {
	cfg := baseConfig()
	cfg.Roles = []model.Role{
		{Name: "Service volunteer", Priority: 2},
		{Name: "Team lead", Max: intPtr(1), Priority: 1},
	}

	roles := cfg.RoleTable()

	lead, ok := roles.ByName("Team lead")
	require.True(t, ok)
	require.NotNil(t, lead.Max)
	assert.Equal(t, 1, *lead.Max)

	uncapped, ok := roles.Uncapped()
	require.True(t, ok)
	assert.Equal(t, "Service volunteer", uncapped.Name)

	assert.Equal(t, "Team lead", roles.ByPriority()[0].Name)
}

// Roles and requiresMale round-trip from the file, since a config that parses
// but loses its roles would fail much later and much less clearly.
func TestLoadFromPath_RolesAndRequiresMale(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "roles.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(`
volunteerSheetID: "sheet123"
serviceVolunteersTab: "Volunteers"
rotaSheetID: "rota456"
databaseURL: "postgres://localhost:5432/test"
gmailUserID: "user@example.com"
maxAllocationFrequency: 0.25
defaultShiftSize: 2
shiftStartTime: "19:30"
shiftEndTime: "21:30"
requiresMale: true
roles:
  - name: "Team lead"
    max: 1
    priority: 1
  - name: "Service volunteer"
    priority: 2
rotaOverrides:
  - rrule: "FREQ=WEEKLY;BYDAY=SU"
    preallocations:
      - volunteerID: "vol-1"
        role: "Team lead"
      - custom: "St John's team"
        role: "Service volunteer"
`), 0644))

	cfg, err := LoadFromPath(configPath)
	require.NoError(t, err)

	assert.True(t, cfg.RequiresMale)
	require.Len(t, cfg.Roles, 2)
	assert.Equal(t, "Team lead", cfg.Roles[0].Name)
	require.NotNil(t, cfg.Roles[0].Max)
	assert.Equal(t, 1, *cfg.Roles[0].Max)
	assert.Equal(t, "Service volunteer", cfg.Roles[1].Name)
	assert.Nil(t, cfg.Roles[1].Max, "the uncapped role carries no ceiling")

	require.Len(t, cfg.RotaOverrides[0].Preallocations, 2)
	assert.Equal(t, Preallocation{VolunteerID: "vol-1", Role: "Team lead"}, cfg.RotaOverrides[0].Preallocations[0])
	assert.Equal(t, Preallocation{Custom: "St John's team", Role: "Service volunteer"}, cfg.RotaOverrides[0].Preallocations[1])
}

func TestValidate_ValidConfig(t *testing.T) {
	shiftSize := 5
	cfg := &Config{
		VolunteerSheetID:       "sheet123",
		ServiceVolunteersTab:   "Volunteers",
		RotaSheetID:            "rota456",
		DatabaseURL:            "postgres://localhost:5432/test",
		GmailUserID:            "user@example.com",
		GmailSender:            "sender@example.com",
		MaxAllocationFrequency: 0.25,
		DefaultShiftSize:       2,
		ShiftStartTime:         "19:30",
		ShiftEndTime:           "21:30",
		Roles:                  validRoles(),
		RotaOverrides: []RotaOverride{
			{
				RRule: "FREQ=WEEKLY;BYDAY=SU",
				Preallocations: []Preallocation{
					{Custom: "John Doe", Role: "Service volunteer"},
					{Custom: "Jane Smith", Role: "Service volunteer"},
				},
				ShiftSize: &shiftSize,
			},
		},
	}

	err := Validate(cfg)
	assert.NoError(t, err)
}

func TestValidate_MinimalConfig(t *testing.T) {
	cfg := &Config{
		VolunteerSheetID:       "sheet123",
		ServiceVolunteersTab:   "Volunteers",
		RotaSheetID:            "rota456",
		DatabaseURL:            "postgres://localhost:5432/test",
		GmailUserID:            "user@example.com",
		MaxAllocationFrequency: 0.25,
		DefaultShiftSize:       2,
		ShiftStartTime:         "19:30",
		ShiftEndTime:           "21:30",
		Roles:                  validRoles(),
	}

	err := Validate(cfg)
	assert.NoError(t, err)
}

func TestValidate_MissingRequiredField(t *testing.T) {
	cfg := &Config{
		VolunteerSheetID:     "sheet123",
		ServiceVolunteersTab: "Volunteers",
		RotaSheetID:          "rota456",
		// Missing DatabaseURL
		GmailUserID: "user@example.com",
	}

	err := Validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestValidate_InvalidRRule(t *testing.T) {
	cfg := &Config{
		VolunteerSheetID:       "sheet123",
		ServiceVolunteersTab:   "Volunteers",
		RotaSheetID:            "rota456",
		DatabaseURL:            "postgres://localhost:5432/test",
		GmailUserID:            "user@example.com",
		MaxAllocationFrequency: 0.25,
		DefaultShiftSize:       2,
		ShiftStartTime:         "19:30",
		ShiftEndTime:           "21:30",
		Roles:                  validRoles(),
		RotaOverrides: []RotaOverride{
			{
				RRule:          "INVALID_RRULE_SYNTAX",
				Preallocations: []Preallocation{{Custom: "John Doe", Role: "Service volunteer"}},
			},
		},
	}

	err := Validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid rrule")
}

func TestValidate_MultipleInvalidRRules(t *testing.T) {
	cfg := &Config{
		VolunteerSheetID:       "sheet123",
		ServiceVolunteersTab:   "Volunteers",
		RotaSheetID:            "rota456",
		DatabaseURL:            "postgres://localhost:5432/test",
		GmailUserID:            "user@example.com",
		MaxAllocationFrequency: 0.25,
		DefaultShiftSize:       2,
		ShiftStartTime:         "19:30",
		ShiftEndTime:           "21:30",
		Roles:                  validRoles(),
		RotaOverrides: []RotaOverride{
			{
				RRule: "FREQ=WEEKLY;BYDAY=SU",
			},
			{
				RRule: "INVALID_RRULE",
			},
		},
	}

	err := Validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid rrule")
}

func TestValidate_EmptyRRule(t *testing.T) {
	cfg := &Config{
		VolunteerSheetID:       "sheet123",
		ServiceVolunteersTab:   "Volunteers",
		RotaSheetID:            "rota456",
		DatabaseURL:            "postgres://localhost:5432/test",
		GmailUserID:            "user@example.com",
		MaxAllocationFrequency: 0.25,
		DefaultShiftSize:       2,
		ShiftStartTime:         "19:30",
		ShiftEndTime:           "21:30",
		Roles:                  validRoles(),
		RotaOverrides: []RotaOverride{
			{
				RRule:          "",
				Preallocations: []Preallocation{{Custom: "John Doe", Role: "Service volunteer"}},
			},
		},
	}

	err := Validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestValidate_ComplexValidRRule(t *testing.T) {
	cfg := &Config{
		VolunteerSheetID:       "sheet123",
		ServiceVolunteersTab:   "Volunteers",
		RotaSheetID:            "rota456",
		DatabaseURL:            "postgres://localhost:5432/test",
		GmailUserID:            "user@example.com",
		MaxAllocationFrequency: 0.25,
		DefaultShiftSize:       2,
		ShiftStartTime:         "19:30",
		ShiftEndTime:           "21:30",
		Roles:                  validRoles(),
		RotaOverrides: []RotaOverride{
			{
				RRule: "FREQ=MONTHLY;BYDAY=1SU;BYMONTH=1,4,7,10",
			},
		},
	}

	err := Validate(cfg)
	assert.NoError(t, err)
}

func TestLoadFromPath_ValidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_config.yaml")

	validConfig := `
volunteerSheetID: "sheet123"
serviceVolunteersTab: "Volunteers"
rotaSheetID: "rota456"
databaseURL: "postgres://localhost:5432/test"
gmailUserID: "user@example.com"
gmailSender: "sender@example.com"
maxAllocationFrequency: 0.25
defaultShiftSize: 2
shiftStartTime: "19:30"
shiftEndTime: "21:30"
requiresMale: true
roles:
  - name: "Team lead"
    max: 1
    priority: 1
  - name: "Service volunteer"
    priority: 2
rotaOverrides:
  - rrule: "FREQ=WEEKLY;BYDAY=SU"
    preallocations:
      - custom: "John Doe"
        role: "Service volunteer"
      - custom: "Jane Smith"
        role: "Service volunteer"
    shiftSize: 5
`

	err := os.WriteFile(configPath, []byte(validConfig), 0644)
	require.NoError(t, err)

	cfg, err := LoadFromPath(configPath)
	require.NoError(t, err)

	// Verify required fields
	assert.Equal(t, "sheet123", cfg.VolunteerSheetID)
	assert.Equal(t, "Volunteers", cfg.ServiceVolunteersTab)
	assert.Equal(t, "rota456", cfg.RotaSheetID)
	assert.Equal(t, "postgres://localhost:5432/test", cfg.DatabaseURL)
	assert.Equal(t, "user@example.com", cfg.GmailUserID)
	assert.Equal(t, "sender@example.com", cfg.GmailSender)

	// Verify optional rotaOverrides
	require.Len(t, cfg.RotaOverrides, 1)
	override := cfg.RotaOverrides[0]
	assert.Equal(t, "FREQ=WEEKLY;BYDAY=SU", override.RRule)
	require.Len(t, override.Preallocations, 2)
	assert.Equal(t, Preallocation{Custom: "John Doe", Role: "Service volunteer"}, override.Preallocations[0])
	assert.Equal(t, Preallocation{Custom: "Jane Smith", Role: "Service volunteer"}, override.Preallocations[1])
	require.NotNil(t, override.ShiftSize)
	assert.Equal(t, 5, *override.ShiftSize)
}

func TestLoadFromPath_InvalidRRule(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid_rrule.yaml")

	invalidConfig := `
volunteerSheetID: "sheet123"
serviceVolunteersTab: "Volunteers"
rotaSheetID: "rota456"
databaseURL: "postgres://localhost:5432/test"
gmailUserID: "user@example.com"
maxAllocationFrequency: 0.25
defaultShiftSize: 2
shiftStartTime: "19:30"
shiftEndTime: "21:30"
requiresMale: true
roles:
  - name: "Team lead"
    max: 1
    priority: 1
  - name: "Service volunteer"
    priority: 2
rotaOverrides:
  - rrule: "INVALID_RRULE_SYNTAX"
    preallocations:
      - custom: "John Doe"
        role: "Service volunteer"
`

	err := os.WriteFile(configPath, []byte(invalidConfig), 0644)
	require.NoError(t, err)

	_, err = LoadFromPath(configPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid rrule")
}

func TestLoadFromPath_MinimalConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "minimal_config.yaml")

	minimalConfig := `
volunteerSheetID: "sheet123"
serviceVolunteersTab: "Volunteers"
rotaSheetID: "rota456"
databaseURL: "postgres://localhost:5432/test"
gmailUserID: "user@example.com"
maxAllocationFrequency: 0.25
defaultShiftSize: 2
shiftStartTime: "19:30"
shiftEndTime: "21:30"
requiresMale: true
roles:
  - name: "Team lead"
    max: 1
    priority: 1
  - name: "Service volunteer"
    priority: 2
`

	err := os.WriteFile(configPath, []byte(minimalConfig), 0644)
	require.NoError(t, err)

	cfg, err := LoadFromPath(configPath)
	require.NoError(t, err)

	assert.Equal(t, "sheet123", cfg.VolunteerSheetID)
	assert.Empty(t, cfg.GmailSender)
	assert.Empty(t, cfg.RotaOverrides)
}

func TestLoadFromPath_MissingRequiredField(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid_config.yaml")

	invalidConfig := `
volunteerSheetID: "sheet123"
serviceVolunteersTab: "Volunteers"
rotaSheetID: "rota456"
# Missing databaseURL
gmailUserID: "user@example.com"
`

	err := os.WriteFile(configPath, []byte(invalidConfig), 0644)
	require.NoError(t, err)

	_, err = LoadFromPath(configPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestLoadFromPath_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid_yaml.yaml")

	invalidYAML := `
volunteerSheetID: "sheet123"
  invalid indentation
rotaSheetID: "rota456"
`

	err := os.WriteFile(configPath, []byte(invalidYAML), 0644)
	require.NoError(t, err)

	_, err = LoadFromPath(configPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse config file")
}

func TestLoadFromPath_FileNotFound(t *testing.T) {
	_, err := LoadFromPath("/nonexistent/path/config.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file")
}
func TestLoadFromPath_RotaOverrideWithoutRRule(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid_override.yaml")

	invalidOverride := `
volunteerSheetID: "sheet123"
serviceVolunteersTab: "Volunteers"
rotaSheetID: "rota456"
databaseURL: "postgres://localhost:5432/test"
gmailUserID: "user@example.com"
maxAllocationFrequency: 0.25
defaultShiftSize: 2
shiftStartTime: "19:30"
shiftEndTime: "21:30"
requiresMale: true
roles:
  - name: "Team lead"
    max: 1
    priority: 1
  - name: "Service volunteer"
    priority: 2
rotaOverrides:
  - preallocations:
      - custom: "John Doe"
        role: "Service volunteer"
    shiftSize: 5
`

	err := os.WriteFile(configPath, []byte(invalidOverride), 0644)
	require.NoError(t, err)

	_, err = LoadFromPath(configPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestValidate_InvalidShiftTime(t *testing.T) {
	cfg := &Config{
		VolunteerSheetID:       "sheet123",
		ServiceVolunteersTab:   "Volunteers",
		RotaSheetID:            "rota456",
		DatabaseURL:            "postgres://localhost:5432/test",
		GmailUserID:            "user@example.com",
		MaxAllocationFrequency: 0.25,
		DefaultShiftSize:       2,
		ShiftStartTime:         "7:30pm",
		ShiftEndTime:           "21:30",
		Roles:                  validRoles(),
	}

	err := Validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestValidate_InvalidShiftTimezone(t *testing.T) {
	cfg := &Config{
		VolunteerSheetID:       "sheet123",
		ServiceVolunteersTab:   "Volunteers",
		RotaSheetID:            "rota456",
		DatabaseURL:            "postgres://localhost:5432/test",
		GmailUserID:            "user@example.com",
		MaxAllocationFrequency: 0.25,
		DefaultShiftSize:       2,
		ShiftStartTime:         "19:30",
		ShiftEndTime:           "21:30",
		Roles:                  validRoles(),
		ShiftTimezone:          "Not/AZone",
	}

	err := Validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestValidate_ServerConfig(t *testing.T) {
	base := Config{
		VolunteerSheetID:       "sheet123",
		ServiceVolunteersTab:   "Volunteers",
		RotaSheetID:            "rota456",
		DatabaseURL:            "postgres://localhost:5432/test",
		GmailUserID:            "user@example.com",
		MaxAllocationFrequency: 0.25,
		DefaultShiftSize:       2,
		ShiftStartTime:         "19:30",
		ShiftEndTime:           "21:30",
		Roles:                  validRoles(),
	}

	validServer := func() *ServerConfig {
		return &ServerConfig{
			Port:          8080,
			SessionSecret: "a-sufficiently-long-secret",
			AdminEmails:   []string{"admin@example.com"},
		}
	}

	valid := base
	valid.Server = validServer()
	assert.NoError(t, Validate(&valid))

	invalidPort := base
	invalidPort.Server = validServer()
	invalidPort.Server.Port = 0
	assert.Error(t, Validate(&invalidPort))

	missingSecret := base
	missingSecret.Server = validServer()
	missingSecret.Server.SessionSecret = ""
	assert.Error(t, Validate(&missingSecret))

	noAdmins := base
	noAdmins.Server = validServer()
	noAdmins.Server.AdminEmails = nil
	assert.Error(t, Validate(&noAdmins))

	badAdminEmail := base
	badAdminEmail.Server = validServer()
	badAdminEmail.Server.AdminEmails = []string{"not-an-email"}
	assert.Error(t, Validate(&badAdminEmail))
}

func TestValidate_DevMode(t *testing.T) {
	base := Config{
		VolunteerSheetID:       "sheet123",
		ServiceVolunteersTab:   "Volunteers",
		RotaSheetID:            "rota456",
		DatabaseURL:            "postgres://localhost:5432/test",
		GmailUserID:            "user@example.com",
		MaxAllocationFrequency: 0.25,
		DefaultShiftSize:       2,
		ShiftStartTime:         "19:30",
		ShiftEndTime:           "21:30",
		Roles:                  validRoles(),
	}

	validDevMode := func() *DevModeConfig {
		return &DevModeConfig{
			AdminEmail:    "agent@example.com",
			VolunteersCSV: "test_data/volunteers.csv",
		}
	}

	valid := base
	valid.DevMode = validDevMode()
	assert.NoError(t, Validate(&valid))

	missingEmail := base
	missingEmail.DevMode = validDevMode()
	missingEmail.DevMode.AdminEmail = ""
	assert.Error(t, Validate(&missingEmail))

	badEmail := base
	badEmail.DevMode = validDevMode()
	badEmail.DevMode.AdminEmail = "not-an-email"
	assert.Error(t, Validate(&badEmail))

	missingCSV := base
	missingCSV.DevMode = validDevMode()
	missingCSV.DevMode.VolunteersCSV = ""
	assert.Error(t, Validate(&missingCSV))
}

// The dev stubs replace Google with a roster file and a session minted for a
// configured address — catastrophic in prod, where anyone could then log in as
// an admin. The env name is the gate: only "dev" may carry a devMode block.
func TestCheckDevMode_OnlyPermittedInDevEnv(t *testing.T) {
	withDevMode := &Config{DevMode: &DevModeConfig{
		AdminEmail:    "agent@example.com",
		VolunteersCSV: "test_data/volunteers.csv",
	}}

	assert.NoError(t, checkDevMode(withDevMode, DevEnv))

	for _, env := range []string{"prod", "test", "staging", ""} {
		err := checkDevMode(withDevMode, env)
		require.Error(t, err, "env %q must not be allowed to enable dev mode", env)
		assert.Contains(t, err.Error(), "devMode")
	}

	// A config without the block is fine anywhere, prod included.
	for _, env := range []string{"prod", "test", DevEnv} {
		assert.NoError(t, checkDevMode(&Config{}, env))
	}
}

// The gate has to hold on the path the server actually loads config through,
// not just on the helper underneath it.
func TestLoadWithEnv_RejectsDevModeInProd(t *testing.T) {
	dir := t.TempDir()
	cfg := `
volunteerSheetID: "sheet123"
serviceVolunteersTab: "Volunteers"
rotaSheetID: "rota456"
databaseURL: "postgres://localhost:5432/test"
gmailUserID: "user@example.com"
maxAllocationFrequency: 0.25
defaultShiftSize: 2
shiftStartTime: "19:30"
shiftEndTime: "21:30"
requiresMale: true
roles:
  - name: "Team lead"
    max: 1
    priority: 1
  - name: "Service volunteer"
    priority: 2
server:
  port: 8080
  sessionSecret: "a-sufficiently-long-secret"
  adminEmails:
    - "admin@example.com"
devMode:
  adminEmail: "agent@example.com"
  volunteersCSV: "test_data/volunteers.csv"
`
	for _, env := range []string{"prod", DevEnv} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "drop_in_config."+env+".yaml"), []byte(cfg), 0644))
	}
	t.Chdir(dir)

	_, err := LoadWithEnv("prod")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "devMode")

	loaded, err := LoadWithEnv(DevEnv)
	require.NoError(t, err)
	require.NotNil(t, loaded.DevMode)
	assert.Equal(t, "agent@example.com", loaded.DevMode.AdminEmail)
}

func TestShiftTimes(t *testing.T) {
	cfg := &Config{
		ShiftStartTime: "19:30",
		ShiftEndTime:   "21:30",
		Roles:          validRoles(),
	}

	// GMT date: London is UTC+0
	start, end, err := cfg.ShiftTimes("2026-01-12")
	require.NoError(t, err)
	assert.Equal(t, "2026-01-12T19:30:00Z", start.UTC().Format(time.RFC3339))
	assert.Equal(t, "2026-01-12T21:30:00Z", end.UTC().Format(time.RFC3339))

	// BST date: London is UTC+1
	start, end, err = cfg.ShiftTimes("2026-07-13")
	require.NoError(t, err)
	assert.Equal(t, "2026-07-13T18:30:00Z", start.UTC().Format(time.RFC3339))
	assert.Equal(t, "2026-07-13T20:30:00Z", end.UTC().Format(time.RFC3339))

	// Explicit timezone override
	cfg.ShiftTimezone = "UTC"
	start, _, err = cfg.ShiftTimes("2026-07-13")
	require.NoError(t, err)
	assert.Equal(t, "2026-07-13T19:30:00Z", start.UTC().Format(time.RFC3339))

	// Invalid date
	_, _, err = cfg.ShiftTimes("13/07/2026")
	assert.Error(t, err)
}

func TestRotaOverride_NilShiftSize(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "nil_shiftsize.yaml")

	configWithNilShiftSize := `
volunteerSheetID: "sheet123"
serviceVolunteersTab: "Volunteers"
rotaSheetID: "rota456"
databaseURL: "postgres://localhost:5432/test"
gmailUserID: "user@example.com"
maxAllocationFrequency: 0.25
defaultShiftSize: 2
shiftStartTime: "19:30"
shiftEndTime: "21:30"
requiresMale: true
roles:
  - name: "Team lead"
    max: 1
    priority: 1
  - name: "Service volunteer"
    priority: 2
rotaOverrides:
  - rrule: "FREQ=WEEKLY;BYDAY=SU"
    preallocations:
      - custom: "John Doe"
        role: "Service volunteer"
`

	err := os.WriteFile(configPath, []byte(configWithNilShiftSize), 0644)
	require.NoError(t, err)

	cfg, err := LoadFromPath(configPath)
	require.NoError(t, err)

	require.Len(t, cfg.RotaOverrides, 1)
	assert.Nil(t, cfg.RotaOverrides[0].ShiftSize)
}
