package db

import (
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
