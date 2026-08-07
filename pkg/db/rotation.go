package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrRotaAllocated reports that a write was refused because the Rotation has
// already been allocated. Named here rather than checked by the caller for the
// reason ErrShiftDateTaken is: the refusal is decided inside the transaction
// that holds the rota's row lock, so no read outside it can stand in for it.
var ErrRotaAllocated = errors.New("rota has already been allocated")

// RotaInFlight is the rota being worked on: the one Rotation that has not been
// allocated yet, together with how much of an availability round has grown
// around it.
//
// There is at most one, and that is a rule rather than an observation —
// services.DefineRota refuses to mint a second (issue #139). It is what lets
// every screen downstream address "the rota" without a picker.
//
// The round counts are here because they are what a discard destroys, and an
// admin is entitled to know that before agreeing to it. They are counts rather
// than the round itself: the round proper is a read of its own, needing the
// roster to say who anybody is, and this one must answer on a screen that has
// no round to show.
type RotaInFlight struct {
	Rotation
	// Asked is how many volunteers hold a link for this rota, Sent how many
	// have been emailed theirs, and Replied how many have answered at least
	// once. Sent and Replied are volunteers, not emails or submissions: a
	// resend and a resubmission are the same person twice.
	Asked   int
	Sent    int
	Replied int
}

// GetRotations retrieves all rotation records. Start, end, and shift count are
// derived from each rotation's shifts (ADR 0001): the stored rotation columns
// are write-only until the contract-phase migration drops them. Relies on the
// invariant that a rotation always has at least one shift.
func (d *DB) GetRotations(ctx context.Context) ([]Rotation, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT r.id, MIN(`+shiftDateExpr+`), MAX(`+shiftDateExpr+`), COUNT(*), r.allocated_datetime, r.inputs_changed_at
		FROM rotation r
		JOIN shift s ON s.rota_id = r.id
		GROUP BY r.id
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query rotations: %w", err)
	}
	defer rows.Close()

	var rotations []Rotation
	for rows.Next() {
		var r Rotation
		var start, end time.Time
		var allocatedDatetime, inputsChangedAt *time.Time
		if err := rows.Scan(&r.ID, &start, &end, &r.ShiftCount, &allocatedDatetime, &inputsChangedAt); err != nil {
			return nil, fmt.Errorf("failed to scan rotation: %w", err)
		}
		r.Start = start.Format("2006-01-02")
		r.End = end.Format("2006-01-02")
		if allocatedDatetime != nil {
			r.AllocatedDatetime = allocatedDatetime.UTC().Format(time.RFC3339)
		}
		if inputsChangedAt != nil {
			r.InputsChangedAt = inputsChangedAt.UTC()
		}
		rotations = append(rotations, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rotations: %w", err)
	}

	return rotations, nil
}

