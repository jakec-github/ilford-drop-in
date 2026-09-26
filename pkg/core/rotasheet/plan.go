package rotasheet

import "slices"

// OpKind is one structural edit to Latest.
type OpKind string

const (
	InsertColumns OpKind = "insertColumns"
	DeleteColumns OpKind = "deleteColumns"
	MoveColumns   OpKind = "moveColumns"
	MoveRows      OpKind = "moveRows"
)

// Op is a structural edit, in the Sheets API's terms: 0-based indices into the
// sheet as it stands when the op runs, after every op before it. A move's To is
// where the moved range starts, counted before it is lifted out — which is how
// the API's MoveDimension counts it.
type Op struct {
	Kind  OpKind
	Index int
	Count int
	To    int
}

// Plan is what a publish does to Latest.
type Plan struct {
	// Archive copies Latest to a tab of its own first, because it holds a rota
	// that is about to stop being the latest one.
	Archive bool
	// Rebuild clears Latest and writes Sheet's FullValues, discarding whatever
	// is there. When false, Ops are applied and then the owned cells written.
	Rebuild bool
	Ops     []Op
	Sheet   Sheet
}

// PlanEdits works out what to do to Latest, given what the app last published
// there (nil when it has no record) and the rota as it now is.
//
// A different rota, no record at all, or a record a failed publish has made
// untrustworthy, is treated as a new rota: Latest is archived and rebuilt. The
// archive keeps whatever people typed; editing from a wrong record would
// misalign it. The same rota is edited in place, so that everything people typed
// into the columns the app does not own stays beside the shift it was typed
// against:
//
//   - a group gone, or narrower, has its columns deleted
//   - groups out of order are moved, contents and all
//   - a group new, or wider, has columns inserted at its end
//   - rows are moved to follow their shift
func PlanEdits(last *Layout, next Sheet) Plan {
	if last == nil || last.Stale || last.RotaID != next.Layout.RotaID {
		return Plan{Archive: true, Rebuild: true, Sheet: next}
	}
	if !sameMembers(last.ShiftIDs, next.Layout.ShiftIDs) {
		// An allocated rota never gains or loses a shift, so the record and
		// the rota disagreeing means something is wrong with one of them.
		// Rebuilding loses the unowned cells but leaves the sheet right.
		return Plan{Rebuild: true, Sheet: next}
	}

	ops := planColumns(last.Groups, next.Layout.Groups)
	ops = append(ops, planRows(last.ShiftIDs, next.Layout.ShiftIDs)...)
	return Plan{Ops: ops, Sheet: next}
}

func planColumns(old, next []Group) []Op {
	want := make(map[string]int, len(next))
	for _, g := range next {
		want[g.Key] = g.Width
	}

	var ops []Op
	work := slices.Clone(old)

	// Narrow first, working right to left, so nothing moved or inserted later
	// has to account for columns about to go.
	for i := len(work) - 1; i >= 0; i-- {
		w := want[work[i].Key]
		switch {
		case w == 0:
			ops = append(ops, Op{Kind: DeleteColumns, Index: groupStart(work, i), Count: work[i].Width})
			work = slices.Delete(work, i, i+1)
		case w < work[i].Width:
			ops = append(ops, Op{Kind: DeleteColumns, Index: groupStart(work, i) + w, Count: work[i].Width - w})
			work[i].Width = w
		}
	}

	// Then place each group in turn, left to right. Everything left of pos is
	// already where it belongs, so a group found further right only ever moves
	// left.
	pos := 0
	for _, g := range next {
		if g.Width == 0 {
			continue
		}
		j := slices.IndexFunc(work, func(w Group) bool { return w.Key == g.Key })
		if j == -1 {
			ops = append(ops, Op{Kind: InsertColumns, Index: groupStart(work, pos), Count: g.Width})
			work = slices.Insert(work, pos, Group{Key: g.Key, Width: g.Width})
			pos++
			continue
		}
		if j != pos {
			ops = append(ops, Op{Kind: MoveColumns, Index: groupStart(work, j), Count: work[j].Width, To: groupStart(work, pos)})
			moved := work[j]
			work = slices.Delete(work, j, j+1)
			work = slices.Insert(work, pos, moved)
		}
		if g.Width > work[pos].Width {
			ops = append(ops, Op{Kind: InsertColumns, Index: groupStart(work, pos) + work[pos].Width, Count: g.Width - work[pos].Width})
			work[pos].Width = g.Width
		}
		pos++
	}

	return ops
}

func planRows(old, next []string) []Op {
	var ops []Op
	work := slices.Clone(old)
	for i, id := range next {
		j := slices.Index(work, id)
		if j == i {
			continue
		}
		ops = append(ops, Op{Kind: MoveRows, Index: FirstDataRow + j, Count: 1, To: FirstDataRow + i})
		work = slices.Delete(work, j, j+1)
		work = slices.Insert(work, i, id)
	}
	return ops
}

// groupStart is the column a group starts at: column A is the date, and the
// groups follow it.
func groupStart(groups []Group, i int) int {
	start := 1
	for _, g := range groups[:i] {
		start += g.Width
	}
	return start
}

func sameMembers(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sa, sb := slices.Clone(a), slices.Clone(b)
	slices.Sort(sa)
	slices.Sort(sb)
	return slices.Equal(sa, sb)
}
