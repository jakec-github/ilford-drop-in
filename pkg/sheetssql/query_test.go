package sheetssql

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetFieldValue_String(t *testing.T) {
	type TestStruct struct {
		Name string
	}

	var s TestStruct
	field := reflect.ValueOf(&s).Elem().Field(0)

	err := setFieldValue(field, "test value")
	assert.NoError(t, err)
	assert.Equal(t, "test value", s.Name)
}

func TestSetFieldValue_Int(t *testing.T) {
	type TestStruct struct {
		Count int
	}

	var s TestStruct
	field := reflect.ValueOf(&s).Elem().Field(0)

	err := setFieldValue(field, "42")
	assert.NoError(t, err)
	assert.Equal(t, 42, s.Count)
}

func TestSetFieldValue_EmptyInt(t *testing.T) {
	type TestStruct struct {
		Count int
	}

	var s TestStruct
	field := reflect.ValueOf(&s).Elem().Field(0)

	err := setFieldValue(field, "")
	assert.NoError(t, err)
	assert.Equal(t, 0, s.Count)
}

func TestSetFieldValue_Bool(t *testing.T) {
	type TestStruct struct {
		Active bool
	}

	var s TestStruct
	field := reflect.ValueOf(&s).Elem().Field(0)

	err := setFieldValue(field, "true")
	assert.NoError(t, err)
	assert.True(t, s.Active)

	err = setFieldValue(field, "false")
	assert.NoError(t, err)
	assert.False(t, s.Active)
}

func TestSetFieldValue_InvalidInt(t *testing.T) {
	type TestStruct struct {
		Count int
	}

	var s TestStruct
	field := reflect.ValueOf(&s).Elem().Field(0)

	err := setFieldValue(field, "not a number")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse int")
}
