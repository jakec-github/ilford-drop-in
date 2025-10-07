package sheetssql

import (
	"fmt"
	"reflect"
	"strconv"
)

// GetTableAs retrieves all rows from a table and maps them to structs of type T
// Skips the first two rows (headers and types)
func GetTableAs[T any](db *DB, tableName string) ([]T, error) {
	// Get all values from the table
	values, err := db.client.GetValues(db.spreadsheetID, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to get table %s: %w", tableName, err)
	}

	if len(values) < 3 {
		// Need at least headers, types, and one data row
		return []T{}, nil
	}

	// Skip first two rows (headers and types)
	dataRows := values[2:]

	// Get column mapping from headers
	if len(values) < 1 {
		return nil, fmt.Errorf("table has no header row")
	}
	headers := values[0]

	// Get the type T to work with
	var model T
	t := reflect.TypeOf(model)

	// Build mapping of column name to index
	columnIndexes := make(map[string]int)
	for i, header := range headers {
		if headerStr, ok := header.(string); ok {
			columnIndexes[headerStr] = i
		}
	}

	// Build mapping of struct fields
	fieldMap := make(map[string]reflect.StructField)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		columnName := field.Tag.Get("ssql_header")
		if columnName != "" {
			fieldMap[columnName] = field
		}
	}

	// Parse each data row into a struct
	results := make([]T, 0, len(dataRows))
	for rowIdx, row := range dataRows {
		result := reflect.New(t).Elem()

		for columnName, colIdx := range columnIndexes {
			field, ok := fieldMap[columnName]
			if !ok {
				// Column doesn't map to a field, skip it
				continue
			}

			// Get the cell value
			if colIdx >= len(row) {
				// Column is empty in this row
				continue
			}

			cellValue := row[colIdx]
			if cellValue == nil {
				continue
			}

			// Convert and set the value
			if err := setFieldValue(result.FieldByName(field.Name), cellValue); err != nil {
				return nil, fmt.Errorf("row %d, column %s: %w", rowIdx+3, columnName, err)
			}
		}

		results = append(results, result.Interface().(T))
	}

	return results, nil
}

// setFieldValue converts a sheet cell value to the appropriate Go type and sets it on the field
func setFieldValue(field reflect.Value, cellValue interface{}) error {
	if !field.CanSet() {
		return fmt.Errorf("field cannot be set")
	}

	// Get the cell as a string first (sheets API returns strings)
	cellStr, ok := cellValue.(string)
	if !ok {
		return fmt.Errorf("cell value is not a string")
	}

	// Convert based on field type
	switch field.Kind() {
	case reflect.String:
		field.SetString(cellStr)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if cellStr == "" {
			field.SetInt(0)
		} else {
			intVal, err := strconv.ParseInt(cellStr, 10, 64)
			if err != nil {
				return fmt.Errorf("failed to parse int: %w", err)
			}
			field.SetInt(intVal)
		}

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if cellStr == "" {
			field.SetUint(0)
		} else {
			uintVal, err := strconv.ParseUint(cellStr, 10, 64)
			if err != nil {
				return fmt.Errorf("failed to parse uint: %w", err)
			}
			field.SetUint(uintVal)
		}

	case reflect.Float32, reflect.Float64:
		if cellStr == "" {
			field.SetFloat(0)
		} else {
			floatVal, err := strconv.ParseFloat(cellStr, 64)
			if err != nil {
				return fmt.Errorf("failed to parse float: %w", err)
			}
			field.SetFloat(floatVal)
		}

	case reflect.Bool:
		if cellStr == "" {
			field.SetBool(false)
		} else {
			boolVal, err := strconv.ParseBool(cellStr)
			if err != nil {
				return fmt.Errorf("failed to parse bool: %w", err)
			}
			field.SetBool(boolVal)
		}

	default:
		return fmt.Errorf("unsupported field type: %s", field.Kind())
	}

	return nil
}

// InsertModel appends a struct as a row to its corresponding table
func InsertModel[T any](db *DB, model T) error {
	t := reflect.TypeOf(model)
	v := reflect.ValueOf(model)

	// Get table name from struct name
	tableName := toSnakeCase(t.Name())

	// Build row from struct fields
	row := make([]interface{}, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		columnName := field.Tag.Get("ssql_header")
		if columnName == "" {
			continue
		}

		// Get field value
		fieldValue := v.Field(i)
		row = append(row, fieldValue.Interface())
	}

	return db.InsertRow(tableName, row)
}

// InsertModels appends multiple structs as rows to their corresponding table
func InsertModels[T any](db *DB, models []T) error {
	if len(models) == 0 {
		return nil
	}

	t := reflect.TypeOf(models[0])
	tableName := toSnakeCase(t.Name())

	rows := make([][]interface{}, 0, len(models))
	for _, model := range models {
		v := reflect.ValueOf(model)
		row := make([]interface{}, 0, t.NumField())

		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			columnName := field.Tag.Get("ssql_header")
			if columnName == "" {
				continue
			}

			fieldValue := v.Field(i)
			row = append(row, fieldValue.Interface())
		}

		rows = append(rows, row)
	}

	return db.InsertRows(tableName, rows)
}
