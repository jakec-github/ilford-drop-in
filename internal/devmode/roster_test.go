package devmode

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

// repoCSV is the sample roster shipped with the repo — the file the dev config
// points at, so it is the fixture worth testing against.
const repoCSV = "../../test_data/volunteers.csv"

// twoRoles is the pair of Roles the sample roster ticks.
func twoRoles() model.Roles {
	max := 1
	return model.NewRoles([]model.Role{
		{Name: string(model.RoleTeamLead), Max: &max, Priority: 1},
		{Name: string(model.RoleVolunteer), Priority: 2},
	})
}

func TestLoadVolunteers_RepoSampleRoster(t *testing.T) {
	volunteers, err := LoadVolunteers(repoCSV, twoRoles())
	require.NoError(t, err)
	require.NotEmpty(t, volunteers)

	byID := make(map[string]model.Volunteer, len(volunteers))
	for _, v := range volunteers {
		byID[v.ID] = v
	}

	lead, ok := byID["XYZ"]
	require.True(t, ok, "expected the first data row to be loaded")
	assert.Equal(t, "Emma", lead.FirstName)
	assert.Equal(t, "Welder", lead.LastName)
	assert.Equal(t, []string{string(model.RoleTeamLead), string(model.RoleVolunteer)}, lead.Roles)
	assert.Equal(t, "Active", lead.Status)
	assert.Equal(t, "Female", lead.Gender)
	assert.Equal(t, "youremail+sarah.johnson@gmail.com", lead.Email)
	assert.Equal(t, "Group A", lead.GroupKey)

	// Display names are disambiguated across the whole roster, exactly as the
	// sheet path does: a unique first name stands alone, and the two Emmas —
	// Welder and Williams, so the initial does not separate them either — fall
	// back to full names.
	assert.Equal(t, "Michael", byID["ABC"].DisplayName)
	assert.Equal(t, "Emma Welder", byID["XYZ"].DisplayName)
	assert.Equal(t, "Emma Williams", byID["DEF"].DisplayName)
	// John Torres and John Edwards do separate on the initial.
	assert.Equal(t, "John T.", byID["LOP"].DisplayName)

	// A blank optional cell is empty, not the next column's value.
	assert.Empty(t, byID["GHI"].GroupKey)
}

func TestLoadVolunteers_MissingFile(t *testing.T) {
	_, err := LoadVolunteers(filepath.Join(t.TempDir(), "nope.csv"), twoRoles())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nope.csv")
}

func TestLoadVolunteers_MissingColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roster.csv")
	require.NoError(t, os.WriteFile(path, []byte(
		"Unique ID,First name,Last name,Role,Status,Sex/Gender,Email\nABC,Michael,Smith,Service volunteer,Active,Male,m@example.com\n",
	), 0644))

	_, err := LoadVolunteers(path, twoRoles())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Group key")
}

func TestLoadVolunteers_RaggedRows(t *testing.T) {
	// A hand-edited CSV routinely has short rows where trailing cells are
	// blank; the sheet path tolerates them, so this one must too.
	path := filepath.Join(t.TempDir(), "roster.csv")
	require.NoError(t, os.WriteFile(path, []byte(
		"Unique ID,First name,Last name,Role,Status,Sex/Gender,Email,Group key\nABC,Michael,Smith,Service volunteer,Active\n",
	), 0644))

	volunteers, err := LoadVolunteers(path, twoRoles())
	require.NoError(t, err)
	require.Len(t, volunteers, 1)
	assert.Equal(t, "Michael", volunteers[0].DisplayName)
	assert.Empty(t, volunteers[0].Email)
}

func TestLoadVolunteers_EmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roster.csv")
	require.NoError(t, os.WriteFile(path, nil, 0644))

	_, err := LoadVolunteers(path, twoRoles())
	require.Error(t, err)
}
