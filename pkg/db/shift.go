package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// InsertRotationAndShifts inserts a rotation and all of its minted shifts in a
// single transaction, so a rotation can never exist without its shifts.
func (d *DB) InsertRotationAndShifts(ctx context.Context, rotation *Rotation, shifts []Shift) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO rotation (id, start, shift_count)
		VALUES ($1, $2, $3)
	`, rotation.ID, rotation.Start, rotation.ShiftCount)
	if err != nil {
		return fmt.Errorf("failed to insert rotation: %w", err)
	}

	batch := &pgx.Batch{}
	for _, s := range shifts {
		batch.Queue(`
			INSERT INTO shift (id, date, rota_id)
			VALUES ($1, $2, $3)
		`, s.ID, s.Date, s.RotaID)
	}
	results := tx.SendBatch(ctx, batch)
	for range shifts {
		if _, err := results.Exec(); err != nil {
			results.Close()
			return fmt.Errorf("failed to insert shift: %w", err)
		}
	}
	if err := results.Close(); err != nil {
		return fmt.Errorf("failed to close shift batch: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
