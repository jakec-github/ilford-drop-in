package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
