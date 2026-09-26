package rotasheet

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	lead    = Role{ID: "role-lead", Name: "Team lead"}
	service = Role{ID: "role-service", Name: "Service volunteer"}
)

func shift(id, date string, names map[string][]string) Shift {
	return Shift{ID: id, Date: date, Names: names}
}

// Every Role gets columns of its own, in the order given, as many as the fullest
// shift needs — one person per cell.
func TestLayGivesEveryRoleItsOwnColumns(t *testing.T) {
	sheet := Lay(Rota{
		ID:    "rota",
		Roles: []Role{lead, service},
		Shifts: []Shift{
			shift("s1", "Sun Jan 04 2026", map[string][]string{lead.ID: {"Alice"}, service.ID: {"Bob", "Cat", "Dan"}}),
			shift("s2", "Sun Jan 11 2026", map[string][]string{lead.ID: {"Eve"}, service.ID: {"Fay"}}),
		},
	})

	assert.Equal(t, []string{"Date", "Team lead", "Service volunteer 1", "Service volunteer 2", "Service volunteer 3"}, sheet.Header)
	assert.Equal(t, [][]string{
		{"Sun Jan 04 2026", "Alice", "Bob", "Cat", "Dan"},
		{"Sun Jan 11 2026", "Eve", "Fay", "", ""},
	}, sheet.Rows)
	assert.Equal(t, Layout{
		RotaID:   "rota",
		ShiftIDs: []string{"s1", "s2"},
		Groups:   []Group{{Key: lead.ID, Width: 1}, {Key: service.ID, Width: 3}},
	}, sheet.Layout)
}

// Somebody whose Role the app cannot name still worked the shift: they get
// columns after every Role's, and only when there is somebody to put in them.
func TestLayPublishesUnknownRolesInColumnsOfTheirOwn(t *testing.T) {
	without := Lay(Rota{ID: "rota", Roles: []Role{lead}, Shifts: []Shift{
		shift("s1", "Sun Jan 04 2026", map[string][]string{lead.ID: {"Alice"}}),
	}})
	assert.Equal(t, []string{"Date", "Team lead"}, without.Header)

	with := Lay(Rota{ID: "rota", Roles: []Role{lead}, Shifts: []Shift{
		shift("s1", "Sun Jan 04 2026", map[string][]string{lead.ID: {"Alice"}, UnknownRoleKey: {"Zed", "Yan"}}),
	}})
	assert.Equal(t, []string{"Date", "Team lead", "Unknown role 1", "Unknown role 2"}, with.Header)
	assert.Equal(t, [][]string{{"Sun Jan 04 2026", "Alice", "Zed", "Yan"}}, with.Rows)
	assert.Equal(t, Group{Key: UnknownRoleKey, Width: 2}, with.Layout.Groups[1])
}

// A closed shift says so once, in the first cell after the date, and nothing
// else.
func TestLayAnnouncesAClosedShiftOnce(t *testing.T) {
	sheet := Lay(Rota{ID: "rota", Roles: []Role{lead, service}, Shifts: []Shift{
		shift("s1", "Sun Jan 04 2026", map[string][]string{lead.ID: {"Alice"}, service.ID: {"Bob", "Cat"}}),
		{ID: "s2", Date: "Sun Jan 11 2026", Closed: true},
	}})

	assert.Equal(t, []string{"Sun Jan 11 2026", "CLOSED", "", ""}, sheet.Rows[1])
}

// A rota of nothing but closed shifts still has a column for each Role, and so
// somewhere to say it is closed.
func TestLayLeavesAColumnForAnEntirelyClosedRota(t *testing.T) {
	sheet := Lay(Rota{ID: "rota", Roles: []Role{lead}, Shifts: []Shift{
		{ID: "s1", Date: "Sun Jan 04 2026", Closed: true},
	}})

	assert.Equal(t, []string{"Date", "Team lead"}, sheet.Header)
	assert.Equal(t, [][]string{{"Sun Jan 04 2026", "CLOSED"}}, sheet.Rows)
}

// Built from scratch, Latest has two empty rows, then the header with Hot food
// and Collection headed and left empty on every row.
func TestFullValuesHeadsTheUnownedColumns(t *testing.T) {
	sheet := Lay(Rota{ID: "rota", Roles: []Role{lead}, Shifts: []Shift{
		shift("s1", "Sun Jan 04 2026", map[string][]string{lead.ID: {"Alice"}}),
	}})

	assert.Equal(t, [][]string{
		{},
		{},
		{"Date", "Team lead", "Hot food", "Collection"},
		{"Sun Jan 04 2026", "Alice", "", ""},
	}, sheet.FullValues())
	assert.Equal(t, [][]string{
		{"Date", "Team lead"},
		{"Sun Jan 04 2026", "Alice"},
	}, sheet.OwnedValues())
}

// The layout is what the database keeps, and the names on the rota must never
// reach it: its JSON holds ids and widths, and no Role or volunteer name.
func TestLayoutJSONHoldsNoNames(t *testing.T) {
	sheet := Lay(Rota{ID: "rota", Roles: []Role{lead}, Shifts: []Shift{
		shift("s1", "Sun Jan 04 2026", map[string][]string{lead.ID: {"Alice"}, UnknownRoleKey: {"Zed"}}),
	}})
	sheet.Layout.Stale = true

	raw, err := json.Marshal(sheet.Layout)
	require.NoError(t, err)

	assert.JSONEq(t, `{
		"rotaId": "rota",
		"shiftIds": ["s1"],
		"groups": [{"key": "role-lead", "width": 1}, {"key": "unknown-role", "width": 1}]
	}`, string(raw))
}