// GetRotaInFlight retrieves the unallocated Rotation and the size of its round,
// or nil when every rota has been allocated — which is the ordinary state
// between one rota going out and the next being defined, not an error.
//
// LIMIT 1 over an ordering rather than an assertion that only one row matches.
// The one-rota-in-flight rule is enforced where rotas are minted, and a
// deployment that predates it, or one whose rows were edited by hand, is better
// served by being shown the earliest unallocated rota — the one that has to be
// dealt with first — than by a screen that refuses to load.
func (d *DB) GetRotaInFlight(ctx context.Context) (*RotaInFlight, error) {
	var inFlight RotaInFlight
	var start, end time.Time
	var inputsChangedAt *time.Time
	err := d.pool.QueryRow(ctx, `
		SELECT r.id, MIN(`+shiftDateExpr+`), MAX(`+shiftDateExpr+`), COUNT(*), r.inputs_changed_at
		FROM rotation r
		JOIN shift s ON s.rota_id = r.id
		WHERE r.allocated_datetime IS NULL
		GROUP BY r.id
		ORDER BY MIN(`+shiftDateExpr+`)
		LIMIT 1
	`).Scan(&inFlight.ID, &start, &end, &inFlight.ShiftCount, &inputsChangedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query the rota in flight: %w", err)
	}
	inFlight.Start = start.Format("2006-01-02")
	inFlight.End = end.Format("2006-01-02")
	if inputsChangedAt != nil {
		inFlight.InputsChangedAt = inputsChangedAt.UTC()
	}

	err = d.pool.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE req.sent_at IS NOT NULL),
			COUNT(*) FILTER (WHERE EXISTS (
				SELECT 1 FROM availability_response res
				WHERE res.availability_request_id = req.id
			))
		FROM availability_request req
		WHERE req.rota_id = $1
	`, inFlight.ID).Scan(&inFlight.Asked, &inFlight.Sent, &inFlight.Replied)
	if err != nil {
		return nil, fmt.Errorf("failed to count the round for rota %s: %w", inFlight.ID, err)
	}

	return &inFlight, nil
}

// Marking a Rotation as having had its inputs move is what makes a Draft Rota
// Allocation dirty (issue #142, ADR 0008): the draft carries the stamp it read
// when it was solved, and a Rotation whose stamp has moved on since has had
// something change that the draft has not seen.
//
// It happens here, in the write itself, rather than at the call site above it.
// Every one of these writes is already declared an allocator input somewhere in
// this package — it is why closing a Shift freezes at allocation and editing its
// times does not — so the stamp belongs beside the write, inside whatever
// transaction the write is running in. A caller cannot forget it, and a new
// caller of an existing write inherits it.
//
// Allocated Rotations are left alone throughout. They have no draft to make
// stale, and stamping one would say something about a rota that has been
// decided.

// markRotaInputsChanged stamps one Rotation, named directly.
func markRotaInputsChanged(ctx context.Context, q querier, rotaID string) error {
	if _, err := q.Exec(ctx, `
		UPDATE rotation SET inputs_changed_at = now()
		WHERE id = $1 AND allocated_datetime IS NULL
	`, rotaID); err != nil {
		return fmt.Errorf("failed to mark the inputs of rotation %s as changed: %w", rotaID, err)
	}
	return nil
}

// markRotaInputsChangedForShift stamps the Rotation a Shift belongs to. The
// Shift is the sole authority on which rota it is part of (ADR 0001), so a
// per-Shift write never has to be told.
func markRotaInputsChangedForShift(ctx context.Context, q querier, shiftID string) error {
	if _, err := q.Exec(ctx, `
		UPDATE rotation SET inputs_changed_at = now()
		WHERE allocated_datetime IS NULL
		  AND id = (SELECT rota_id FROM shift WHERE id = $1)
	`, shiftID); err != nil {
		return fmt.Errorf("failed to mark the inputs of the rota holding shift %s as changed: %w", shiftID, err)
	}
	return nil
}

// markAllRotaInputsChanged stamps every unallocated Rotation, for the inputs
// that belong to no single rota — the Roles and the Allocation Settings, which
// are how the whole drop-in runs rather than facts about one rota. There is at
// most one unallocated Rotation anyway (issue #139); the statement does not rely
// on that.
func markAllRotaInputsChanged(ctx context.Context, q querier) error {
	if _, err := q.Exec(ctx, `
		UPDATE rotation SET inputs_changed_at = now()
		WHERE allocated_datetime IS NULL
	`); err != nil {
		return fmt.Errorf("failed to mark the inputs of the rota in flight as changed: %w", err)
	}
	return nil
}

// discardStatements empty a Rotation of everything that hangs off it, in the
// order the foreign keys allow, ending with the Rotation itself. Each takes the
// rota id as $1.
//
// Everything a discard touches is listed here, and everything it must not touch
// is left to the foreign keys. There is no DELETE for allocations, covers or
// alterations: those only exist for a rota that has been allocated, which is a
// rota this never runs against — so if one is ever present, the shift delete
// fails, the transaction rolls back and nothing is lost. A silent cascade there
// would be the one bug worth being loud about.
//
// Three tables are absent for the opposite reason, each declared ON DELETE
// CASCADE by the migration that added it, and each because the row is part of
// something this deletes rather than something that outlives it:
// shift_requirement (a Shape belongs to its Shift), draft_allocation (a draft
// Seat likewise), and draft_rota_allocation (a Draft Rota Allocation belongs to
// its Rotation). `allocation` deliberately does not cascade — those rows are the
// record of a rota that ran.
var discardStatements = []string{
	`DELETE FROM shift_availability
	 WHERE response_id IN (
		SELECT res.id
		FROM availability_response res
		JOIN availability_request req ON req.id = res.availability_request_id
		WHERE req.rota_id = $1
	 )`,
	`DELETE FROM availability_response
	 WHERE availability_request_id IN (
		SELECT id FROM availability_request WHERE rota_id = $1
	 )`,
	`DELETE FROM availability_request WHERE rota_id = $1`,
	`DELETE FROM preallocation
	 WHERE shift_id IN (SELECT id FROM shift WHERE rota_id = $1)`,
	`DELETE FROM shift WHERE rota_id = $1`,
	`DELETE FROM rotation WHERE id = $1`,
}

// DiscardRota destroys an unallocated Rotation and everything hanging off it —
// its Shifts, their Shapes, its Preallocations, its Draft Rota Allocation, its
// availability round and every response to it — in one transaction. It reports
// whether a rotation with that id existed, and ErrRotaAllocated when it did but
// has been allocated.
//
// One transaction is the whole point: a rota half-discarded is worse than either
// state it lies between — shifts with no rotation, or a rotation whose round
// still hands out links to dates nobody will staff.
//
// The row lock is taken before anything is read, so the allocated check cannot
// be overtaken by an allocation landing between the check and the deletes. It is
// the same lock InsertAllocationsAndSetAllocated takes (issue #8), which is what
// makes the two mutually exclusive rather than merely unlikely to collide.
func (d *DB) DiscardRota(ctx context.Context, rotaID string) (bool, error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var allocatedDatetime *time.Time
	err = tx.QueryRow(ctx, `
		SELECT allocated_datetime FROM rotation WHERE id = $1 FOR UPDATE
	`, rotaID).Scan(&allocatedDatetime)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to lock rotation %s: %w", rotaID, err)
	}
	if allocatedDatetime != nil {
		return false, ErrRotaAllocated
	}

	for _, stmt := range discardStatements {
		if _, err := tx.Exec(ctx, stmt, rotaID); err != nil {
			return false, fmt.Errorf("failed to discard rota %s: %w", rotaID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return true, nil
}
