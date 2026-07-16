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

	if err := fn(&rotaTx{tx: tx}); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
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
