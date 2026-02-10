package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// GetRotations retrieves all rotation records
func (d *DB) GetRotations(ctx context.Context) ([]db.Rotation, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, start, shift_count
		FROM rotation
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query rotations: %w", err)
	}
	defer rows.Close()

	var rotations []db.Rotation
	for rows.Next() {
		var r db.Rotation
		var start time.Time
		if err := rows.Scan(&r.ID, &start, &r.ShiftCount); err != nil {
			return nil, fmt.Errorf("failed to scan rotation: %w", err)
		}
		r.Start = start.Format("2006-01-02")
		rotations = append(rotations, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rotations: %w", err)
	}

	return rotations, nil
}

// InsertRotation inserts a new rotation record
func (d *DB) InsertRotation(rotation *db.Rotation) error {
	_, err := d.pool.Exec(context.Background(), `
		INSERT INTO rotation (id, start, shift_count)
		VALUES ($1, $2, $3)
	`, rotation.ID, rotation.Start, rotation.ShiftCount)
	if err != nil {
		return fmt.Errorf("failed to insert rotation: %w", err)
	}
	return nil
}
