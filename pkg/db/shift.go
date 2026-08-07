package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// shiftTimestampLayout is how a Shift's start and end are spelled on this side
// of the boundary: a local date and time of day with nothing on the end saying
// which zone reads it, matching the TIMESTAMP without time zone the columns
// hold (ADR 0007).
const shiftTimestampLayout = "2006-01-02T15:04:05"

// shiftDateExpr is where a Shift's date comes from: the date of the evening it
// starts (ADR 0007). Written against a shift aliased `s`, so every query that
// wants a date — selecting it, ordering by it, bounding a range with it — spells
// it the same way and none of them can drift apart.
//
// There is nothing else it could come from: issue #135 dropped the stored copy,
// and the unique index enforcing one Shift per date is defined on this very
// expression. `start_at::date` is IMMUTABLE, which is what allows that; the
// timestamptz equivalent is only STABLE and could not be indexed.
const shiftDateExpr = "s.start_at::date"

// ErrShiftDateTaken reports that a write would have put two Shifts on one date.
// It is named for the same reason ErrDuplicateRoleName is: an admin moving a
// Shift onto a day the drop-in already runs has made an ordinary mistake and is
// told so, and reading the driver's error code is this package's job rather
// than every caller's.
var ErrShiftDateTaken = errors.New("another shift already starts on that date")

// isShiftDateTaken reports whether an error is the one-Shift-per-date index
// refusing a write. The index is named rather than any unique violation being
// assumed, so a later index on the table cannot quietly start reporting itself
// as a date clash.
func isShiftDateTaken(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == uniqueViolation &&
		pgErr.ConstraintName == "shift_start_date_key"
}

// localTimestamp renders a TIMESTAMP column in the layout this package spells
// wall-clock times in. Nothing about the value says which zone reads it, which
// is the point (ADR 0007).
func localTimestamp(t time.Time) string {
	return t.Format(shiftTimestampLayout)
}

// GetShiftsByRotaID retrieves a rotation's shifts, ordered by date ascending.
// Consumers that once recomputed a rota's dates by arithmetic read them here
// instead (ADR 0001).
func (d *DB) GetShiftsByRotaID(ctx context.Context, rotaID string) ([]Shift, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT s.id, `+shiftDateExpr+`, s.rota_id, s.closed, s.start_at, s.end_at
		FROM shift s
		WHERE s.rota_id = $1
		ORDER BY `+shiftDateExpr+`
	`, rotaID)
	if err != nil {
		return nil, fmt.Errorf("failed to query shifts for rota %s: %w", rotaID, err)
	}
	defer rows.Close()

	var shifts []Shift
	for rows.Next() {
		var s Shift
		var date, startAt, endAt time.Time
		if err := rows.Scan(&s.ID, &date, &s.RotaID, &s.Closed, &startAt, &endAt); err != nil {
			return nil, fmt.Errorf("failed to scan shift: %w", err)
		}
		s.Date = date.Format("2006-01-02")
		s.StartAt, s.EndAt = localTimestamp(startAt), localTimestamp(endAt)
		shifts = append(shifts, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating shifts: %w", err)
	}

	return shifts, nil
}

// ShiftInRange is a minted shift within a queried date range, carrying whether
// its rota has been allocated. Allocation is whole-rota today (derived from the
// rota's allocated_datetime), but the flag is exposed per shift to leave room
// for per-shift allocation later (ADR 0001).
type ShiftInRange struct {
	Shift
	Allocated bool
}

// GetShiftsInRange retrieves the minted shifts whose date falls between from and
// to (inclusive), allocated or not, ordered by date ascending. A zero time
// leaves that bound open, mirroring GetAllocationsInRange. Each shift carries
// its rota's allocated state, joined from rotation.allocated_datetime.
func (d *DB) GetShiftsInRange(ctx context.Context, from, to time.Time) ([]ShiftInRange, error) {
	where, args := shiftDateWhere(from, to)
	rows, err := d.pool.Query(ctx, `
		SELECT s.id, `+shiftDateExpr+`, s.rota_id, s.closed, s.start_at, s.end_at, r.allocated_datetime IS NOT NULL
		FROM shift s
		JOIN rotation r ON r.id = s.rota_id
	`+where+`
		ORDER BY `+shiftDateExpr+`
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query shifts in range: %w", err)
	}
	defer rows.Close()

	var shifts []ShiftInRange
	for rows.Next() {
		var s ShiftInRange
		var date, startAt, endAt time.Time
		if err := rows.Scan(&s.ID, &date, &s.RotaID, &s.Closed, &startAt, &endAt, &s.Allocated); err != nil {
			return nil, fmt.Errorf("failed to scan shift: %w", err)
		}
		s.Date = date.Format("2006-01-02")
		s.StartAt, s.EndAt = localTimestamp(startAt), localTimestamp(endAt)
		shifts = append(shifts, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating shifts: %w", err)
	}

	return shifts, nil
}

