package db

import (
	"context"
	"fmt"
)

// InsertCoverAndAlterations inserts a cover record and its associated
// alterations in a single transaction.
func (d *DB) InsertCoverAndAlterations(ctx context.Context, cover *Cover, alterations []Alteration) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO cover (id, reason, user_email)
		VALUES ($1, $2, $3)
	`, cover.ID, cover.Reason, cover.UserEmail)
	if err != nil {
		return fmt.Errorf("failed to insert cover: %w", err)
	}

	for _, a := range alterations {
		var volunteerID, customValue, role *string
		if a.VolunteerID != "" {
			volunteerID = &a.VolunteerID
		}
		if a.CustomValue != "" {
			customValue = &a.CustomValue
		}
		if a.Role != "" {
			role = &a.Role
		}

		_, err := tx.Exec(ctx, `
			INSERT INTO alteration (id, shift_date, rota_id, direction, volunteer_id, custom_value, cover_id, role)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, a.ID, a.ShiftDate, a.RotaID, a.Direction, volunteerID, customValue, a.CoverID, role)
		if err != nil {
			return fmt.Errorf("failed to insert alteration: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
