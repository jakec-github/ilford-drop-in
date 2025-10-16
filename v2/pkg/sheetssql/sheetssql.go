package sheetssql

import (
	"fmt"

	"github.com/jakechorley/ilford-drop-in/pkg/clients/sheetsclient"
	"google.golang.org/api/sheets/v4"
)

// SheetsClient defines the interface for sheets operations
type SheetsClient interface {
	GetValues(spreadsheetID, sheetRange string) ([][]interface{}, error)
	AppendRows(spreadsheetID, sheetRange string, values [][]interface{}) error
	CreateSheet(spreadsheetID, sheetTitle string) (int64, error)
	Service() *sheets.Service
}

// Column defines a column with name and type
type Column struct {
	Name string
	Type string // e.g., "text", "date", "int", "bool", "uuid"
}

// TableSchema defines the structure of a table
type TableSchema struct {
	Name    string
	Columns []Column
}

// Schema defines the database schema
type Schema struct {
	Tables []TableSchema
}

// DB represents a connection to a Google Sheets "database"
type DB struct {
	client        SheetsClient
	spreadsheetID string
	schema        *Schema
}

// NewDB creates a new Sheets SQL database connection and ensures schema exists
func NewDB(client *sheetsclient.Client, spreadsheetID string, schema *Schema) (*DB, error) {
	db := &DB{
		client:        client,
		spreadsheetID: spreadsheetID,
		schema:        schema,
	}

	// Ensure schema exists
	if err := db.ensureSchema(); err != nil {
		return nil, fmt.Errorf("failed to ensure schema: %w", err)
	}

	return db, nil
}

// Client returns the underlying sheets client
func (db *DB) Client() SheetsClient {
	return db.client
}

// SpreadsheetID returns the database spreadsheet ID
func (db *DB) SpreadsheetID() string {
	return db.spreadsheetID
}

// InsertRow appends a single row to the specified table
func (db *DB) InsertRow(tableName string, row []interface{}) error {
	return db.client.AppendRows(db.spreadsheetID, tableName, [][]interface{}{row})
}

// InsertRows appends multiple rows to the specified table
func (db *DB) InsertRows(tableName string, rows [][]interface{}) error {
	return db.client.AppendRows(db.spreadsheetID, tableName, rows)
}
