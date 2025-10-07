package sheetsClient

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

	return volunteers, nil
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

		volunteer := model.Volunteer{
			ID:        getField("Unique ID", row),
			FirstName: firstName,
			LastName:  getField("Last name", row),
			Status:    getField("Status", row),
			Gender:    getField("Sex/Gender", row),
			Email:     getField("Email", row),
			GroupKey:  getField("Group key", row),
		}

		volunteers = append(volunteers, volunteer)
	}

	return volunteers, nil
}
