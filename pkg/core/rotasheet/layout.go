// Package rotasheet lays the allocated rota out as the Latest tab of the rota
// sheet, and works out the edits that turn what was last published into what
// should be there now (issue #191).
//
// It does no I/O. What it answers is a list of sheet operations in the Sheets
// API's own terms — column and row indices — for sheetsclient to carry out, so
// every rule about how the sheet changes lives here, where it can be tested
// without Google.
package rotasheet

import "fmt"

// UnknownRoleKey is the column group for people whose allocation names a Role
// the app does not know, or none. Not a Role id, so it can never collide with
// one.
const UnknownRoleKey = "unknown-role"

const (
	unknownRoleHeading = "Unknown role"
	// closedCell is what a shift that did not run says in place of a name.
	closedCell = "CLOSED"
)

// UnownedHeadings are the columns after the app's own. The app writes their
// headers when it builds Latest from scratch and never touches their cells
// again: people type into them.
var UnownedHeadings = []string{"Hot food", "Collection"}

// HeaderRow is the 0-based sheet row holding the headers. Rows 1-2 are empty,
// as they always have been, and a shift's row follows the header.
const HeaderRow = 2

// FirstDataRow is the 0-based sheet row of the first shift.
const FirstDataRow = HeaderRow + 1

// Role is a Role as the sheet needs it: the id its columns are known by, and the
// name its header shows. Listed in priority order, which is column order.
type Role struct {
	ID   string
	Name string
}

// Shift is one row of the rota.
type Shift struct {
	ID string
	// Date is the row's first cell, already formatted for reading.
	Date   string
	Closed bool
	// Names are the people on the shift by Role id, or under UnknownRoleKey,
	// one per cell in the order given.
	Names map[string][]string
}

// Rota is the allocated rota to be published: its Roles in column order and its
// shifts in row order.
type Rota struct {
	ID     string
	Roles  []Role
	Shifts []Shift
}

// Group is one Role's run of columns, or the Unknown role's.
type Group struct {
	Key   string `json:"key"`
	Width int    `json:"width"`
	// Heading is the Role's name when the group was laid out. Not remembered
	// between publishes: a rename is a header rewrite, never a new group.
	Heading string `json:"-"`
}

// Layout is the structure of Latest as the app left it: which rota, which shift
// is on which row, and how many columns each group took. It is what the app
// remembers between publishes, and what the next publish is diffed against.
type Layout struct {
	RotaID   string   `json:"rotaId"`
	ShiftIDs []string `json:"shiftIds"`
	Groups   []Group  `json:"groups"`
}

// Sheet is a rota laid out: its structure, and the cells of the columns the app
// owns — Date, then every group — from the header row down.
type Sheet struct {
	Layout Layout
	Header []string
	Rows   [][]string
}

// OwnedWidth is how many columns, counting from A, belong to the app.
func (l Layout) OwnedWidth() int {
	width := 1
	for _, g := range l.Groups {
		width += g.Width
	}
	return width
}

// Lay lays a rota out.
//
// Every Role gets columns of its own, in priority order, as many as the rota's
// fullest shift needs of it and never fewer than one, so a rota of nothing but
// closed shifts still has somewhere to say so. The Unknown role gets columns
// only when somebody is in it.
func Lay(rota Rota) Sheet {
	groups := make([]Group, 0, len(rota.Roles)+1)
	for _, role := range rota.Roles {
		groups = append(groups, Group{Key: role.ID, Heading: role.Name, Width: max(1, widest(rota.Shifts, role.ID))})
	}
	if w := widest(rota.Shifts, UnknownRoleKey); w > 0 {
		groups = append(groups, Group{Key: UnknownRoleKey, Heading: unknownRoleHeading, Width: w})
	}

	header := []string{"Date"}
	for _, g := range groups {
		header = append(header, headings(g.Heading, g.Width)...)
	}

	shiftIDs := make([]string, 0, len(rota.Shifts))
	rows := make([][]string, 0, len(rota.Shifts))
	for _, shift := range rota.Shifts {
		shiftIDs = append(shiftIDs, shift.ID)
		row := []string{shift.Date}
		for _, g := range groups {
			names := shift.Names[g.Key]
			for i := 0; i < g.Width; i++ {
				switch {
				case shift.Closed && len(row) == 1:
					// The closure is announced in the row's first cell after
					// the date, whatever column that turns out to be.
					row = append(row, closedCell)
				case !shift.Closed && i < len(names):
					row = append(row, names[i])
				default:
					row = append(row, "")
				}
			}
		}
		rows = append(rows, row)
	}

	return Sheet{
		Layout: Layout{RotaID: rota.ID, ShiftIDs: shiftIDs, Groups: groups},
		Header: header,
		Rows:   rows,
	}
}

// FullValues is the whole of Latest built from scratch: two empty rows, the
// header, and a row per shift, with the unowned columns headed and empty.
func (s Sheet) FullValues() [][]string {
	values := [][]string{{}, {}, append(append([]string{}, s.Header...), UnownedHeadings...)}
	for _, row := range s.Rows {
		full := append([]string{}, row...)
		for range UnownedHeadings {
			full = append(full, "")
		}
		values = append(values, full)
	}
	return values
}

// OwnedValues is the app's own columns from the header row down: what every
// publish rewrites, whatever was typed over it.
func (s Sheet) OwnedValues() [][]string {
	return append([][]string{s.Header}, s.Rows...)
}

func widest(shifts []Shift, key string) int {
	w := 0
	for _, shift := range shifts {
		if !shift.Closed && len(shift.Names[key]) > w {
			w = len(shift.Names[key])
		}
	}
	return w
}

// headings names one group's columns: its own name when it is one column,
// numbered when there are several.
func headings(name string, width int) []string {
	if width == 1 {
		return []string{name}
	}
	out := make([]string, 0, width)
	for i := 0; i < width; i++ {
		out = append(out, fmt.Sprintf("%s %d", name, i+1))
	}
	return out
}
