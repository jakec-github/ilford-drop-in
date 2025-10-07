package db

import (
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

// InsertRotation inserts a new rotation record
func (db *DB) InsertRotation(rotation *Rotation) error {
	if err := sheetssql.InsertModel(db.ssql, *rotation); err != nil {
		return fmt.Errorf("failed to insert rotation: %w", err)
	}
	return nil
}
