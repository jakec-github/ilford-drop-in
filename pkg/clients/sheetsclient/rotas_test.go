package sheetsclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateTabTitle(t *testing.T) {
	tests := []struct {
		name      string
		startDate string
		endDate   string
		want      string
		wantErr   bool
	}{
		{
			name:      "single shift",
			startDate: "2025-01-05",
			endDate:   "2025-01-05",
			want:      "Jan 05 - Jan 05",
			wantErr:   false,
		},
		{
			name:      "multiple shifts",
			startDate: "2025-08-24",
			endDate:   "2025-11-09",
			want:      "Aug 24 - Nov 09",
			wantErr:   false,
		},
		{
			name:      "two shifts",
			startDate: "2025-01-05",
			endDate:   "2025-01-12",
			want:      "Jan 05 - Jan 12",
			wantErr:   false,
		},
		{
			name:      "invalid start date",
			startDate: "invalid",
			endDate:   "2025-01-05",
			want:      "",
			wantErr:   true,
		},
		{
			name:      "invalid end date",
			startDate: "2025-01-05",
			endDate:   "invalid",
			want:      "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateTabTitle(tt.startDate, tt.endDate)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// The published sheet's layout rule, stated once here (issue #185): a column
// group per configured Role in priority order, each as wide as the fullest
// shift needs and never narrower than one, then unknown-Role columns only if
// somebody is in one, then the two hand-typed columns.
func TestRotaValuesGivesEveryRoleItsOwnColumns(t *testing.T) {
	rota := &PublishedRota{
		RoleNames: []string{"Team lead", "Service volunteer", "Food collector"},
		Rows: []PublishedRotaRow{
			{
				Date:  "Mon Jul 13 2026",
				Roles: map[string][]string{"Team lead": {"Alice"}, "Service volunteer": {"Bob", "Carla"}},
			},
			{
				Date:  "Mon Jul 20 2026",
				Roles: map[string][]string{"Service volunteer": {"Dev"}},
			},
		},
	}

	values := rotaValues(rota)

	require.Len(t, values, 5, "two blank rows, a header, and one row per shift")
	assert.Equal(t, []interface{}{}, values[0])
	assert.Equal(t, []interface{}{}, values[1])
	assert.Equal(t, []interface{}{
		"Date",
		"Team lead",
		"Service volunteer 1", "Service volunteer 2",
		"Food collector",
		"Hot food", "Collection",
	}, values[2], "a Role filled twice takes two numbered columns; one nobody filled still takes one")
	assert.Equal(t, []interface{}{
		"Mon Jul 13 2026", "Alice", "Bob", "Carla", "", "", "",
	}, values[3])
	assert.Equal(t, []interface{}{
		"Mon Jul 20 2026", "", "Dev", "", "", "", "",
	}, values[4], "one person per cell, so an unfilled Seat is an empty cell rather than a shuffle")
}

// Somebody whose Role the app cannot name still worked the shift, so they are
// published — under a heading that says exactly that, and only when there is
// somebody to put there.
func TestRotaValuesPublishesUnknownRolesInColumnsOfTheirOwn(t *testing.T) {
	rota := &PublishedRota{
		RoleNames: []string{"Service volunteer"},
		Rows: []PublishedRotaRow{
			{
				Date:        "Mon Jul 13 2026",
				Roles:       map[string][]string{"Service volunteer": {"Bob"}},
				UnknownRole: []string{"Erin", "[St John's team]"},
			},
			{Date: "Mon Jul 20 2026", Roles: map[string][]string{"Service volunteer": {"Dev"}}},
		},
	}

	values := rotaValues(rota)

	assert.Equal(t, []interface{}{
		"Date", "Service volunteer",
		"Unknown role 1", "Unknown role 2",
		"Hot food", "Collection",
	}, values[2])
	assert.Equal(t, []interface{}{
		"Mon Jul 13 2026", "Bob", "Erin", "[St John's team]", "", "",
	}, values[3])

	nobodyUnknown := &PublishedRota{
		RoleNames: []string{"Service volunteer"},
		Rows: []PublishedRotaRow{
			{Date: "Mon Jul 13 2026", Roles: map[string][]string{"Service volunteer": {"Bob"}}},
		},
	}
	assert.Equal(t,
		[]interface{}{"Date", "Service volunteer", "Hot food", "Collection"},
		rotaValues(nobodyUnknown)[2],
		"a rota where every Role is known has no unknown-role columns at all")
}

// A closed shift says so in the first cell after the date, whatever column that
// turns out to be, and says it once.
func TestRotaValuesAnnouncesAClosedShiftOnce(t *testing.T) {
	rota := &PublishedRota{
		RoleNames: []string{"Team lead", "Service volunteer"},
		Rows: []PublishedRotaRow{
			{Date: "Mon Jul 13 2026", Closed: true},
			{Date: "Mon Jul 20 2026", Roles: map[string][]string{"Service volunteer": {"Bob", "Carla"}}},
		},
	}

	values := rotaValues(rota)

	assert.Equal(t, []interface{}{
		"Mon Jul 13 2026", "CLOSED", "", "", "", "",
	}, values[3])
}

// A rota of nothing but closed shifts still has a column to say so in, which is
// why a Role nobody filled keeps one.
func TestRotaValuesLeavesAColumnForAnEntirelyClosedRota(t *testing.T) {
	rota := &PublishedRota{
		RoleNames: []string{"Service volunteer"},
		Rows:      []PublishedRotaRow{{Date: "Mon Jul 13 2026", Closed: true}},
	}

	values := rotaValues(rota)

	assert.Equal(t, []interface{}{"Date", "Service volunteer", "Hot food", "Collection"}, values[2])
	assert.Equal(t, []interface{}{"Mon Jul 13 2026", "CLOSED", "", ""}, values[3])
}
