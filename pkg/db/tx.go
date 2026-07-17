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
type PreallocationTxStore interface {
	RotaAllocated(ctx context.Context, rotaID string) (bool, error)
	GetManualPreallocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]ManualPreallocation, error)
	InsertManualPreallocation(ctx context.Context, mp ManualPreallocation) error
	DeleteManualPreallocationByID(ctx context.Context, id string) (bool, error)
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

func (r *rotaTx) GetManualPreallocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]ManualPreallocation, error) {
	return getManualPreallocationsByShiftIDs(ctx, r.tx, shiftIDs)
}

func (r *rotaTx) InsertManualPreallocation(ctx context.Context, mp ManualPreallocation) error {
	return insertManualPreallocation(ctx, r.tx, mp)
}

func (r *rotaTx) DeleteManualPreallocationByID(ctx context.Context, id string) (bool, error) {
	return deleteManualPreallocationByID(ctx, r.tx, id)
}
