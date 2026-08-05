package sheetsclient

import (
	"encoding/csv"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

// Expected column names in volunteers sheet.
var volunteerFields = []string{
	"Unique ID",
	"First name",
	"Last name",
	"Status",
	"Sex/Gender",
	"Email",
	"Group key",
	"Roles",
}

// roleSeparator splits the Roles cell. A Sheets multi-select dropdown packs the
// chips someone picked into one string joined by this, quoting any value that
// holds the separator or a quotation mark and doubling the quotation marks
// inside — the CSV rules. heldRoles reads it as such, so a Role name may hold
// either character and still come back whole.
const roleSeparator = ','

// noGroupValue is what the Group key dropdown holds for a volunteer in no
// group. A Sheets dropdown cannot be unset, so `None` is the only way to say
// "no longer in a group" once a key has been picked, and it means what a blank
// cell means. Normalising it away here keeps the placeholder — a spreadsheet
// artefact — out of the domain entirely. Compared lower-cased and trimmed,
// since a dropdown's label is not guaranteed to keep its capitalisation.
const noGroupValue = "none"

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
// The Roles a volunteer holds come from one `Roles` cell, whose values are
// matched against the configured Roles. Config stays authoritative: a value
// naming a Role config does not have is warned about and skipped rather than
// failing the roster, because it is usually a half-finished edit and the rest
// of the cell is still good.
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
			Roles:     heldRoles(getField("Roles", row), roles),
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

// heldRoles reads one volunteer's Roles cell, returning the Roles they hold in
// priority order — so the first is the one a caller showing a single Role
// wants, whatever order the chips were picked in. Duplicates collapse for the
// same reason: the answer is a set.
func heldRoles(cell string, roles model.Roles) []string {
	picked := make(map[string]bool)

	for _, value := range splitRoleCell(cell) {
		// The dropdown writes no padding, but a hand-typed cell has whatever
		// someone typed. A Role whose name only differs by its surrounding
		// space is not a case worth keeping over that.
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, known := roles.ByName(value); !known {
			slog.Warn("volunteer sheet names a Role no configured Role matches; ignoring it",
				"role", value)
			continue
		}
		picked[value] = true
	}

	var held []string
	for _, role := range roles.ByPriority() {
		if picked[role.Name] {
			held = append(held, role.Name)
		}
	}

	return held
}

// splitRoleCell unpacks a multi-select cell into the values it holds. The cell
// is a single CSV record, so `encoding/csv` is the parser rather than a
// hand-rolled one: it is the same escaping, and getting quoting subtly wrong is
// how a Role named `Kitchen, hot food` silently becomes two Roles nobody holds.
//
// LazyQuotes because a cell is hand-editable: quoting the dropdown would never
// have written should cost the values it mangles, not the whole roster. What
// survives is matched against config anyway, and anything that does not match
// is warned about there.
func splitRoleCell(cell string) []string {
	if strings.TrimSpace(cell) == "" {
		return nil
	}

	reader := csv.NewReader(strings.NewReader(cell))
	reader.Comma = roleSeparator
	// Sheets is not consistent about a space after the separator, and a quoted
	// value must still be read as quoted when one is there.
	reader.TrimLeadingSpace = true
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1

	values, err := reader.Read()
	if err != nil {
		slog.Warn("could not read the Roles cell in the volunteer sheet; ignoring it",
			"cell", cell, "error", err)
		return nil
	}

	return values
}
