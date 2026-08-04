package sheetsclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

func intPtr(i int) *int { return &i }

// twoRoles is today's configuration: a capped Team lead ahead of an uncapped
// Service volunteer.
func twoRoles() model.Roles {
	return model.NewRoles([]model.Role{
		{Name: "Team lead", Max: intPtr(1), Priority: 1},
		{Name: "Service volunteer", Priority: 2},
	})
}

// row widens a slice of strings into the shape the Sheets API returns.
func row(cells ...string) []any {
	widened := make([]any, len(cells))
	for i, cell := range cells {
		widened[i] = cell
	}
	return widened
}

func TestParseVolunteers_RoleColumnsFoundBySuffix(t *testing.T) {
	raw := [][]any{
		row("Unique ID", "First name", "Last name", "Status", "Sex/Gender", "Email", "Group key", "Team lead - Role", "Service volunteer - Role"),
		row("XYZ", "Emma", "Welder", "Active", "Female", "emma@example.com", "Group A", "TRUE", "TRUE"),
		row("ABC", "Michael", "Smith", "Active", "Male", "michael@example.com", "", "FALSE", "TRUE"),
	}

	volunteers, err := ParseVolunteers(raw, twoRoles())
	require.NoError(t, err)
	require.Len(t, volunteers, 2)

	// Held Roles come back in priority order, so the first is the one a caller
	// showing a single Role wants.
	assert.Equal(t, []string{"Team lead", "Service volunteer"}, volunteers[0].Roles)
	assert.Equal(t, []string{"Service volunteer"}, volunteers[1].Roles)

	assert.True(t, volunteers[0].Holds("Team lead"))
	assert.False(t, volunteers[1].Holds("Team lead"))
	assert.True(t, volunteers[1].Holds("Service volunteer"))
}

// The legacy Role dropdown does not carry the suffix, so it is left alone and
// the two can sit side by side while the ticks are filled in.
func TestParseVolunteers_LegacyRoleColumnIsIgnored(t *testing.T) {
	raw := [][]any{
		row("Unique ID", "First name", "Last name", "Role", "Status", "Sex/Gender", "Email", "Group key", "Team lead - Role", "Service volunteer - Role"),
		row("XYZ", "Emma", "Welder", "Team lead", "Active", "Female", "emma@example.com", "", "", "TRUE"),
	}

	volunteers, err := ParseVolunteers(raw, twoRoles())
	require.NoError(t, err)
	require.Len(t, volunteers, 1)

	// The dropdown says Team lead; only the ticks count.
	assert.Equal(t, []string{"Service volunteer"}, volunteers[0].Roles)
}

func TestParseVolunteers_TickTruthiness(t *testing.T) {
	tests := []struct {
		cell string
		held bool
	}{
		{"TRUE", true},
		{"true", true},
		{" True ", true},
		{"yes", true},
		{"YES", true},
		{"✓", true},
		{"FALSE", false},
		{"false", false},
		{"no", false},
		{"", false},
		{"maybe", false},
	}

	for _, tc := range tests {
		t.Run(tc.cell, func(t *testing.T) {
			raw := [][]any{
				row("Unique ID", "First name", "Last name", "Status", "Sex/Gender", "Email", "Group key", "Team lead - Role"),
				row("XYZ", "Emma", "Welder", "Active", "Female", "emma@example.com", "", tc.cell),
			}

			volunteers, err := ParseVolunteers(raw, twoRoles())
			require.NoError(t, err)
			require.Len(t, volunteers, 1)
			assert.Equal(t, tc.held, volunteers[0].Holds("Team lead"))
		})
	}
}

// A ` - Role` column config does not name is a sheet the config has not caught
// up with: ignore it rather than failing the whole roster.
func TestParseVolunteers_UnknownRoleColumnIsIgnored(t *testing.T) {
	raw := [][]any{
		row("Unique ID", "First name", "Last name", "Status", "Sex/Gender", "Email", "Group key", "Service volunteer - Role", "Food collector - Role"),
		row("XYZ", "Emma", "Welder", "Active", "Female", "emma@example.com", "", "TRUE", "TRUE"),
	}

	volunteers, err := ParseVolunteers(raw, twoRoles())
	require.NoError(t, err)
	require.Len(t, volunteers, 1)
	assert.Equal(t, []string{"Service volunteer"}, volunteers[0].Roles)
}

