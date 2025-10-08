package db

import (
	"context"
	"fmt"

	"github.com/jakechorley/ilford-drop-in/pkg/sheetssql"
)

// DB provides database operations using SheetsSQL
type DB struct {
	ssql *sheetssql.DB
}

// NewDB creates a new database instance
func NewDB(ssql *sheetssql.DB) *DB {
	return &DB{
		ssql: ssql,
	}
}

// GetRotations retrieves all rotation records
func (db *DB) GetRotations(ctx context.Context) ([]Rotation, error) {
	rotations, err := sheetssql.GetTableAs[Rotation](db.ssql, "rotation")
	if err != nil {
		return nil, fmt.Errorf("failed to get rotations: %w", err)
	}
	return rotations, nil
}

// InsertRotation inserts a new rotation record
func (db *DB) InsertRotation(rotation *Rotation) error {
	if err := sheetssql.InsertModel(db.ssql, *rotation); err != nil {
		return fmt.Errorf("failed to insert rotation: %w", err)
	}
	return nil
}

// GetAvailabilityRequests retrieves all availability request records, filtering out duplicates.
// For duplicate records (same ID), only the record with form_sent=true is returned.
// If no form_sent=true record exists, the form_sent=false record is returned.
// Returns an error if multiple records exist with the same ID and same form_sent value (data integrity violation).
func (db *DB) GetAvailabilityRequests(ctx context.Context) ([]AvailabilityRequest, error) {
	allRequests, err := sheetssql.GetTableAs[AvailabilityRequest](db.ssql, "availability_request")
	if err != nil {
		return nil, fmt.Errorf("failed to get availability requests: %w", err)
	}

	// Track records by ID and form_sent state to detect integrity violations
	type recordState struct {
		formSentFalse *AvailabilityRequest
		formSentTrue  *AvailabilityRequest
	}
	stateMap := make(map[string]*recordState)

	// First pass: group by ID and validate no duplicates per form_sent state
	for i := range allRequests {
		req := &allRequests[i]

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
			state.formSentTrue = req
		} else {
			if state.formSentFalse != nil {
				return nil, fmt.Errorf(
					"data integrity violation: multiple records found with ID=%s and form_sent=false (rota_id=%s, shift_date=%s, volunteer_id=%s)",
					req.ID, req.RotaID, req.ShiftDate, req.VolunteerID,
				)
			}
			state.formSentFalse = req
		}
	}

	// Second pass: prefer form_sent=true over form_sent=false
	result := make([]AvailabilityRequest, 0, len(stateMap))
	for _, state := range stateMap {
		if state.formSentTrue != nil {
			result = append(result, *state.formSentTrue)
		} else if state.formSentFalse != nil {
			result = append(result, *state.formSentFalse)
		}
	}

	return result, nil
}
