package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/sheetssql"
)

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

// SetRotationAllocatedDatetime is a no-op for SheetsSQL (feature targets Postgres only)
func (db *DB) SetRotationAllocatedDatetime(ctx context.Context, rotaID string, datetime time.Time) error {
	return nil
}
