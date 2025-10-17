package db

import (
	"fmt"

	"github.com/jakechorley/ilford-drop-in/pkg/sheetssql"
)

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
