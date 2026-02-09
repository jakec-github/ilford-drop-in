package postgres

import (
	"context"
	"fmt"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// GetAvailabilityRequests retrieves all availability request records, filtering out duplicates.
// For duplicate records (same ID), only the record with form_sent=true is returned.
// If no form_sent=true record exists, the form_sent=false record is returned.
// Returns an error if multiple records exist with the same ID and same form_sent value.
func (d *DB) GetAvailabilityRequests(ctx context.Context) ([]db.AvailabilityRequest, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, rota_id, shift_date, volunteer_id, form_id, form_url, form_sent
		FROM availability_request
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query availability requests: %w", err)
	}
	defer rows.Close()

	// Track records by ID and form_sent state to detect integrity violations
	type recordState struct {
		formSentFalse *db.AvailabilityRequest
		formSentTrue  *db.AvailabilityRequest
	}
	stateMap := make(map[string]*recordState)

	for rows.Next() {
		var req db.AvailabilityRequest
		if err := rows.Scan(&req.ID, &req.RotaID, &req.ShiftDate, &req.VolunteerID, &req.FormID, &req.FormURL, &req.FormSent); err != nil {
			return nil, fmt.Errorf("failed to scan availability request: %w", err)
		}

		state, exists := stateMap[req.ID]
		if !exists {
			state = &recordState{}
			stateMap[req.ID] = state
		}

		if req.FormSent {
			if state.formSentTrue != nil {
				return nil, fmt.Errorf(
					"data integrity violation: multiple records found with ID=%s and form_sent=true (rota_id=%s, shift_date=%s, volunteer_id=%s)",
					req.ID, req.RotaID, req.ShiftDate, req.VolunteerID,
				)
			}
			r := req // copy to avoid loop variable issues
			state.formSentTrue = &r
		} else {
			if state.formSentFalse != nil {
				return nil, fmt.Errorf(
					"data integrity violation: multiple records found with ID=%s and form_sent=false (rota_id=%s, shift_date=%s, volunteer_id=%s)",
					req.ID, req.RotaID, req.ShiftDate, req.VolunteerID,
				)
			}
			r := req
			state.formSentFalse = &r
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating availability requests: %w", err)
	}

	// Prefer form_sent=true over form_sent=false
	result := make([]db.AvailabilityRequest, 0, len(stateMap))
	for _, state := range stateMap {
		if state.formSentTrue != nil {
			result = append(result, *state.formSentTrue)
		} else if state.formSentFalse != nil {
			result = append(result, *state.formSentFalse)
		}
	}

	return result, nil
}

// InsertAvailabilityRequests inserts multiple availability request records in a batch
func (d *DB) InsertAvailabilityRequests(requests []db.AvailabilityRequest) error {
	if len(requests) == 0 {
		return nil
	}

	ctx := context.Background()
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, req := range requests {
		_, err := tx.Exec(ctx, `
			INSERT INTO availability_request (id, rota_id, shift_date, volunteer_id, form_id, form_url, form_sent)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, req.ID, req.RotaID, req.ShiftDate, req.VolunteerID, req.FormID, req.FormURL, req.FormSent)
		if err != nil {
			return fmt.Errorf("failed to insert availability request: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
