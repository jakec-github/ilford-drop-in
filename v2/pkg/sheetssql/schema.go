package sheetssql

import (
	"fmt"
	"reflect"
	"strings"
)

// SchemaFromModels builds a Schema by reflecting on struct definitions
// Each struct represents a table, with fields representing columns
// Fields must have `ssql_header:"column_name"` and `ssql_type:"column_type"` tags
func SchemaFromModels(models ...interface{}) (*Schema, error) {
	tables := make([]TableSchema, 0, len(models))

	for _, model := range models {
		table, err := tableSchemaFromModel(model)
		if err != nil {
			return nil, err
		}
		tables = append(tables, table)
	}

	return &Schema{Tables: tables}, nil
}

// tableSchemaFromModel extracts a TableSchema from a single struct
func tableSchemaFromModel(model interface{}) (TableSchema, error) {
	t := reflect.TypeOf(model)

	// Handle pointer to struct
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return TableSchema{}, fmt.Errorf("model must be a struct, got %s", t.Kind())
	}

	// Use struct name as table name (convert to snake_case)
	tableName := toSnakeCase(t.Name())

	columns := make([]Column, 0, t.NumField())

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Get ssql_header tag (column name)
		sheetTag := field.Tag.Get("ssql_header")
		if sheetTag == "" {
			return TableSchema{}, fmt.Errorf("field %s.%s missing 'ssql_header' tag", t.Name(), field.Name)
		}

		// Get ssql_type tag (column type)
		typeTag := field.Tag.Get("ssql_type")
		if typeTag == "" {
			return TableSchema{}, fmt.Errorf("field %s.%s missing 'ssql_type' tag", t.Name(), field.Name)
		}

		columns = append(columns, Column{
			Name: sheetTag,
			Type: typeTag,
		})
	}

	if len(columns) == 0 {
		return TableSchema{}, fmt.Errorf("struct %s has no fields", t.Name())
	}

	return TableSchema{
		Name:    tableName,
		Columns: columns,
	}, nil
}

// toSnakeCase converts PascalCase to snake_case
func toSnakeCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteRune('_')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}

// ensureSchema validates that all tables in the schema exist and match expected structure
// Creates any missing tables
func (db *DB) ensureSchema() error {
	// Get list of existing sheets
	existingSheets, err := db.getExistingSheets()
	if err != nil {
		return fmt.Errorf("failed to get existing sheets: %w", err)
	}

	// Build a set of existing sheet names for quick lookup
	sheetSet := make(map[string]bool)
	for _, sheet := range existingSheets {
		sheetSet[sheet] = true
	}

	// Verify or create each table
	for _, table := range db.schema.Tables {
		if sheetSet[table.Name] {
			// Sheet exists, verify it matches schema
			if err := db.verifyTableSchema(table); err != nil {
				return fmt.Errorf("table %s schema mismatch: %w", table.Name, err)
			}
		} else {
			// Sheet doesn't exist, create it
			if err := db.createTable(table); err != nil {
				return fmt.Errorf("failed to create table %s: %w", table.Name, err)
			}
		}
	}

	return nil
}

// getExistingSheets returns a list of sheet names in the spreadsheet
func (db *DB) getExistingSheets() ([]string, error) {
	spreadsheet, err := db.client.Service().Spreadsheets.Get(db.spreadsheetID).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to get spreadsheet: %w", err)
	}

	sheets := make([]string, 0, len(spreadsheet.Sheets))
	for _, sheet := range spreadsheet.Sheets {
		sheets = append(sheets, sheet.Properties.Title)
	}

	return sheets, nil
}

// verifyTableSchema checks that a table's header and type rows match the schema
func (db *DB) verifyTableSchema(table TableSchema) error {
	// Get first two rows (headers and types)
	values, err := db.client.GetValues(db.spreadsheetID, fmt.Sprintf("%s!A1:ZZ2", table.Name))
	if err != nil {
		return fmt.Errorf("failed to read table headers: %w", err)
	}

	if len(values) < 2 {
		return fmt.Errorf("table missing header or type row")
	}

	headers := values[0]
	types := values[1]

	if len(headers) != len(table.Columns) {
		return fmt.Errorf("expected %d columns, found %d", len(table.Columns), len(headers))
	}

	// Verify each column
	for i, col := range table.Columns {
		if i >= len(headers) {
			return fmt.Errorf("missing header for column %s", col.Name)
		}

		headerStr, ok := headers[i].(string)
		if !ok || headerStr != col.Name {
			return fmt.Errorf("column %d: expected header '%s', got '%v'", i, col.Name, headers[i])
		}

		if i >= len(types) {
			return fmt.Errorf("missing type for column %s", col.Name)
		}

		typeStr, ok := types[i].(string)
		if !ok || typeStr != col.Type {
			return fmt.Errorf("column %d (%s): expected type '%s', got '%v'", i, col.Name, col.Type, types[i])
		}
	}

	return nil
}

// createTable creates a new sheet with header and type rows
func (db *DB) createTable(table TableSchema) error {
	// Create the sheet
	_, err := db.client.CreateSheet(db.spreadsheetID, table.Name)
	if err != nil {
		return fmt.Errorf("failed to create sheet: %w", err)
	}

	// Build header and type rows
	headers := make([]interface{}, len(table.Columns))
	types := make([]interface{}, len(table.Columns))

	for i, col := range table.Columns {
		headers[i] = col.Name
		types[i] = col.Type
	}

	// Write headers and types as first two rows
	rows := [][]interface{}{headers, types}
	if err := db.client.AppendRows(db.spreadsheetID, table.Name, rows); err != nil {
		return fmt.Errorf("failed to write headers and types: %w", err)
	}

	return nil
}
