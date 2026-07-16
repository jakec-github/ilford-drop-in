package db

import (
	"context"
	"fmt"
)

// insertCoverAndAlterations inserts a cover record and its associated
// alterations. It is only reachable through WithRotaLock's transaction-bound
// store (issue #41, hazard H1): a rota change must be validated and inserted
// under the rotation-row lock, so no pool-level variant exists.
func insertCoverAndAlterations(ctx context.Context, q querier, cover *Cover, alterations []Alteration) error {
	_, err := q.Exec(ctx, `
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

		// Resolve the minted shift for this rota and date and store only its
		// reference; the shift is the sole authority on rota and date (ADR 0001).
		// A missing shift trips the NOT NULL constraint and fails loudly.
		_, err := q.Exec(ctx, `
			INSERT INTO alteration (id, direction, volunteer_id, custom_value, cover_id, role, shift_id)
			VALUES ($1, $2, $3, $4, $5, $6,
				(SELECT id FROM shift WHERE rota_id = $7 AND date = $8))
		`, a.ID, a.Direction, volunteerID, customValue, a.CoverID, role, a.RotaID, a.ShiftDate)
		if err != nil {
			return fmt.Errorf("failed to insert alteration: %w", err)
		}
	}

	return nil
}
