package db

import (
	"context"
	"fmt"
)

// insertCoverAndAlterations inserts a cover record and its associated
// alterations. It is only reachable through WithRotaLock's transaction-bound
// store (issue #41, hazard H1): a rota change must be validated and inserted
// under the rotation-row lock, so no pool-level variant exists.
func insertCoverAndAlterations(ctx context.Context, q querier, organisationID string, cover *Cover, alterations []Alteration) error {
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
		if a.RoleID != "" {
			role = &a.RoleID
		}

		// The alteration references its shift directly; the shift is the sole
		// authority on rota and date (ADR 0001). It is selected from the Shift
		// rather than written as VALUES, so an unknown ShiftID — or another
		// Organisation's — inserts nothing and fails loudly.
		tag, err := q.Exec(ctx, `
			INSERT INTO alteration (id, direction, volunteer_id, custom_value, cover_id, role_id, shift_id)
			SELECT $1, $2, $3, $4, $5, $6, s.id
			FROM shift s
			WHERE s.id = $7 AND s.organisation_id = $8
		`, a.ID, a.Direction, volunteerID, customValue, a.CoverID, role, a.ShiftID, organisationID)
		if err != nil {
			return fmt.Errorf("failed to insert alteration: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("failed to insert alteration: no shift %s", a.ShiftID)
		}
	}

	return nil
}
