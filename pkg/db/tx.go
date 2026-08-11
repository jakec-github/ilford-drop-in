package db

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// querier is the subset of pgxpool.Pool and pgx.Tx that query helpers need,
// so the same query can run against the pool or inside a transaction.
type querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// inTx runs fn inside a transaction, committing when it returns nil and rolling
// back on any error.
//
// It is for the writes that are one statement plus the stamp that makes the rota
// in flight's draft stale (issue #142) — a Role, the Allocation Settings. They
// take no rota lock, because neither belongs to a rota; what they need is for
// the change and the stamp to land together, so that a draft can never be solved
// from settings nobody has recorded a change to.
func (d *DB) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// RotaChangeStore is the transaction-bound view of the store that WithRotaLock
// hands to its callback: every read and write issued through it runs inside
// the locking transaction, so a flow's validation and insert see one
// consistent snapshot of the locked rotas.
type RotaChangeStore interface {
	GetAllocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]Allocation, error)
	GetAlterationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]Alteration, error)
	InsertCoverAndAlterations(ctx context.Context, cover *Cover, alterations []Alteration) error
}

// WithRotaLock runs fn inside a transaction that first locks the given
// rotation rows with SELECT ... FOR UPDATE, deduplicated and in sorted order
// so two flows locking overlapping rota sets cannot deadlock. This is the
// same row lock InsertAllocationsAndSetAllocated takes (issue #8), so the
// callback is serialised against allocation of the locked rotas as well as
// against other WithRotaLock flows (issue #41, hazards H1 and H2). An error
// from fn rolls the whole transaction back.
func (d *DB) WithRotaLock(ctx context.Context, rotaIDs []string, fn func(store RotaChangeStore) error) error {
	return d.withRotaLockTx(ctx, rotaIDs, func(tx pgx.Tx) error {
		return fn(&rotaTx{tx: tx})
	})
}

// withRotaLockTx is the shared locking span behind WithRotaLock and
// WithRotaPreallocationLock: it begins a transaction, locks the given rotation
// rows FOR UPDATE (deduplicated, sorted, so overlapping lock sets cannot
// deadlock), runs fn against the raw transaction, and commits — or rolls the
// whole thing back on any error. Callers wrap tx in whatever store view their
// flow needs.
func (d *DB) withRotaLockTx(ctx context.Context, rotaIDs []string, fn func(tx pgx.Tx) error) error {
	ids := slices.Clone(rotaIDs)
	slices.Sort(ids)
	ids = slices.Compact(ids)

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, id := range ids {
		var locked string
		if err := tx.QueryRow(ctx, `SELECT id FROM rotation WHERE id = $1 FOR UPDATE`, id).Scan(&locked); err != nil {
			return fmt.Errorf("failed to lock rotation %s: %w", id, err)
		}
	}

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// PreallocationTxStore is the transaction-bound view WithRotaPreallocationLock
// hands its callback: reading the rota's allocation state, reading existing
// pins, and inserting or deleting a pin all run inside the same locking
// transaction, so the frozen-after-allocation guard and the duplicate-assignee
// checks validate against a snapshot that cannot change before the write lands
// (issue #39, mirroring the changeRota locking discipline).
//
// The Shift's Shape is read here for the same reason ShapeTxStore reads the
// pins: the Seats a Role has on that Shift are what say whether there is one
// left to promise (issue #185), and a Shape edit landing between the count and
// the insert is exactly what the lock exists to rule out.
type PreallocationTxStore interface {
	RotaAllocated(ctx context.Context, rotaID string) (bool, error)
	GetPreallocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]Preallocation, error)
	GetShiftShapes(ctx context.Context, shiftIDs []string) (map[string][]ShiftRequirement, error)
	InsertPreallocation(ctx context.Context, mp Preallocation) error
	DeletePreallocationByID(ctx context.Context, id string) (bool, error)
}

// WithRotaPreallocationLock runs fn under the same rotation-row lock as
// WithRotaLock (so preallocation mutations serialise against allocation and
// against each other), handing the callback a PreallocationTxStore bound to the
// locking transaction.
func (d *DB) WithRotaPreallocationLock(ctx context.Context, rotaIDs []string, fn func(store PreallocationTxStore) error) error {
	return d.withRotaLockTx(ctx, rotaIDs, func(tx pgx.Tx) error {
		return fn(&rotaTx{tx: tx})
	})
}

