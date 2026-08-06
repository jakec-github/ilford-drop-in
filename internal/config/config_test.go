package config

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func intPtr(i int) *int { return &i }

// minimalConfigYAML is every required field and nothing else — the smallest
// file LoadFromPath accepts. Tests that care about one addition append it.
const minimalConfigYAML = `
volunteerSheetID: "sheet123"
serviceVolunteersTab: "Volunteers"
rotaSheetID: "rota456"
databaseURL: "postgres://localhost:5432/test"
gmailUserID: "user@example.com"
maxAllocationFrequency: 0.25
requiresMale: true
`

// baseConfig is a valid config the role tests vary one field of.
func baseConfig() *Config {
	return &Config{
		VolunteerSheetID:       "sheet123",
		ServiceVolunteersTab:   "Volunteers",
		RotaSheetID:            "rota456",
		DatabaseURL:            "postgres://localhost:5432/test",
		GmailUserID:            "user@example.com",
		MaxAllocationFrequency: 0.25,
	}
}

func TestValidate_ValidConfig(t *testing.T) {
	cfg := &Config{
		VolunteerSheetID:       "sheet123",
		ServiceVolunteersTab:   "Volunteers",
		RotaSheetID:            "rota456",
		DatabaseURL:            "postgres://localhost:5432/test",
		GmailUserID:            "user@example.com",
		GmailSender:            "sender@example.com",
		MaxAllocationFrequency: 0.25,
		RotaOverrides: []RotaOverride{
			{RRule: "FREQ=WEEKLY;BYDAY=SU"},
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
		RotaOverrides: []RotaOverride{
			{
				RRule: "INVALID_RRULE_SYNTAX",
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
		RotaOverrides: []RotaOverride{
			{
				RRule: "",
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
requiresMale: true
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
	// Neither the `preallocations:` block in that file nor its `shiftSize:`
	// configures anything now — Config Preallocations were deleted in issue #131
	// and the shift size in #129 — and an unknown key is warned about rather
	// than rejected, so the rest of the file still loads.
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
requiresMale: true
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

	err := os.WriteFile(configPath, []byte(minimalConfigYAML), 0644)
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

func TestLoadFromPath_EmptyFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configPath, nil, 0644))

	_, err := LoadFromPath(configPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is empty")
}

// A key the struct does not know must not stop the app starting: the same file
// is read by whatever build is deployed, so a key one version lacks — during a
// rollback, or on a branch cut either side of a rename — would otherwise be an
// outage. It is warned about by name, because the other way to get one is a key
// that used to configure something and now configures nothing.
func TestLoadFromPath_UnknownKey(t *testing.T) {
	tests := []struct {
		name    string
		extra   string
		wantKey string
	}{
		{
			name:    "top level",
			extra:   "customPreallocations:\n  - 'OC Church'\n",
			wantKey: "customPreallocations",
		},
		{
			name:    "inside a rota override",
			extra:   "rotaOverrides:\n  - rrule: 'FREQ=WEEKLY'\n    preallocatedTeamLeadID: 'V1'\n",
			wantKey: "preallocatedTeamLeadID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(configPath, []byte(minimalConfigYAML+tt.extra), 0644))

			logged := captureWarnings(t)

			cfg, err := LoadFromPath(configPath)
			require.NoError(t, err)
			// The keys either side of the unknown one still configure what they say.
			assert.Equal(t, "sheet123", cfg.VolunteerSheetID)

			assert.Contains(t, logged.String(), tt.wantKey)
		})
	}
}

// A config still carrying the `roles:` key it used to configure Roles with must
// load, because the file outlives the build reading it: the deployed config is
// edited on its own schedule, and a rollback puts a build that never knew the
// key in front of one that has it. It configures nothing now (ADR 0006), so it
// is warned about — which is the only way an operator finds out their Roles are
// coming from somewhere else.
func TestLoadFromPath_RolesKeyIsIgnoredNotRejected(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	legacyRoles := `roles:
  - name: "Team lead"
    max: 1
    priority: 1
    colour: "violet"
  - name: "Service volunteer"
    priority: 2
`
	require.NoError(t, os.WriteFile(configPath, []byte(minimalConfigYAML+legacyRoles), 0644))

	logged := captureWarnings(t)

	cfg, err := LoadFromPath(configPath)
	require.NoError(t, err)
	assert.Equal(t, "sheet123", cfg.VolunteerSheetID)
	assert.Contains(t, logged.String(), "roles")
}

// Same for the shift times. They were required keys until ticket #128 and are
// Rota Defaults now (ADR 0006), so every deployed config still has them on the
// day the build that ignores them lands — and rejecting the file would take the
// site down at exactly the moment the settings had yet to be filled in.
func TestLoadFromPath_ShiftTimeKeysAreIgnoredNotRejected(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	legacyTimes := `shiftStartTime: "19:30"
shiftEndTime: "21:30"
shiftTimezone: "Europe/London"
`
	require.NoError(t, os.WriteFile(configPath, []byte(minimalConfigYAML+legacyTimes), 0644))

	logged := captureWarnings(t)

	cfg, err := LoadFromPath(configPath)
	require.NoError(t, err)
	assert.Equal(t, "sheet123", cfg.VolunteerSheetID)
	for _, key := range []string{"shiftStartTime", "shiftEndTime", "shiftTimezone"} {
		assert.Contains(t, logged.String(), key)
	}
}

// The known keys of a valid config are warned about by nobody — a warning that
// fires on every start is a warning nobody reads.
func TestLoadFromPath_NoWarningsForAKnownConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(minimalConfigYAML), 0644))

	logged := captureWarnings(t)

	_, err := LoadFromPath(configPath)
	require.NoError(t, err)
	assert.Empty(t, logged.String())
}

// The line number is the useful half of an unknown key in a long file, so it
// survives being lifted out of yaml's message.
func TestUnknownKeys_NamesTheKeyAndItsLine(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	body := minimalConfigYAML + "databaseSheetID: 'legacy'\n"
	require.NoError(t, os.WriteFile(configPath, []byte(body), 0644))

	line := strings.Count(body[:strings.Index(body, "databaseSheetID")], "\n") + 1
	assert.Equal(t, []string{fmt.Sprintf("databaseSheetID (line %d)", line)}, UnknownKeys(configPath))
}

func TestUnknownKeys_NoneForAKnownConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(minimalConfigYAML), 0644))

	assert.Empty(t, UnknownKeys(configPath))
}

