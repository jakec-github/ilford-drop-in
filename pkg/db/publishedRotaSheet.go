package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// GetPublishedRotaSheet reads what was last published to the rota sheet's Latest
// tab, as the JSON the publish stored. Nil is a deployment that has never
// published, which is an ordinary answer rather than an error.
//
// Carried as JSON rather than decoded here: the layout is rotasheet's, and this
// package knows nothing of the domain packages.
func (d *DB) GetPublishedRotaSheet(ctx context.Context) ([]byte, error) {
	var layout []byte
	err := d.pool.QueryRow(ctx, `SELECT layout FROM published_rota_sheet`).Scan(&layout)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query published rota sheet: %w", err)
	}
	return layout, nil
}

// SavePublishedRotaSheet records what a publish has just left on Latest,
// replacing the record of the one before.
func (d *DB) SavePublishedRotaSheet(ctx context.Context, rotaID string, layout []byte) error {
	_, err := d.pool.Exec(ctx, `
		INSERT INTO published_rota_sheet (singleton, rota_id, layout, published_at)
		VALUES (TRUE, $1, $2, now())
		ON CONFLICT (singleton) DO UPDATE
		SET rota_id = EXCLUDED.rota_id, layout = EXCLUDED.layout, published_at = EXCLUDED.published_at
	`, rotaID, layout)
	if err != nil {
		return fmt.Errorf("failed to save published rota sheet: %w", err)
	}
	return nil
}