// ShiftTxStore is the transaction-bound view WithRotaShiftLock hands its
// callback. Closing or reopening a Shift is an allocator input, so it is frozen
// once the Rotation is allocated; reading that state and writing the flag under
// one rota-row lock is what stops a close landing against a rota that was
// allocated a moment earlier.
//
// Times are here for the opposite reason. They are not frozen — they are
// descriptive, not an allocator input (ADR 0007) — but a single edit may move
// both, and one transaction is what stops a rejected time change leaving a
// closure behind it committed.
type ShiftTxStore interface {
	RotaAllocated(ctx context.Context, rotaID string) (bool, error)
	SetShiftClosed(ctx context.Context, shiftID string, closed bool) (bool, error)
	SetShiftTimes(ctx context.Context, shiftID, startAt, endAt string) (bool, error)
}

// WithRotaShiftLock runs fn under the same rotation-row lock as WithRotaLock, so
// per-Shift edits serialise against allocation of the rota they belong to.
func (d *DB) WithRotaShiftLock(ctx context.Context, rotaIDs []string, fn func(store ShiftTxStore) error) error {
	return d.withRotaLockTx(ctx, rotaIDs, func(tx pgx.Tx) error {
		return fn(&rotaTx{tx: tx})
	})
}

// ShapeTxStore is the transaction-bound view WithRotaShapeLock hands its
// callback: everything editing one Shift's Shape has to see at once (issue
// #138).
//
// A Shape is an allocator input, so it is frozen once the Rotation is
// allocated — the solver filled Seats against it. The pins are here because
// they are the other thing a Shape has to agree with: a Role a Shift has no
// Seat for is an error the solver reports rather than a rota it can produce, so
// a Shape that would leave a pin without one is refused. Reading both and
// writing under one rota-row lock is what stops either check being overtaken by
// an allocation or a pin landing a moment later.
type ShapeTxStore interface {
	RotaAllocated(ctx context.Context, rotaID string) (bool, error)
	GetPreallocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]Preallocation, error)
	SetShiftShape(ctx context.Context, shiftID string, seats []ShiftRequirement) (bool, error)
}

// WithRotaShapeLock runs fn under the same rotation-row lock as WithRotaLock, so
// a Shape edit serialises against allocation of the rota it belongs to.
func (d *DB) WithRotaShapeLock(ctx context.Context, rotaIDs []string, fn func(store ShapeTxStore) error) error {
	return d.withRotaLockTx(ctx, rotaIDs, func(tx pgx.Tx) error {
		return fn(&rotaTx{tx: tx})
	})
}

// rotaTx implements RotaChangeStore against the locking transaction.
type rotaTx struct {
	tx pgx.Tx
}

func (r *rotaTx) GetAllocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]Allocation, error) {
	return getAllocationsByShiftIDs(ctx, r.tx, shiftIDs)
}

func (r *rotaTx) GetAlterationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]Alteration, error) {
	return getAlterationsByShiftIDs(ctx, r.tx, shiftIDs)
}

func (r *rotaTx) InsertCoverAndAlterations(ctx context.Context, cover *Cover, alterations []Alteration) error {
	return insertCoverAndAlterations(ctx, r.tx, cover, alterations)
}

func (r *rotaTx) RotaAllocated(ctx context.Context, rotaID string) (bool, error) {
	return rotaAllocated(ctx, r.tx, rotaID)
}

func (r *rotaTx) GetPreallocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]Preallocation, error) {
	return getPreallocationsByShiftIDs(ctx, r.tx, shiftIDs)
}

func (r *rotaTx) InsertPreallocation(ctx context.Context, mp Preallocation) error {
	return insertPreallocation(ctx, r.tx, mp)
}

func (r *rotaTx) DeletePreallocationByID(ctx context.Context, id string) (bool, error) {
	return deletePreallocationByID(ctx, r.tx, id)
}

func (r *rotaTx) SetShiftClosed(ctx context.Context, shiftID string, closed bool) (bool, error) {
	return setShiftClosed(ctx, r.tx, shiftID, closed)
}

func (r *rotaTx) SetShiftTimes(ctx context.Context, shiftID, startAt, endAt string) (bool, error) {
	return setShiftTimes(ctx, r.tx, shiftID, startAt, endAt)
}

func (r *rotaTx) GetShiftShapes(ctx context.Context, shiftIDs []string) (map[string][]ShiftRequirement, error) {
	return getShiftShapes(ctx, r.tx, shiftIDs)
}

func (r *rotaTx) SetShiftShape(ctx context.Context, shiftID string, seats []ShiftRequirement) (bool, error) {
	return setShiftShape(ctx, r.tx, shiftID, seats)
}
