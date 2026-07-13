package db

import (
	"context"
	"fmt"
	"time"
)

// GetRotations retrieves all rotation records
func (d *DB) GetRotations(ctx context.Context) ([]Rotation, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, start, shift_count, allocated_datetime
		FROM rotation
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query rotations: %w", err)
	}
	defer rows.Close()

	var rotations []Rotation
	for rows.Next() {
		var r Rotation
		var start time.Time
		var allocatedDatetime *time.Time
		if err := rows.Scan(&r.ID, &start, &r.ShiftCount, &allocatedDatetime); err != nil {
			return nil, fmt.Errorf("failed to scan rotation: %w", err)
		}
		r.Start = start.Format("2006-01-02")
		if allocatedDatetime != nil {
			r.AllocatedDatetime = allocatedDatetime.UTC().Format(time.RFC3339)
		}
		rotations = append(rotations, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rotations: %w", err)
	}

	return rotations, nil
}
