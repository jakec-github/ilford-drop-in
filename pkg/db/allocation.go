package db

import (
	"context"
	"fmt"

	"github.com/jakechorley/ilford-drop-in/pkg/sheetssql"
)

// GetAllocations retrieves all allocation records
func (db *DB) GetAllocations(ctx context.Context) ([]Allocation, error) {
	allocations, err := sheetssql.GetTableAs[Allocation](db.ssql, "allocation")
	if err != nil {
		return nil, fmt.Errorf("failed to get allocations: %w", err)
	}
	return allocations, nil
}

// InsertAllocations inserts allocation records into the database
func (db *DB) InsertAllocations(allocations []Allocation) error {
	if len(allocations) == 0 {
		return nil
	}

	if err := sheetssql.InsertModels(db.ssql, allocations); err != nil {
		return fmt.Errorf("failed to insert allocations: %w", err)
	}
	return nil
}
