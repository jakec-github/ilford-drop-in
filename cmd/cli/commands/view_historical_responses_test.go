package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

func TestAvailabilityColor(t *testing.T) {
	green := "GREEN"
	yellow := "YELLOW"
	orange := "ORANGE"

	tests := []struct {
		name      string
		available int
		total     int
		expected  string
	}{
		{"1 of 8 - orange (<=3)", 1, 8, orange},
		{"2 of 8 - orange (<=3)", 2, 8, orange},
		{"3 of 8 - orange (<=3)", 3, 8, orange},
		{"4 of 8 - yellow (<=half, >3)", 4, 8, yellow},
		{"5 of 8 - green (>half)", 5, 8, green},
		{"8 of 8 - green (>half)", 8, 8, green},
		{"1 of 2 - orange (<=3)", 1, 2, orange},
		{"2 of 2 - orange (<=3)", 2, 2, orange},
		{"3 of 4 - orange (<=3)", 3, 4, orange},
		{"4 of 6 - green (>half)", 4, 6, green},
		{"3 of 6 - orange (<=3)", 3, 6, orange},
		{"4 of 10 - yellow (<=half, >3)", 4, 10, yellow},
		{"5 of 10 - yellow (<=half, >3)", 5, 10, yellow},
		{"6 of 10 - green (>half)", 6, 10, green},
		{"1 of 1 - orange (<=3)", 1, 1, orange},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := availabilityColor(tt.available, tt.total, green, yellow, orange)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTotalAvailability(t *testing.T) {
	rotations := []db.Rotation{
		{ID: "rota-1", ShiftCount: 6},
		{ID: "rota-2", ShiftCount: 8},
		{ID: "rota-3", ShiftCount: 4},
	}

	matrix := map[string]map[string]services.VolunteerRotaStatus{
		"alice": {
			"rota-1": {Status: "available", AvailableCount: 4, ShiftCount: 6},
			"rota-2": {Status: "available", AvailableCount: 7, ShiftCount: 8},
			"rota-3": {Status: "available", AvailableCount: 3, ShiftCount: 4},
		},
		"bob": {
			"rota-1": {Status: "no_response", ShiftCount: 6},
			"rota-2": {Status: "available", AvailableCount: 5, ShiftCount: 8},
			"rota-3": {Status: "no_form", ShiftCount: 4},
		},
		"carol": {
			"rota-1": {Status: "no_form", ShiftCount: 6},
			"rota-2": {Status: "no_availability", ShiftCount: 8},
			"rota-3": {Status: "form_error", ShiftCount: 4},
		},
		"dave": {
			"rota-1": {Status: "available", AvailableCount: 6, ShiftCount: 6},
			"rota-2": {Status: "no_response", ShiftCount: 8},
			"rota-3": {Status: "available", AvailableCount: 4, ShiftCount: 4},
		},
	}

	tests := []struct {
		name     string
		volID    string
		expected int
	}{
		{"alice - all available sums to 14", "alice", 14},
		{"bob - only rota-2 counts (5)", "bob", 5},
		{"carol - nothing counts (0)", "carol", 0},
		{"dave - rota-1 + rota-3 (10)", "dave", 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vol := model.Volunteer{ID: tt.volID}
			result := totalAvailability(vol, rotations, matrix)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTotalAvailability_MissingMatrixEntries(t *testing.T) {
	rotations := []db.Rotation{
		{ID: "rota-1", ShiftCount: 6},
		{ID: "rota-2", ShiftCount: 8},
	}

	// Volunteer not in matrix at all
	matrix := map[string]map[string]services.VolunteerRotaStatus{}

	vol := model.Volunteer{ID: "unknown"}
	result := totalAvailability(vol, rotations, matrix)
	assert.Equal(t, 0, result)
}
