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

// GetAvailabilityRequests retrieves all availability request records
func (db *DB) GetAvailabilityRequests(ctx context.Context) ([]AvailabilityRequest, error) {
	requests, err := sheetssql.GetTableAs[AvailabilityRequest](db.ssql, "availability_request")
	if err != nil {
		return nil, fmt.Errorf("failed to get availability requests: %w", err)
	}
	return requests, nil
}
