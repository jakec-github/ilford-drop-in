package rotasheet

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	food  = Role{ID: "role-food", Name: "Food collector"}
	names = []string{"Ann", "Ben", "Cal", "Dee", "Eli", "Flo"}
)

// staffed builds a shift with n people in each Role, keyed by Role id, so a
// test states the shape of a rota rather than spelling out every name.
func staffed(id string, counts map[string]int) Shift {
	s := Shift{ID: id, Date: "date " + id, Names: map[string][]string{}}
	for key, n := range counts {
		for i := 0; i < n; i++ {
			s.Names[key] = append(s.Names[key], id+" "+key+" "+names[i])
		}
	}
	return s
}

// grid is Latest as the Sheets API would leave it: rows of cells.
type grid [][]string

// publishedGrid is Latest after sheet was built from scratch and then typed
// into by people: Hot food and Collection filled in on every shift's row, and a
// column of their own added after Collection. Each typed cell names its shift,
// so a test can tell where it ended up.
func publishedGrid(sheet Sheet) grid {
	g := grid{}
	for _, row := range sheet.FullValues() {
		g = append(g, slices.Clone(row))
	}
	g[HeaderRow] = append(g[HeaderRow], "Notes")
	width := len(g[HeaderRow])
	for i, id := range sheet.Layout.ShiftIDs {
		row := g[FirstDataRow+i]
		owned := sheet.Layout.OwnedWidth()
		row[owned], row[owned+1] = "hot "+id, "collection "+id
		g[FirstDataRow+i] = append(row, "note "+id)
	}
	return g.padded(width)
}

func (g grid) padded(width int) grid {
	for i := range g {
		for len(g[i]) < width {
			g[i] = append(g[i], "")
		}
	}
	return g
}

// apply carries a plan out the way the Sheets API would: each op against the
// sheet as the ops before it left it, and a move's destination counted before
// the moved range is lifted out. Then the owned cells are written.
func (g grid) apply(t *testing.T, plan Plan) grid {
	t.Helper()
	require.False(t, plan.Rebuild, "an edit in place was expected")

	for _, op := range plan.Ops {
		switch op.Kind {
		case InsertColumns:
			for i := range g {
				g[i] = slices.Insert(g[i], op.Index, make([]string, op.Count)...)
			}
		case DeleteColumns:
			for i := range g {
				g[i] = slices.Delete(g[i], op.Index, op.Index+op.Count)
			}
		case MoveColumns:
			for i := range g {
				g[i] = move(g[i], op)
			}
		case MoveRows:
			g = move(g, op)
		default:
			t.Fatalf("unknown op %q", op.Kind)
		}
	}

	for r, row := range plan.Sheet.OwnedValues() {
		copy(g[HeaderRow+r], row)
	}
	return g
}

func move[T any](s []T, op Op) []T {
	moved := slices.Clone(s[op.Index : op.Index+op.Count])
	s = slices.Delete(s, op.Index, op.Index+op.Count)
	to := op.To
	if to > op.Index {
		to -= op.Count
	}
	return slices.Insert(s, to, moved...)
}

// assertPublished checks Latest holds next: the app's cells rewritten, and
// every typed cell still on its own shift's row, after the app's columns, in
// the order people left them.
func assertPublished(t *testing.T, g grid, next Sheet) {
	t.Helper()
	owned := next.Layout.OwnedWidth()

	assert.Equal(t, append(slices.Clone(next.Header), "Hot food", "Collection", "Notes"), g[HeaderRow], "header")
	require.Len(t, g, FirstDataRow+len(next.Rows), "one row per shift")
	for i, id := range next.Layout.ShiftIDs {
		row := g[FirstDataRow+i]
		assert.Equal(t, next.Rows[i], row[:owned], "owned cells of %s", id)
		assert.Equal(t, []string{"hot " + id, "collection " + id, "note " + id}, row[owned:], "typed cells of %s", id)
	}
}

func rota(roles []Role, shifts ...Shift) Sheet {
	return Lay(Rota{ID: "rota", Roles: roles, Shifts: shifts})
}