// shiftDateWhere builds a WHERE clause bounding the shift's date (aliased s),
// with zero times leaving the corresponding bound open. Its sole remaining user
// is GetShiftsInRange.
func shiftDateWhere(from, to time.Time) (string, []any) {
	var conds []string
	var args []any
	if !from.IsZero() {
		args = append(args, from)
		conds = append(conds, fmt.Sprintf("%s >= $%d", shiftDateExpr, len(args)))
	}
	if !to.IsZero() {
		args = append(args, to)
		conds = append(conds, fmt.Sprintf("%s <= $%d", shiftDateExpr, len(args)))
	}
	if len(conds) == 0 {
		return "", nil
	}
	return "WHERE " + strings.Join(conds, " AND "), args
}

// GetShiftByDate retrieves the single shift on the given date, or nil if no
// shift exists for that date. Dates are unique, so at most one row matches;
// this is the lookup that resolves a date to its shift and rota.
func (d *DB) GetShiftByDate(ctx context.Context, date time.Time) (*Shift, error) {
	var s Shift
	var d0, startAt, endAt time.Time
	err := d.pool.QueryRow(ctx, `
		SELECT s.id, `+shiftDateExpr+`, s.rota_id, s.closed, s.start_at, s.end_at
		FROM shift s
		WHERE `+shiftDateExpr+` = $1
	`, date).Scan(&s.ID, &d0, &s.RotaID, &s.Closed, &startAt, &endAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query shift for date %s: %w", date.Format("2006-01-02"), err)
	}
	s.Date = d0.Format("2006-01-02")
	s.StartAt, s.EndAt = localTimestamp(startAt), localTimestamp(endAt)
	return &s, nil
}

// GetShiftByID retrieves one shift with its rota's allocation state, or nil if
// no shift has that id. It is the read behind close/reopen: whether the shift
// exists and whether its rota has been allocated are the two things that decide
// whether the flag may move at all.
func (d *DB) GetShiftByID(ctx context.Context, id string) (*ShiftInRange, error) {
	var s ShiftInRange
	var date, startAt, endAt time.Time
	err := d.pool.QueryRow(ctx, `
		SELECT s.id, `+shiftDateExpr+`, s.rota_id, s.closed, s.start_at, s.end_at, r.allocated_datetime IS NOT NULL
		FROM shift s
		JOIN rotation r ON r.id = s.rota_id
		WHERE s.id = $1
	`, id).Scan(&s.ID, &date, &s.RotaID, &s.Closed, &startAt, &endAt, &s.Allocated)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query shift %s: %w", id, err)
	}
	s.Date = date.Format("2006-01-02")
	s.StartAt, s.EndAt = localTimestamp(startAt), localTimestamp(endAt)
	return &s, nil
}