// captureWarnings redirects the default slog logger into a buffer for the
// duration of one test, since the unknown-key warning is the whole diagnostic
// left once the load no longer fails.
func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	return &buf
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
requiresMale: true
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

func TestValidate_ServerConfig(t *testing.T) {
	base := Config{
		VolunteerSheetID:       "sheet123",
		ServiceVolunteersTab:   "Volunteers",
		RotaSheetID:            "rota456",
		DatabaseURL:            "postgres://localhost:5432/test",
		GmailUserID:            "user@example.com",
		MaxAllocationFrequency: 0.25,
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
requiresMale: true
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

// `defaultShiftSize` and the `shiftSize` inside a rota override were how big a
// Shift was until ticket #129. What a Shift asks for is the default Shape now,
// stated Role by Role in the Rota Defaults (ADR 0006) — so both keys configure
// nothing, and both have to be ignored rather than rejected: every deployed
// config still carries them on the day the build that drops them lands.
func TestLoadFromPath_ShiftSizeKeysAreIgnoredNotRejected(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	legacySizes := `defaultShiftSize: 4
rotaOverrides:
  - rrule: "FREQ=WEEKLY;BYDAY=SU"
    shiftSize: 5
`
	require.NoError(t, os.WriteFile(configPath, []byte(minimalConfigYAML+legacySizes), 0644))

	logged := captureWarnings(t)

	cfg, err := LoadFromPath(configPath)
	require.NoError(t, err)
	assert.Equal(t, "sheet123", cfg.VolunteerSheetID)
	require.Len(t, cfg.RotaOverrides, 1)
	for _, key := range []string{"defaultShiftSize", "shiftSize"} {
		assert.Contains(t, logged.String(), key)
	}
}
