package sheetsclient

import (
	"fmt"
	"strings"
	"time"

	"google.golang.org/api/sheets/v4"
)

const latestTabTitle = "Latest"

// closedCell is what a shift that did not run says in place of a name.
const closedCell = "CLOSED"

// PublishedRotaRow represents a single row in the published rota
type PublishedRotaRow struct {
	Date string // Format: "Mon Jan 02 2006"
	// Closed is a shift that did not run. It is rendered in the first capped
	// Role's column, which is where the sheet has always shown it.
	Closed bool
	// CappedRoles holds the names filling each capped Role, by Role name. Each
	// gets its own columns, ahead of the uncapped Role's.
	CappedRoles map[string][]string
	Volunteers  []string // Full names of volunteers in the uncapped Role
	HotFood     string   // Leave blank for now
	Collection  string   // Leave blank for now
}

// PublishedRota represents the complete published rota data
type PublishedRota struct {
	StartDate  string // Format: "2006-01-02"
	ShiftCount int
	// CappedRoleNames are the capped Roles in priority order, one group of
	// columns each. The sheet's shape follows the configured Roles rather than
	// a hardcoded "Team lead" column.
	CappedRoleNames []string
	Rows            []PublishedRotaRow
}

// PublishRota publishes a rota to the "Latest" tab in Google Sheets.
// If the "Latest" tab already exists and previousRotaTabTitle is non-empty,
// the existing content is first copied to a new tab named previousRotaTabTitle
// (to preserve user-entered columns), then "Latest" is overwritten in-place.
// If "Latest" does not exist, it is created fresh.
func (c *Client) PublishRota(
	spreadsheetID string,
	publishedRota *PublishedRota,
	previousRotaTabTitle string,
) error {
	spreadsheet, err := c.service.Spreadsheets.Get(spreadsheetID).Do()
	if err != nil {
		return fmt.Errorf("failed to get spreadsheet metadata: %w", err)
	}

	var latestSheetID int64 = -1
	for _, sheet := range spreadsheet.Sheets {
		if sheet.Properties.Title == latestTabTitle {
			latestSheetID = sheet.Properties.SheetId
			break
		}
	}
	latestExists := latestSheetID != -1

	if latestExists && previousRotaTabTitle != "" {
		resolvedTitle := resolveUniqueTitle(spreadsheet, previousRotaTabTitle)
		newSheetID, err := c.DuplicateSheet(spreadsheetID, latestSheetID)
		if err != nil {
			return fmt.Errorf("failed to duplicate Latest tab: %w", err)
		}
		if err := c.RenameSheet(spreadsheetID, newSheetID, resolvedTitle); err != nil {
			return fmt.Errorf("failed to rename duplicated tab to %q: %w", resolvedTitle, err)
		}
	}

	if !latestExists {
		if _, err := c.CreateSheet(spreadsheetID, latestTabTitle); err != nil {
			return fmt.Errorf("failed to create Latest tab: %w", err)
		}
	}

	return c.writeRotaData(spreadsheetID, latestTabTitle, publishedRota)
}

// GenerateTabTitle creates a tab title in the format "Aug 24 - Nov 09" from the
// rota's first and last shift dates.
func GenerateTabTitle(startDate, endDate string) (string, error) {
	start, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		return "", fmt.Errorf("invalid start date: %w", err)
	}
	end, err := time.Parse("2006-01-02", endDate)
	if err != nil {
		return "", fmt.Errorf("invalid end date: %w", err)
	}
	return fmt.Sprintf("%s - %s",
		start.Format("Jan 02"),
		end.Format("Jan 02"),
	), nil
}

// sheetTitleExists reports whether a tab with the given title exists in the spreadsheet.
func sheetTitleExists(spreadsheet *sheets.Spreadsheet, title string) bool {
	for _, sheet := range spreadsheet.Sheets {
		if sheet.Properties.Title == title {
			return true
		}
	}
	return false
}

// resolveUniqueTitle returns title if it is not already taken, otherwise appends
// " (2)", " (3)", etc. until a free name is found.
func resolveUniqueTitle(spreadsheet *sheets.Spreadsheet, title string) string {
	if !sheetTitleExists(spreadsheet, title) {
		return title
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s (%d)", title, i)
		if !sheetTitleExists(spreadsheet, candidate) {
			return candidate
		}
	}
}