// setShiftClosed writes a shift's closed flag, reporting whether a row matched.
// It carries no freeze check of its own: the caller holds the rota's row lock
// and has already established that the rota is unallocated.
//
// Whether the drop-in runs that day is an allocator input, so the rota's draft
// is stamped stale. Closing a Shift that was already closed stamps it too: the
// alternative is a read-before-write to find out, and the cost of being wrong
// is one re-solve.
func setShiftClosed(ctx context.Context, q querier, id string, closed bool) (bool, error) {
	tag, err := q.Exec(ctx, `UPDATE shift SET closed = $2 WHERE id = $1`, id, closed)
	if err != nil {
		return false, fmt.Errorf("failed to set closed on shift %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if err := markRotaInputsChangedForShift(ctx, q, id); err != nil {
		return false, err
	}
	return true, nil
}

// setShiftTimes writes a shift's start and end, reporting whether a row
// matched. Both together, because they are one statement of when the session
// runs and the database refuses half of one.
//
// Moving a Shift's start onto a day another Shift already starts on comes back
// as ErrShiftDateTaken. That is the one-Shift-per-date index answering, which
// makes it the whole guard rather than a backstop behind a read: a caller
// holding one rota's lock cannot see a clash coming from another rota.
//
// There is no freeze check here and none above: a Shift's times are descriptive
// rather than an allocator input, so they stay editable after the Rotation is
// allocated (ADR 0007).
//
// The day the Shift starts on is the exception, and it is the one thing here
// that stamps the rota's draft stale (issue #142). The solver is told dates and
// never times, so moving a session from 19:30 to 20:00 cannot change the rota it
// would produce, while moving it to the following Sunday changes which
// volunteers are free for it. The stamp goes first, while the old start is still
// there to compare against; if the write below is then refused, the whole
// transaction goes with it.
func setShiftTimes(ctx context.Context, q querier, id, startAt, endAt string) (bool, error) {
	if _, err := q.Exec(ctx, `
		UPDATE rotation SET inputs_changed_at = now()
		WHERE allocated_datetime IS NULL
		  AND id = (
			SELECT s.rota_id FROM shift s
			WHERE s.id = $1 AND `+shiftDateExpr+` IS DISTINCT FROM ($2::timestamp)::date
		  )
	`, id, startAt); err != nil {
		return false, fmt.Errorf("failed to mark the inputs of the rota holding shift %s as changed: %w", id, err)
	}

	tag, err := q.Exec(ctx, `
		UPDATE shift
		SET start_at = $2::timestamp, end_at = $3::timestamp
		WHERE id = $1
	`, id, startAt, endAt)
	if isShiftDateTaken(err) {
		return false, ErrShiftDateTaken
	}
	if err != nil {
		return false, fmt.Errorf("failed to set the times of shift %s: %w", id, err)
	}
	return tag.RowsAffected() > 0, nil
}

// InsertDefinedRota inserts a rotation, all of its minted shifts, the Shapes
// those shifts ask for and the Preallocations its Standing Preallocations
// seeded, in a single transaction — so a rotation can never exist without its
// shifts, a shift can never exist without the Shape it was minted with, and a
// rota can never be defined with only some of the pins an admin was promised.
//
// The Shapes arrive as their own argument rather than on each Shift because
// they are read on their own too (GetShiftShapes): most readers of a Shift do
// not care what it asks for, and one that carried a Shape only on the way in
// would be a struct whose field means something different in each direction.
//
// Concurrency (issue #41, hazard B1): the one-Shift-per-date unique index is
// what makes concurrent runs safe — two rotas minting the same date cannot both
// commit, and the losing transaction writes nothing. Any change that relaxes
// that index must introduce a replacement guard here. That refusal comes back
// as ErrShiftDateTaken, since it is also how a rota defined to start on a day
// the drop-in already runs is turned away.
func (d *DB) InsertDefinedRota(ctx context.Context, rotation *Rotation, shifts []Shift, preallocations []Preallocation, requirements []ShiftRequirement) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// The rotation row is now identity plus allocated_datetime; start and shift
	// count are derived from its shifts by GetRotations (ADR 0001).
	_, err = tx.Exec(ctx, `
		INSERT INTO rotation (id)
		VALUES ($1)
	`, rotation.ID)
	if err != nil {
		return fmt.Errorf("failed to insert rotation: %w", err)
	}

	batch := &pgx.Batch{}
	for _, s := range shifts {
		// Date is not written: it is derived from the start on the way out
		// (shiftDateExpr, ADR 0007), so the caller's job is to say when the
		// session runs and the date follows. An empty time is written as NULL
		// and refused by the NOT NULL, which is the loudest place for "nobody
		// said when this runs" to surface.
		batch.Queue(`
			INSERT INTO shift (id, rota_id, closed, start_at, end_at)
			VALUES ($1, $2, $3, NULLIF($4, '')::timestamp, NULLIF($5, '')::timestamp)
		`, s.ID, s.RotaID, s.Closed, s.StartAt, s.EndAt)
	}
	results := tx.SendBatch(ctx, batch)
	for range shifts {
		if _, err := results.Exec(); err != nil {
			results.Close()
			// A date this rota would have shared with one that already exists is
			// reported as itself. It became an ordinary mistake when the start
			// date became an admin's to state (issue #140): a rota begun a week
			// too early overlaps the last one, and "failed to insert shift"
			// would tell nobody that.
			if isShiftDateTaken(err) {
				return ErrShiftDateTaken
			}
			return fmt.Errorf("failed to insert shift: %w", err)
		}
	}
	if err := results.Close(); err != nil {
		return fmt.Errorf("failed to close shift batch: %w", err)
	}

	// A handful of Seats per Shift, so one at a time and named on failure for
	// the same reason the pins below are.
	if err := insertShiftRequirements(ctx, tx, requirements); err != nil {
		return err
	}

	// Written one at a time rather than batched: there are a handful at most,
	// and the shift_id foreign key means a mistake in the seeding is worth
	// naming the row it came from.
	for _, p := range preallocations {
		if err := insertPreallocation(ctx, tx, p); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
