package sheetssql

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type TestRotation struct {
	ID         string `ssql_header:"id" ssql_type:"uuid"`
	Start      string `ssql_header:"start" ssql_type:"date"`
	ShiftCount int    `ssql_header:"shift_count" ssql_type:"int"`
}

type TestAllocation struct {
	ID          string `ssql_header:"id" ssql_type:"uuid"`
	RotaID      string `ssql_header:"rota_id" ssql_type:"uuid"`
	ShiftDate   string `ssql_header:"shift_date" ssql_type:"date"`
	Role        string `ssql_header:"role" ssql_type:"text"`
	VolunteerID string `ssql_header:"volunteer_id" ssql_type:"text"`
}

func TestSchemaFromModels_SingleModel(t *testing.T) {
	schema, err := SchemaFromModels(TestRotation{})
	require.NoError(t, err)

	require.Len(t, schema.Tables, 1)
	table := schema.Tables[0]

	assert.Equal(t, "test_rotation", table.Name)
	require.Len(t, table.Columns, 3)

	assert.Equal(t, "id", table.Columns[0].Name)
	assert.Equal(t, "uuid", table.Columns[0].Type)

	assert.Equal(t, "start", table.Columns[1].Name)
	assert.Equal(t, "date", table.Columns[1].Type)

	assert.Equal(t, "shift_count", table.Columns[2].Name)
	assert.Equal(t, "int", table.Columns[2].Type)
}

func TestSchemaFromModels_MultipleModels(t *testing.T) {
	schema, err := SchemaFromModels(TestRotation{}, TestAllocation{})
	require.NoError(t, err)

	require.Len(t, schema.Tables, 2)

	// Check first table
	assert.Equal(t, "test_rotation", schema.Tables[0].Name)
	assert.Len(t, schema.Tables[0].Columns, 3)

	// Check second table
	assert.Equal(t, "test_allocation", schema.Tables[1].Name)
	assert.Len(t, schema.Tables[1].Columns, 5)
}

func TestSchemaFromModels_WithPointer(t *testing.T) {
	schema, err := SchemaFromModels(&TestRotation{})
	require.NoError(t, err)

	require.Len(t, schema.Tables, 1)
	assert.Equal(t, "test_rotation", schema.Tables[0].Name)
}

func TestSchemaFromModels_MissingSheetTag(t *testing.T) {
	type InvalidModel struct {
		ID string `ssql_type:"uuid"` // Missing ssql_header tag
	}

	_, err := SchemaFromModels(InvalidModel{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing 'ssql_header' tag")
}

func TestSchemaFromModels_MissingTypeTag(t *testing.T) {
	type InvalidModel struct {
		ID string `ssql_header:"id"` // Missing ssql_type tag
	}

	_, err := SchemaFromModels(InvalidModel{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing 'ssql_type' tag")
}

func TestSchemaFromModels_NotAStruct(t *testing.T) {
	_, err := SchemaFromModels("not a struct")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be a struct")
}

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"TestRotation", "test_rotation"},
		{"TestAllocation", "test_allocation"},
		{"AvailabilityRequest", "availability_request"},
		{"UUID", "u_u_i_d"},
		{"simple", "simple"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := toSnakeCase(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
