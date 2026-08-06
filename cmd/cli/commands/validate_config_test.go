package commands_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/cmd/cli/commands"
)

// prodConfigYAML is a valid prod-shaped config whose databaseURL points at a
// port nothing listens on. Every test here loads it, so a run that passes is
// also proof the command never opened the database it names.
const prodConfigYAML = `
volunteerSheetID: "sheet123"
serviceVolunteersTab: "Volunteers"
rotaSheetID: "rota456"
databaseURL: "postgres://nobody:nobody@127.0.0.1:1/unreachable?sslmode=disable"
gmailUserID: "user@example.com"
maxAllocationFrequency: 0.34
defaultShiftSize: 4
requiresMale: true
rotaOverrides:
  - rrule: "FREQ=MONTHLY;BYDAY=3SU"
    shiftSize: 6
`

// runValidateConfig executes the command the way main.go wires it: under a root
// that owns the persistent env flag.
func runValidateConfig(t *testing.T, args ...string) (string, error) {
	t.Helper()

	root := &cobra.Command{Use: "cli", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().StringP("env", "e", "", "Environment")
	root.AddCommand(commands.ValidateConfigCmd())

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"validate-config"}, args...))

	err := root.Execute()
	return out.String(), err
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "drop_in_config.prod.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0644))
	return path
}

func TestValidateConfigCmd_ValidConfig(t *testing.T) {
	path := writeConfig(t, prodConfigYAML)

	out, err := runValidateConfig(t, "-e", "prod", path)
	require.NoError(t, err)

	assert.Contains(t, out, path)
	// The counts are what the operator checks against the change they meant to
	// make — the failure this command exists for dropped a whole section
	// silently.
	assert.Contains(t, out, "1 rota override")
}

// `roles:` left config for the database in ticket #126, and a deployed file
// still carrying it must validate: the file outlives the build reading it. The
// summary is the only place an operator finds out the key stopped configuring
// anything.
func TestValidateConfigCmd_LegacyRolesKeyIsReportedNotRejected(t *testing.T) {
	path := writeConfig(t, prodConfigYAML+`roles:
  - name: "Team lead"
    max: 1
    priority: 1
`)

	out, err := runValidateConfig(t, "-e", "prod", path)
	require.NoError(t, err)

	assert.Contains(t, out, "1 unknown key")
	assert.Contains(t, out, "roles")
}

// An unknown key is reported, not rejected — it may be one another build knows.
// The summary is where the operator sees it, since nothing else will now stop.
func TestValidateConfigCmd_UnknownKey(t *testing.T) {
	path := writeConfig(t, prodConfigYAML+"preallocatedTeamLeadID: 'V1'\n")

	out, err := runValidateConfig(t, "-e", "prod", path)
	require.NoError(t, err)

	assert.Contains(t, out, "1 unknown key")
	assert.Contains(t, out, "preallocatedTeamLeadID")
	// The rest of the file still configured what it says.
	assert.Contains(t, out, "1 rota override")
}

// `preallocations:` under a rota override left config for the database in ticket
// #131 — the pins an admin makes every rota are Standing Preallocations now — so
// a deployed file still carrying one has to validate rather than fail. This is
// the case that matters most: it is what every existing deployment's config
// looks like on the day this ships.
func TestValidateConfigCmd_LegacyPreallocationsKeyIsReportedNotRejected(t *testing.T) {
	path := writeConfig(t, prodConfigYAML+"  - rrule: 'FREQ=WEEKLY'\n    preallocations:\n      - custom: 'X'\n        role: 'Service volunteer'\n")

	out, err := runValidateConfig(t, "-e", "prod", path)
	require.NoError(t, err)

	assert.Contains(t, out, "1 unknown key")
	assert.Contains(t, out, "preallocations")
	assert.Contains(t, out, "2 rota overrides")
}

// devMode is env-dependent, so the command has to know which environment the
// file is destined for rather than just parsing it.
func TestValidateConfigCmd_DevModeRejectedForProd(t *testing.T) {
	path := writeConfig(t, prodConfigYAML+`
server:
  port: 8080
  sessionSecret: "sixteen-characters-long"
  adminEmails:
    - "admin@example.com"
devMode:
  adminEmail: "admin@example.com"
  volunteersCSV: "test_data/volunteers.csv"
`)

	_, err := runValidateConfig(t, "-e", "prod", path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "devMode")
}

func TestValidateConfigCmd_RequiresEnv(t *testing.T) {
	path := writeConfig(t, prodConfigYAML)

	_, err := runValidateConfig(t, path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "env")
}

func TestValidateConfigCmd_MissingFile(t *testing.T) {
	_, err := runValidateConfig(t, "-e", "prod", filepath.Join(t.TempDir(), "absent.yaml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file")
}
