package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// GetPublishedRotaSheet reads what was last published to the rota sheet's Latest
// tab, as the JSON the publish stored, and whether a failed publish has since
// left it untrustworthy. Nil is a deployment that has never published, which is
// an ordinary answer rather than an error.
//
// Carried as JSON rather than decoded here: the layout is rotasheet's, and this
// package knows nothing of the domain packages.
func (d *DB) GetPublishedRotaSheet(ctx context.Context) (layout []byte, stale bool, err error) {
	err = d.pool.QueryRow(ctx, `SELECT layout, stale FROM published_rota_sheet`).Scan(&layout, &stale)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("failed to query published rota sheet: %w", err)
	}
	return layout, stale, nil
}

// SavePublishedRotaSheet records what a publish has just left on Latest,
// replacing the record of the one before.
func (d *DB) SavePublishedRotaSheet(ctx context.Context, rotaID string, layout []byte) error {
	_, err := d.pool.Exec(ctx, `
		INSERT INTO published_rota_sheet (singleton, rota_id, layout, stale, published_at)
		VALUES (TRUE, $1, $2, FALSE, now())
		ON CONFLICT (singleton) DO UPDATE
		SET rota_id = EXCLUDED.rota_id, layout = EXCLUDED.layout, stale = FALSE, published_at = EXCLUDED.published_at
	`, rotaID, layout)
	if err != nil {
		return fmt.Errorf("failed to save published rota sheet: %w", err)
	}
	return nil
}

// MarkPublishedRotaSheetStale records that a publish failed, so the layout on
// record may not be what Latest holds. No record is nothing to mark: the next
// publish rebuilds anyway.
func (d *DB) MarkPublishedRotaSheetStale(ctx context.Context) error {
	if _, err := d.pool.Exec(ctx, `UPDATE published_rota_sheet SET stale = TRUE`); err != nil {
		return fmt.Errorf("failed to mark published rota sheet stale: %w", err)
	}
	return nil
}
