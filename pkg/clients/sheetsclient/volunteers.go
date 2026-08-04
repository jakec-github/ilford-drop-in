package sheetsclient

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

// Expected column names in volunteers sheet. Roles are not among them: they
// arrive as `<name> - Role` tick columns discovered from the header, so that
// configuring a new Role needs a new column and no code change.
var volunteerFields = []string{
	"Unique ID",
	"First name",
	"Last name",
	"Status",
	"Sex/Gender",
	"Email",
	"Group key",
}

// roleColumnSuffix marks a header cell as a Role tick-box column. The legacy
// `Role` dropdown does not carry it, so the two can sit side by side while the
// sheet is migrated.
const roleColumnSuffix = " - Role"

// noGroupValue is what the Group key dropdown holds for a volunteer in no
// group. A Sheets dropdown cannot be unset, so `None` is the only way to say
// "no longer in a group" once a key has been picked, and it means what a blank
// cell means. Normalising it away here keeps the placeholder — a spreadsheet
// artefact — out of the domain entirely. Compared lower-cased and trimmed,
// since a dropdown's label is not guaranteed to keep its capitalisation.
const noGroupValue = "none"

// tickValues are the cell contents that count as a ticked box, compared
// lower-cased and trimmed. A Sheets tick-box writes TRUE; the rest are what
// people type by hand.
var tickValues = map[string]bool{
	"true": true,
	"yes":  true,
	"✓":    true,
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
	volunteers, err := ParseVolunteers(values, cfg.RoleTable())
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

// ParseVolunteers converts raw spreadsheet data into Volunteer structs. It is
// exported because the same tabular shape arrives from more than one place: the
// Sheets API here, and a CSV export in dev mode (internal/devmode). Both go
// through this parser so the column contract is defined once.
//
// The Roles a volunteer holds come from `<name> - Role` tick columns matched
// against the configured Roles, so the sheet and the config can drift without
// the roster failing to load: a column naming a Role config does not have is
// ignored, and a configured Role with no column is simply held by nobody. Both
// are warned about, because both are usually a half-finished edit.
func ParseVolunteers(raw [][]interface{}, roles model.Roles) ([]model.Volunteer, error) {
	if len(raw) < 1 {
		return nil, fmt.Errorf("no header row found")
	}

	// Build field index map from header row
	fieldIndexes := make(map[string]int)
	headerRow := raw[0]

	for _, field := range volunteerFields {
		index := -1
		for i, cell := range headerRow {
			if cellStr, ok := cell.(string); ok && strings.TrimSpace(cellStr) == field {
				index = i
				break
			}
		}
		if index == -1 {
			return nil, fmt.Errorf("missing required field in header: %s", field)
		}
		fieldIndexes[field] = index
	}

	roleIndexes := findRoleColumns(headerRow, roles)

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
			return strings.TrimSpace(str)
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
			Roles:     heldRoles(row, roles, roleIndexes),
			Status:    getField("Status", row),
			Gender:    getField("Sex/Gender", row),
			Email:     getField("Email", row),
			GroupKey:  groupKey(getField("Group key", row)),
		}

		volunteers = append(volunteers, volunteer)
	}

	return volunteers, nil
}

// groupKey reads one volunteer's Group key cell, returning the empty string for
// anyone in no group however the sheet says it.
func groupKey(cell string) string {
	if strings.ToLower(cell) == noGroupValue {
		return ""
	}
	return cell
}

// findRoleColumns maps each configured Role to the column holding its ticks,
// warning about either side of a mismatch between the sheet and the config.
func findRoleColumns(headerRow []interface{}, roles model.Roles) map[string]int {
	indexes := make(map[string]int)

	for i, cell := range headerRow {
		header, ok := cell.(string)
		if !ok {
			continue
		}
		header = strings.TrimSpace(header)
		if !strings.HasSuffix(header, roleColumnSuffix) {
			continue
		}

		name := strings.TrimSpace(strings.TrimSuffix(header, roleColumnSuffix))
		if _, known := roles.ByName(name); !known {
			slog.Warn("volunteer sheet has a Role column no configured Role matches; ignoring it",
				"column", header, "role", name)
			continue
		}
		indexes[name] = i
	}

	for _, role := range roles.ByPriority() {
		if _, found := indexes[role.Name]; !found {
			slog.Warn("configured Role has no column in the volunteer sheet; nobody holds it",
				"role", role.Name, "expectedColumn", role.Name+roleColumnSuffix)
		}
	}

	return indexes
}

// heldRoles reads one volunteer's ticks, returning the Roles they hold in
// priority order.
func heldRoles(row []interface{}, roles model.Roles, roleIndexes map[string]int) []string {
	var held []string

	for _, role := range roles.ByPriority() {
		index, found := roleIndexes[role.Name]
		if !found || index >= len(row) {
			continue
		}
		cell, ok := row[index].(string)
		if !ok {
			continue
		}
		if tickValues[strings.ToLower(strings.TrimSpace(cell))] {
			held = append(held, role.Name)
		}
	}

	return held
}
