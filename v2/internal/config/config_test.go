package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate_ValidConfig(t *testing.T) {
	shiftSize := 5
	cfg := &Config{
		VolunteerSheetID:       "sheet123",
		ServiceVolunteersTab:   "Volunteers",
		RotaSheetID:            "rota456",
		DatabaseSheetID:        "db789",
		GmailUserID:            "user@example.com",
		GmailSender:            "sender@example.com",
		MaxAllocationFrequency: 0.25,
		DefaultShiftSize:       2,
		RotaOverrides: []RotaOverride{
			{
				RRule:                "FREQ=WEEKLY;BYDAY=SU",
				CustomPreallocations: []string{"John Doe", "Jane Smith"},
				ShiftSize:            &shiftSize,
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
		DatabaseSheetID:        "db789",
		GmailUserID:            "user@example.com",
		MaxAllocationFrequency: 0.25,
		DefaultShiftSize:       2,
	}

	err := Validate(cfg)
	assert.NoError(t, err)
}

func TestValidate_MissingRequiredField(t *testing.T) {
	cfg := &Config{
		VolunteerSheetID:     "sheet123",
		ServiceVolunteersTab: "Volunteers",
		RotaSheetID:          "rota456",
		// Missing DatabaseSheetID
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
		DatabaseSheetID:        "db789",
		GmailUserID:            "user@example.com",
		MaxAllocationFrequency: 0.25,
		DefaultShiftSize:       2,
		RotaOverrides: []RotaOverride{
			{
				RRule:                "INVALID_RRULE_SYNTAX",
				CustomPreallocations: []string{"John Doe"},
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
		DatabaseSheetID:        "db789",
		GmailUserID:            "user@example.com",
		MaxAllocationFrequency: 0.25,
		DefaultShiftSize:       2,
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
		DatabaseSheetID:        "db789",
		GmailUserID:            "user@example.com",
		MaxAllocationFrequency: 0.25,
		DefaultShiftSize:       2,
		RotaOverrides: []RotaOverride{
			{
				RRule:                "",
				CustomPreallocations: []string{"John Doe"},
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
		DatabaseSheetID:        "db789",
		GmailUserID:            "user@example.com",
		MaxAllocationFrequency: 0.25,
		DefaultShiftSize:       2,
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
databaseSheetID: "db789"
gmailUserID: "user@example.com"
gmailSender: "sender@example.com"
maxAllocationFrequency: 0.25
defaultShiftSize: 2
rotaOverrides:
  - rrule: "FREQ=WEEKLY;BYDAY=SU"
    customPreallocations:
      - "John Doe"
      - "Jane Smith"
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
	assert.Equal(t, "db789", cfg.DatabaseSheetID)
	assert.Equal(t, "user@example.com", cfg.GmailUserID)
	assert.Equal(t, "sender@example.com", cfg.GmailSender)

	// Verify optional rotaOverrides
	require.Len(t, cfg.RotaOverrides, 1)
	override := cfg.RotaOverrides[0]
	assert.Equal(t, "FREQ=WEEKLY;BYDAY=SU", override.RRule)
	assert.Len(t, override.CustomPreallocations, 2)
	assert.Contains(t, override.CustomPreallocations, "John Doe")
	assert.Contains(t, override.CustomPreallocations, "Jane Smith")
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
databaseSheetID: "db789"
gmailUserID: "user@example.com"
maxAllocationFrequency: 0.25
defaultShiftSize: 2
rotaOverrides:
  - rrule: "INVALID_RRULE_SYNTAX"
    customPreallocations:
      - "John Doe"
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
databaseSheetID: "db789"
gmailUserID: "user@example.com"
maxAllocationFrequency: 0.25
defaultShiftSize: 2
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
# Missing databaseSheetID
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
databaseSheetID: "db789"
gmailUserID: "user@example.com"
maxAllocationFrequency: 0.25
defaultShiftSize: 2
rotaOverrides:
  - customPreallocations:
      - "John Doe"
    shiftSize: 5
`

	err := os.WriteFile(configPath, []byte(invalidOverride), 0644)
	require.NoError(t, err)

	_, err = LoadFromPath(configPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestRotaOverride_NilShiftSize(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "nil_shiftsize.yaml")

	configWithNilShiftSize := `
volunteerSheetID: "sheet123"
serviceVolunteersTab: "Volunteers"
rotaSheetID: "rota456"
databaseSheetID: "db789"
gmailUserID: "user@example.com"
maxAllocationFrequency: 0.25
defaultShiftSize: 2
rotaOverrides:
  - rrule: "FREQ=WEEKLY;BYDAY=SU"
    customPreallocations:
      - "John Doe"
`

	err := os.WriteFile(configPath, []byte(configWithNilShiftSize), 0644)
	require.NoError(t, err)

	cfg, err := LoadFromPath(configPath)
	require.NoError(t, err)

	require.Len(t, cfg.RotaOverrides, 1)
	assert.Nil(t, cfg.RotaOverrides[0].ShiftSize)
}
