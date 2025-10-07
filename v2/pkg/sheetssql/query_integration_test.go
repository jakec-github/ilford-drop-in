package sheetssql

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/sheets/v4"
)

// mockSheetsClient implements a mock for testing
type mockSheetsClient struct {
	getValuesFunc  func(spreadsheetID, sheetRange string) ([][]interface{}, error)
	appendRowsFunc func(spreadsheetID, sheetRange string, values [][]interface{}) error
}

func (m *mockSheetsClient) GetValues(spreadsheetID, sheetRange string) ([][]interface{}, error) {
	if m.getValuesFunc != nil {
		return m.getValuesFunc(spreadsheetID, sheetRange)
	}
	return nil, nil
}

func (m *mockSheetsClient) AppendRows(spreadsheetID, sheetRange string, values [][]interface{}) error {
	if m.appendRowsFunc != nil {
		return m.appendRowsFunc(spreadsheetID, sheetRange, values)
	}
	return nil
}

func (m *mockSheetsClient) CreateSheet(spreadsheetID, sheetTitle string) (int64, error) {
	return 0, nil
}

func (m *mockSheetsClient) Service() *sheets.Service {
	return nil
}

// Test model
type TestPerson struct {
	Name   string `ssql_header:"name" ssql_type:"text"`
	Age    int    `ssql_header:"age" ssql_type:"int"`
	Active bool   `ssql_header:"active" ssql_type:"bool"`
}

func TestGetTableAs_ValidData(t *testing.T) {
	mock := &mockSheetsClient{
		getValuesFunc: func(spreadsheetID, sheetRange string) ([][]interface{}, error) {
			return [][]interface{}{
				{"name", "age", "active"},           // Headers
				{"text", "int", "bool"},             // Types
				{"Alice", "30", "true"},             // Data row 1
				{"Bob", "25", "false"},              // Data row 2
				{"Charlie", "35", "true"},           // Data row 3
			}, nil
		},
	}

	db := &DB{
		client:        mock,
		spreadsheetID: "test-sheet",
	}

	results, err := GetTableAs[TestPerson](db, "test_person")
	require.NoError(t, err)
	require.Len(t, results, 3)

	assert.Equal(t, "Alice", results[0].Name)
	assert.Equal(t, 30, results[0].Age)
	assert.True(t, results[0].Active)

	assert.Equal(t, "Bob", results[1].Name)
	assert.Equal(t, 25, results[1].Age)
	assert.False(t, results[1].Active)

	assert.Equal(t, "Charlie", results[2].Name)
	assert.Equal(t, 35, results[2].Age)
	assert.True(t, results[2].Active)
}

func TestGetTableAs_EmptyTable(t *testing.T) {
	mock := &mockSheetsClient{
		getValuesFunc: func(spreadsheetID, sheetRange string) ([][]interface{}, error) {
			return [][]interface{}{
				{"name", "age", "active"}, // Headers
				{"text", "int", "bool"},   // Types
				// No data rows
			}, nil
		},
	}

	db := &DB{
		client:        mock,
		spreadsheetID: "test-sheet",
	}

	results, err := GetTableAs[TestPerson](db, "test_person")
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestGetTableAs_MissingColumns(t *testing.T) {
	mock := &mockSheetsClient{
		getValuesFunc: func(spreadsheetID, sheetRange string) ([][]interface{}, error) {
			return [][]interface{}{
				{"name", "age", "active"}, // Headers
				{"text", "int", "bool"},   // Types
				{"Alice", "30"},           // Missing "active" column
			}, nil
		},
	}

	db := &DB{
		client:        mock,
		spreadsheetID: "test-sheet",
	}

	results, err := GetTableAs[TestPerson](db, "test_person")
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, "Alice", results[0].Name)
	assert.Equal(t, 30, results[0].Age)
	assert.False(t, results[0].Active) // Should be default value
}

func TestGetTableAs_NilValues(t *testing.T) {
	mock := &mockSheetsClient{
		getValuesFunc: func(spreadsheetID, sheetRange string) ([][]interface{}, error) {
			return [][]interface{}{
				{"name", "age", "active"}, // Headers
				{"text", "int", "bool"},   // Types
				{"Alice", nil, "true"},    // Nil age
			}, nil
		},
	}

	db := &DB{
		client:        mock,
		spreadsheetID: "test-sheet",
	}

	results, err := GetTableAs[TestPerson](db, "test_person")
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, "Alice", results[0].Name)
	assert.Equal(t, 0, results[0].Age) // Should be default value
	assert.True(t, results[0].Active)
}

func TestGetTableAs_InvalidIntConversion(t *testing.T) {
	mock := &mockSheetsClient{
		getValuesFunc: func(spreadsheetID, sheetRange string) ([][]interface{}, error) {
			return [][]interface{}{
				{"name", "age", "active"},      // Headers
				{"text", "int", "bool"},        // Types
				{"Alice", "not-a-number", "true"}, // Invalid int
			}, nil
		},
	}

	db := &DB{
		client:        mock,
		spreadsheetID: "test-sheet",
	}

	_, err := GetTableAs[TestPerson](db, "test_person")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse int")
}

func TestInsertModel(t *testing.T) {
	var capturedRange string
	var capturedValues [][]interface{}

	mock := &mockSheetsClient{
		appendRowsFunc: func(spreadsheetID, sheetRange string, values [][]interface{}) error {
			capturedRange = sheetRange
			capturedValues = values
			return nil
		},
	}

	db := &DB{
		client:        mock,
		spreadsheetID: "test-sheet",
	}

	person := TestPerson{
		Name:   "Alice",
		Age:    30,
		Active: true,
	}

	err := InsertModel(db, person)
	require.NoError(t, err)

	assert.Equal(t, "test_person", capturedRange)
	require.Len(t, capturedValues, 1)
	require.Len(t, capturedValues[0], 3)
	assert.Equal(t, "Alice", capturedValues[0][0])
	assert.Equal(t, 30, capturedValues[0][1])
	assert.Equal(t, true, capturedValues[0][2])
}

func TestInsertModels(t *testing.T) {
	var capturedRange string
	var capturedValues [][]interface{}

	mock := &mockSheetsClient{
		appendRowsFunc: func(spreadsheetID, sheetRange string, values [][]interface{}) error {
			capturedRange = sheetRange
			capturedValues = values
			return nil
		},
	}

	db := &DB{
		client:        mock,
		spreadsheetID: "test-sheet",
	}

	people := []TestPerson{
		{Name: "Alice", Age: 30, Active: true},
		{Name: "Bob", Age: 25, Active: false},
	}

	err := InsertModels(db, people)
	require.NoError(t, err)

	assert.Equal(t, "test_person", capturedRange)
	require.Len(t, capturedValues, 2)

	assert.Equal(t, "Alice", capturedValues[0][0])
	assert.Equal(t, 30, capturedValues[0][1])
	assert.Equal(t, true, capturedValues[0][2])

	assert.Equal(t, "Bob", capturedValues[1][0])
	assert.Equal(t, 25, capturedValues[1][1])
	assert.Equal(t, false, capturedValues[1][2])
}

func TestInsertModels_EmptySlice(t *testing.T) {
	mock := &mockSheetsClient{}

	db := &DB{
		client:        mock,
		spreadsheetID: "test-sheet",
	}

	err := InsertModels(db, []TestPerson{})
	assert.NoError(t, err)
}
