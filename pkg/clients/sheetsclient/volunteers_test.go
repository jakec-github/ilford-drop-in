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

func TestParseVolunteers_RolesFromOneColumn(t *testing.T) {
	raw := [][]any{
		row("Unique ID", "First name", "Last name", "Status", "Sex/Gender", "Email", "Group key", "Roles"),
		row("XYZ", "Emma", "Welder", "Active", "Female", "emma@example.com", "Group A", "Team lead, Service volunteer"),
		row("ABC", "Michael", "Smith", "Active", "Male", "michael@example.com", "", "Service volunteer"),
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

// What a multi-select cell holds is a spreadsheet's idea of a list, not a
// parser's: the separator is whatever the UI wrote, the spacing is not
// guaranteed, and a hand-edited cell can hold anything a person typed.
func TestParseVolunteers_RolesCellShapes(t *testing.T) {
	tests := []struct {
		name  string
		cell  string
		roles []string
	}{
		{"empty", "", nil},
		{"blank", "   ", nil},
		{"one", "Team lead", []string{"Team lead"}},
		{"two", "Team lead, Service volunteer", []string{"Team lead", "Service volunteer"}},
		{"no spacing", "Team lead,Service volunteer", []string{"Team lead", "Service volunteer"}},
		{"loose spacing", "  Team lead ,  Service volunteer  ", []string{"Team lead", "Service volunteer"}},
		// Order in the cell is the order chips were picked; priority order is
		// what comes back either way.
		{"reversed", "Service volunteer, Team lead", []string{"Team lead", "Service volunteer"}},
		{"empty item", "Team lead, , Service volunteer", []string{"Team lead", "Service volunteer"}},
		{"trailing separator", "Team lead,", []string{"Team lead"}},
		{"repeated", "Team lead, Team lead", []string{"Team lead"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw := [][]any{
				row("Unique ID", "First name", "Last name", "Status", "Sex/Gender", "Email", "Group key", "Roles"),
				row("XYZ", "Emma", "Welder", "Active", "Female", "emma@example.com", "", tc.cell),
			}

			volunteers, err := ParseVolunteers(raw, twoRoles())
			require.NoError(t, err)
			require.Len(t, volunteers, 1)
			assert.Equal(t, tc.roles, volunteers[0].Roles)
		})
	}
}

// A value config does not name is a sheet the config has not caught up with —
// or a typo in a hand-edited cell. Skip it rather than failing the roster, and
// keep the rest of the cell.
func TestParseVolunteers_UnknownRoleValueIsSkipped(t *testing.T) {
	raw := [][]any{
		row("Unique ID", "First name", "Last name", "Status", "Sex/Gender", "Email", "Group key", "Roles"),
		row("XYZ", "Emma", "Welder", "Active", "Female", "emma@example.com", "", "Food collector, Service volunteer"),
	}

	volunteers, err := ParseVolunteers(raw, twoRoles())
	require.NoError(t, err)
	require.Len(t, volunteers, 1)
	assert.Equal(t, []string{"Service volunteer"}, volunteers[0].Roles)
}

// The columns Roles used to live in. Neither the pre-S1 `Role` dropdown nor the
// S1 `<name> - Role` ticks are read any more; leaving them on the sheet must
// change nothing.
func TestParseVolunteers_RetiredRoleColumnsAreIgnored(t *testing.T) {
	raw := [][]any{
		row("Unique ID", "First name", "Last name", "Role", "Status", "Sex/Gender", "Email", "Group key", "Roles", "Team lead - Role", "Service volunteer - Role"),
		row("XYZ", "Emma", "Welder", "Team lead", "Active", "Female", "emma@example.com", "", "Service volunteer", "TRUE", "TRUE"),
	}

	volunteers, err := ParseVolunteers(raw, twoRoles())
	require.NoError(t, err)
	require.Len(t, volunteers, 1)
	assert.Equal(t, []string{"Service volunteer"}, volunteers[0].Roles)
}

// Roles is a required column like any other: a roster without it would load
// with nobody holding anything and allocate nothing, which is worse than
// failing.
func TestParseVolunteers_MissingRolesColumn(t *testing.T) {
	raw := [][]any{
		row("Unique ID", "First name", "Last name", "Status", "Sex/Gender", "Email", "Group key"),
		row("XYZ", "Emma", "Welder", "Active", "Female", "emma@example.com", ""),
	}

	_, err := ParseVolunteers(raw, twoRoles())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Roles")
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
				row("Unique ID", "First name", "Last name", "Status", "Sex/Gender", "Email", "Group key", "Roles"),
				row("XYZ", "Emma", "Welder", "Active", "Female", "emma@example.com", tc.cell, "Service volunteer"),
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
		row("Unique ID", "First name", "Last name", "Status", "Sex/Gender", "Email", "Group key", "Roles"),
		row("XYZ", "Emma", "Welder", "Active", "Female", "emma@example.com", "None", "Service volunteer"),
		row("ABC", "Michael", "Smith", "Active", "Male", "michael@example.com", "None", "Service volunteer"),
	}

	volunteers, err := ParseVolunteers(raw, twoRoles())
	require.NoError(t, err)
	require.Len(t, volunteers, 2)
	assert.Empty(t, volunteers[0].GroupKey)
	assert.Empty(t, volunteers[1].GroupKey)
}

func TestParseVolunteers_MissingRequiredColumn(t *testing.T) {
	raw := [][]any{
		row("Unique ID", "First name", "Last name", "Status", "Sex/Gender", "Email", "Roles"),
		row("XYZ", "Emma", "Welder", "Active", "Female", "emma@example.com", "Service volunteer"),
	}

	_, err := ParseVolunteers(raw, twoRoles())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Group key")
}

// Short rows are routine in hand-edited exports; a row that stops before the
// Roles column holds no Roles.
func TestParseVolunteers_RaggedRows(t *testing.T) {
	raw := [][]any{
		row("Unique ID", "First name", "Last name", "Status", "Sex/Gender", "Email", "Group key", "Roles"),
		row("XYZ", "Emma", "Welder", "Active"),
	}

	volunteers, err := ParseVolunteers(raw, twoRoles())
	require.NoError(t, err)
	require.Len(t, volunteers, 1)
	assert.Empty(t, volunteers[0].Roles)
	assert.Empty(t, volunteers[0].Email)
}
