package db

import (
	"context"
	"fmt"
)

// InsertCover inserts a new cover record
func (d *DB) InsertCover(ctx context.Context, cover *Cover) error {
	_, err := d.pool.Exec(ctx, `
		INSERT INTO cover (id, reason, user_email)
		VALUES ($1, $2, $3)
	`, cover.ID, cover.Reason, cover.UserEmail)
	if err != nil {
		return fmt.Errorf("failed to insert cover: %w", err)
	}
	return nil
}
