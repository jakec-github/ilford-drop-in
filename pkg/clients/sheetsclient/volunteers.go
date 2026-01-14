package sheetsclient

import (
	"fmt"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

// Expected column names in volunteers sheet
var volunteerFields = []string{
	"Unique ID",
	"First name",
	"Last name",
	"Role",
	"Status",
	"Sex/Gender",
	"Email",
	"Group key",
}

// ListVolunteers retrieves and parses volunteers from the configured spreadsheet
func (c *Client) ListVolunteers(cfg *config.Config) ([]model.Volunteer, error) {
	// Get raw data from spreadsheet
	values, err := c.GetValues(cfg.VolunteerSheetID, cfg.ServiceVolunteersTab)
	if err != nil {
		return nil, fmt.Errorf("failed to get volunteer data: %w", err)
	}

	if len(values) == 0 {
		return nil, fmt.Errorf("spreadsheet is empty")
	}

	// Parse volunteers
	volunteers, err := parseVolunteers(values)
	if err != nil {
		return nil, fmt.Errorf("failed to parse volunteers: %w", err)
	}

	// Compute display names for all volunteers (ensures uniqueness across entire list)
	ComputeDisplayNames(volunteers)

	return volunteers, nil
}

// ComputeDisplayNames calculates display names for a list of volunteers based on uniqueness:
// - If first name is unique: use first name only
// - If first name + first letter of surname is unique: use "FirstName L."
// - Otherwise: use full name "FirstName LastName"
func ComputeDisplayNames(volunteers []model.Volunteer) {
	// Count occurrences of each first name
	firstNameCounts := make(map[string]int)
	for _, v := range volunteers {
		firstNameCounts[v.FirstName]++
	}

	// Count occurrences of each "FirstName L." format
	firstNameInitialCounts := make(map[string]int)
	for _, v := range volunteers {
		if v.LastName != "" {
			key := v.FirstName + " " + string(v.LastName[0]) + "."
			firstNameInitialCounts[key]++
		}
	}

	// Assign display names
	for i := range volunteers {
		v := &volunteers[i]

		// Try first name only
		if firstNameCounts[v.FirstName] == 1 {
			v.DisplayName = v.FirstName
			continue
		}

		// Try first name + initial
		if v.LastName != "" {
			initialKey := v.FirstName + " " + string(v.LastName[0]) + "."
			if firstNameInitialCounts[initialKey] == 1 {
				v.DisplayName = initialKey
				continue
			}
		}

		// Fall back to full name
		v.DisplayName = v.FirstName + " " + v.LastName
	}
}

// parseVolunteers converts raw spreadsheet data into Volunteer structs
func parseVolunteers(raw [][]interface{}) ([]model.Volunteer, error) {
	if len(raw) < 1 {
		return nil, fmt.Errorf("no header row found")
	}

	// Build field index map from header row
	fieldIndexes := make(map[string]int)
	headerRow := raw[0]

	for _, field := range volunteerFields {
		index := -1
		for i, cell := range headerRow {
			if cellStr, ok := cell.(string); ok && cellStr == field {
				index = i
				break
			}
		}
		if index == -1 {
			return nil, fmt.Errorf("missing required field in header: %s", field)
		}
		fieldIndexes[field] = index
	}

	// Helper to get field value from row
	getField := func(field string, row []interface{}) string {
		index, ok := fieldIndexes[field]
		if !ok {
			return ""
		}
		if index >= len(row) {
			return ""
		}
		if str, ok := row[index].(string); ok {
			return str
		}
		return ""
	}

	// Parse data rows
	volunteers := make([]model.Volunteer, 0, len(raw)-1)
	for i := 1; i < len(raw); i++ {
		row := raw[i]

		firstName := getField("First name", row)
		// Skip empty rows (rows with no first name)
		if firstName == "" {
			continue
		}

		role := model.Role(getField("Role", row))
		if !role.IsValid() {
			return nil, fmt.Errorf("invalid role for volunteer in row %d", i)
		}

		volunteer := model.Volunteer{
			ID:        getField("Unique ID", row),
			FirstName: firstName,
			LastName:  getField("Last name", row),
			Role:      role,
			Status:    getField("Status", row),
			Gender:    getField("Sex/Gender", row),
			Email:     getField("Email", row),
			GroupKey:  getField("Group key", row),
		}

		volunteers = append(volunteers, volunteer)
	}

	return volunteers, nil
}