// Each case is a change to an allocated rota already on Latest. Whatever it
// is, the app's columns end up describing the rota as it now is, and what
// people typed stays beside the shift they typed it against.
func TestPlanEditsKeepsTypedCellsBesideTheirShift(t *testing.T) {
	tests := []struct {
		name   string
		before Sheet
		after  Sheet
	}{
		{
			name:   "nothing structural changes",
			before: rota([]Role{lead, service}, staffed("s1", map[string]int{lead.ID: 1, service.ID: 2}), staffed("s2", map[string]int{lead.ID: 1})),
			after:  rota([]Role{lead, service}, staffed("s1", map[string]int{lead.ID: 1, service.ID: 1}), staffed("s2", map[string]int{lead.ID: 1, service.ID: 2})),
		},
		{
			name:   "a Role needs another column",
			before: rota([]Role{lead, service}, staffed("s1", map[string]int{lead.ID: 1, service.ID: 1})),
			after:  rota([]Role{lead, service}, staffed("s1", map[string]int{lead.ID: 2, service.ID: 3})),
		},
		{
			name:   "a column is no longer needed",
			before: rota([]Role{lead, service}, staffed("s1", map[string]int{lead.ID: 2, service.ID: 3})),
			after:  rota([]Role{lead, service}, staffed("s1", map[string]int{lead.ID: 1, service.ID: 1})),
		},
		{
			name:   "somebody's Role becomes unknown",
			before: rota([]Role{lead}, staffed("s1", map[string]int{lead.ID: 2})),
			after:  rota([]Role{lead}, staffed("s1", map[string]int{lead.ID: 1, UnknownRoleKey: 1})),
		},
		{
			name:   "nobody's Role is unknown any more",
			before: rota([]Role{lead}, staffed("s1", map[string]int{lead.ID: 1, UnknownRoleKey: 2})),
			after:  rota([]Role{lead}, staffed("s1", map[string]int{lead.ID: 1})),
		},
		{
			name:   "a Role is renamed",
			before: rota([]Role{lead, service}, staffed("s1", map[string]int{lead.ID: 1, service.ID: 2})),
			after:  rota([]Role{{ID: lead.ID, Name: "Shift lead"}, service}, staffed("s1", map[string]int{lead.ID: 1, service.ID: 2})),
		},
		{
			name:   "a new Role is added between two others",
			before: rota([]Role{lead, service}, staffed("s1", map[string]int{lead.ID: 1, service.ID: 2})),
			after:  rota([]Role{lead, food, service}, staffed("s1", map[string]int{lead.ID: 1, service.ID: 2})),
		},
		{
			name:   "Roles are reordered",
			before: rota([]Role{lead, food, service}, staffed("s1", map[string]int{lead.ID: 1, food.ID: 2, service.ID: 3})),
			after:  rota([]Role{service, lead, food}, staffed("s1", map[string]int{lead.ID: 1, food.ID: 2, service.ID: 3})),
		},
		{
			name: "a shift's start moves it later in the rota",
			before: rota([]Role{lead},
				staffed("s1", map[string]int{lead.ID: 1}), staffed("s2", map[string]int{lead.ID: 1}), staffed("s3", map[string]int{lead.ID: 1})),
			after: rota([]Role{lead},
				staffed("s2", map[string]int{lead.ID: 1}), staffed("s3", map[string]int{lead.ID: 1}), staffed("s1", map[string]int{lead.ID: 1})),
		},
		{
			name: "a shift's start moves it earlier in the rota",
			before: rota([]Role{lead},
				staffed("s1", map[string]int{lead.ID: 1}), staffed("s2", map[string]int{lead.ID: 1}), staffed("s3", map[string]int{lead.ID: 1})),
			after: rota([]Role{lead},
				staffed("s3", map[string]int{lead.ID: 1}), staffed("s1", map[string]int{lead.ID: 1}), staffed("s2", map[string]int{lead.ID: 1})),
		},
		{
			name: "everything at once",
			before: rota([]Role{lead, food, service},
				staffed("s1", map[string]int{lead.ID: 1, food.ID: 2, service.ID: 1, UnknownRoleKey: 1}),
				staffed("s2", map[string]int{lead.ID: 2, service.ID: 3}),
				staffed("s3", map[string]int{food.ID: 1})),
			after: rota([]Role{service, {ID: lead.ID, Name: "Shift lead"}, food},
				staffed("s3", map[string]int{food.ID: 3, service.ID: 1}),
				staffed("s1", map[string]int{lead.ID: 1, service.ID: 4}),
				staffed("s2", map[string]int{lead.ID: 1})),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := tt.before.Layout
			plan := PlanEdits(&before, tt.after)

			assert.False(t, plan.Archive, "the same rota is never archived")
			assertPublished(t, publishedGrid(tt.before).apply(t, plan), tt.after)
		})
	}
}

// A rename is a header rewrite, and nothing moves: the group is known by the
// Role's id.
func TestPlanEditsTreatsARenameAsNoStructuralChange(t *testing.T) {
	before := rota([]Role{lead}, staffed("s1", map[string]int{lead.ID: 1}))
	after := rota([]Role{{ID: lead.ID, Name: "Shift lead"}}, staffed("s1", map[string]int{lead.ID: 1}))

	plan := PlanEdits(&before.Layout, after)

	assert.Empty(t, plan.Ops)
	assert.Equal(t, "Shift lead", plan.Sheet.Header[1])
}

// A new rota, or no record of what Latest holds, archives Latest and builds it
// again from scratch. So does a record a failed publish has left untrusted:
// editing from a layout the sheet may not have would misalign it.
func TestPlanEditsArchivesAndRebuilds(t *testing.T) {
	next := rota([]Role{lead}, staffed("s1", map[string]int{lead.ID: 1}))

	stale := next.Layout
	stale.Stale = true
	other := next.Layout
	other.RotaID = "an older rota"

	for name, last := range map[string]*Layout{
		"no record":           nil,
		"a different rota":    &other,
		"a record gone stale": &stale,
	} {
		t.Run(name, func(t *testing.T) {
			plan := PlanEdits(last, next)

			assert.True(t, plan.Archive)
			assert.True(t, plan.Rebuild)
			assert.Empty(t, plan.Ops)
		})
	}
}

// An allocated rota never gains or loses a shift, so a record disagreeing about
// which shifts it has cannot be edited from. Latest is rebuilt; it is not
// archived, because it holds this rota rather than an older one.
func TestPlanEditsRebuildsWhenTheShiftsDisagree(t *testing.T) {
	before := rota([]Role{lead}, staffed("s1", map[string]int{lead.ID: 1}), staffed("s2", map[string]int{lead.ID: 1}))
	after := rota([]Role{lead}, staffed("s1", map[string]int{lead.ID: 1}), staffed("s9", map[string]int{lead.ID: 1}))

	plan := PlanEdits(&before.Layout, after)

	assert.False(t, plan.Archive)
	assert.True(t, plan.Rebuild)
}