// The other way round: a configured Role nobody can be ticked for. Nobody holds
// it, which is legal but worth a warning.
func TestParseVolunteers_ConfiguredRoleWithNoColumn(t *testing.T) {
	raw := [][]any{
		row("Unique ID", "First name", "Last name", "Status", "Sex/Gender", "Email", "Group key", "Service volunteer - Role"),
		row("XYZ", "Emma", "Welder", "Active", "Female", "emma@example.com", "", "TRUE"),
	}

	volunteers, err := ParseVolunteers(raw, twoRoles())
	require.NoError(t, err)
	require.Len(t, volunteers, 1)
	assert.Equal(t, []string{"Service volunteer"}, volunteers[0].Roles)
}

// A roster with no tick columns at all parses; everyone simply holds nothing.
// This is the state of the sheet the moment before the ticks are added, and
// failing on it would make the migration impossible to stage.
func TestParseVolunteers_NoRoleColumns(t *testing.T) {
	raw := [][]any{
		row("Unique ID", "First name", "Last name", "Role", "Status", "Sex/Gender", "Email", "Group key"),
		row("XYZ", "Emma", "Welder", "Team lead", "Active", "Female", "emma@example.com", ""),
	}

	volunteers, err := ParseVolunteers(raw, twoRoles())
	require.NoError(t, err)
	require.Len(t, volunteers, 1)
	assert.Empty(t, volunteers[0].Roles)
}

// The Group key column is a dropdown, and a dropdown cannot be unset: `None` is
// what someone picks when a volunteer leaves a group, and it means exactly what
// a blank cell means. The boundary is the only place that knows that, so the
// placeholder never reaches the domain.
func TestParseVolunteers_NoneGroupKeyIsNormalised(t *testing.T) {
	tests := []struct {
		cell     string
		groupKey string
	}{
		{"None", ""},
		{"none", ""},
		{" None ", ""},
		{"NONE", ""},
		{"", ""},
		{"Group A", "Group A"},
		{" Group A ", "Group A"},
		{"Nonesuch", "Nonesuch"},
	}

	for _, tc := range tests {
		t.Run(tc.cell, func(t *testing.T) {
			raw := [][]any{
				row("Unique ID", "First name", "Last name", "Status", "Sex/Gender", "Email", "Group key"),
				row("XYZ", "Emma", "Welder", "Active", "Female", "emma@example.com", tc.cell),
			}

			volunteers, err := ParseVolunteers(raw, twoRoles())
			require.NoError(t, err)
			require.Len(t, volunteers, 1)
			assert.Equal(t, tc.groupKey, volunteers[0].GroupKey)
		})
	}
}

// Two volunteers who both left their groups are two groups of one, not one
// group of two — the bug normalising at the boundary fixes.
func TestParseVolunteers_NoneIsNotAGroup(t *testing.T) {
	raw := [][]any{
		row("Unique ID", "First name", "Last name", "Status", "Sex/Gender", "Email", "Group key"),
		row("XYZ", "Emma", "Welder", "Active", "Female", "emma@example.com", "None"),
		row("ABC", "Michael", "Smith", "Active", "Male", "michael@example.com", "None"),
	}

	volunteers, err := ParseVolunteers(raw, twoRoles())
	require.NoError(t, err)
	require.Len(t, volunteers, 2)
	assert.Empty(t, volunteers[0].GroupKey)
	assert.Empty(t, volunteers[1].GroupKey)
}

func TestParseVolunteers_MissingRequiredColumn(t *testing.T) {
	raw := [][]any{
		row("Unique ID", "First name", "Last name", "Status", "Sex/Gender", "Email"),
		row("XYZ", "Emma", "Welder", "Active", "Female", "emma@example.com"),
	}

	_, err := ParseVolunteers(raw, twoRoles())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Group key")
}

// Short rows are routine in hand-edited exports; a missing tick cell is not a
// tick.
func TestParseVolunteers_RaggedRows(t *testing.T) {
	raw := [][]any{
		row("Unique ID", "First name", "Last name", "Status", "Sex/Gender", "Email", "Group key", "Team lead - Role", "Service volunteer - Role"),
		row("XYZ", "Emma", "Welder", "Active"),
	}

	volunteers, err := ParseVolunteers(raw, twoRoles())
	require.NoError(t, err)
	require.Len(t, volunteers, 1)
	assert.Empty(t, volunteers[0].Roles)
	assert.Empty(t, volunteers[0].Email)
}