// writeRotaData writes rota rows to an existing tab, then clears any stale rows below.
// Layout: rows 1-2 empty, row 3 header, row 4+ data.
//
// Columns are Date, then each capped Role in priority order, then the uncapped
// Role's, then the two hand-typed trailing ones. A capped Role always gets at
// least one column even when nothing fills it, so a rota of nothing but closed
// shifts still has somewhere to say so.
func (c *Client) writeRotaData(spreadsheetID, tabTitle string, publishedRota *PublishedRota) error {
	maxVolunteers := 0
	for _, row := range publishedRota.Rows {
		if len(row.Volunteers) > maxVolunteers {
			maxVolunteers = len(row.Volunteers)
		}
	}

	cappedWidths := make(map[string]int, len(publishedRota.CappedRoleNames))
	for _, name := range publishedRota.CappedRoleNames {
		cappedWidths[name] = 1
		for _, row := range publishedRota.Rows {
			if len(row.CappedRoles[name]) > cappedWidths[name] {
				cappedWidths[name] = len(row.CappedRoles[name])
			}
		}
	}

	header := []interface{}{"Date"}
	for _, name := range publishedRota.CappedRoleNames {
		if cappedWidths[name] == 1 {
			header = append(header, name)
			continue
		}
		for i := 0; i < cappedWidths[name]; i++ {
			header = append(header, fmt.Sprintf("%s %d", name, i+1))
		}
	}
	// The uncapped Role's columns keep the heading the sheet has always used
	// rather than taking the Role's configured name, which readers of the
	// published rota have no reason to learn. S3 revisits the sheet's shape.
	for i := 0; i < maxVolunteers; i++ {
		header = append(header, fmt.Sprintf("Volunteer %d", i+1))
	}
	header = append(header, "Hot food", "Collection")

	dataRows := make([][]interface{}, 0, len(publishedRota.Rows))
	for _, row := range publishedRota.Rows {
		sheetRow := []interface{}{row.Date}
		closedCellWritten := false
		for _, name := range publishedRota.CappedRoleNames {
			for i := 0; i < cappedWidths[name]; i++ {
				switch {
				case row.Closed && !closedCellWritten:
					sheetRow = append(sheetRow, closedCell)
					closedCellWritten = true
				case i < len(row.CappedRoles[name]):
					sheetRow = append(sheetRow, row.CappedRoles[name][i])
				default:
					sheetRow = append(sheetRow, "")
				}
			}
		}
		for i := 0; i < maxVolunteers; i++ {
			switch {
			case row.Closed && !closedCellWritten:
				// No capped Role is configured, so the closure is announced in
				// the first column there is.
				sheetRow = append(sheetRow, closedCell)
				closedCellWritten = true
			case i < len(row.Volunteers):
				sheetRow = append(sheetRow, row.Volunteers[i])
			default:
				sheetRow = append(sheetRow, "")
			}
		}
		sheetRow = append(sheetRow, row.HotFood, row.Collection)
		dataRows = append(dataRows, sheetRow)
	}

	allRows := [][]interface{}{{}, {}, header}
	allRows = append(allRows, dataRows...)

	valueRange := &sheets.ValueRange{Values: allRows}
	_, err := c.service.Spreadsheets.Values.Update(
		spreadsheetID,
		fmt.Sprintf("%s!A1", tabTitle),
		valueRange,
	).ValueInputOption("RAW").Do()
	if err != nil {
		return fmt.Errorf("failed to write data to tab %q: %w", tabTitle, err)
	}

	// Clear any stale rows left over from a previously longer rota
	clearRange := fmt.Sprintf("%s!A%d:ZZ", tabTitle, len(allRows)+1)
	_, err = c.service.Spreadsheets.Values.Clear(spreadsheetID, clearRange, &sheets.ClearValuesRequest{}).Do()
	if err != nil {
		return fmt.Errorf("failed to clear stale rows in tab %q: %w", tabTitle, err)
	}

	return nil
}

// findColumnIndex finds the index of a column by its header name
func findColumnIndex(header []interface{}, columnName string) int {
	for i, cell := range header {
		if str, ok := cell.(string); ok && str == columnName {
			return i
		}
	}
	return -1
}

// findVolunteerColumns finds all volunteer column indices in order
func findVolunteerColumns(header []interface{}) []int {
	var cols []int
	for i, cell := range header {
		if str, ok := cell.(string); ok && strings.HasPrefix(str, "Volunteer ") {
			cols = append(cols, i)
		}
	}
	return cols
}
