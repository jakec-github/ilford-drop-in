package sheetsclient

import (
	"fmt"
	"time"

	"google.golang.org/api/sheets/v4"
)

const latestTabTitle = "Latest"

// closedCell is what a shift that did not run says in place of a name.
const closedCell = "CLOSED"

// PublishedRotaRow represents a single row in the published rota
type PublishedRotaRow struct {
	Date string // Format: "Mon Jan 02 2006"
	// Closed is a shift that did not run. It is rendered in the row's first
	// column after the date, which is where the sheet has always shown it.
	Closed bool
	// Roles holds the names filling each Role, by Role name — one person per
	// cell, each Role in columns of its own.
	Roles map[string][]string
	// UnknownRole holds names whose allocation named a Role the app does not
	// know, or named none at all. They worked the shift, so they are published;
	// the sheet says plainly that it cannot say what they did.
	UnknownRole []string
	HotFood     string // Leave blank for now
	Collection  string // Leave blank for now
}

// PublishedRota represents the complete published rota data
type PublishedRota struct {
	StartDate  string // Format: "2006-01-02"
	ShiftCount int
	// RoleNames are the configured Roles in priority order, one group of
	// columns each. The sheet's shape follows the configured Roles rather than
	// a hardcoded "Team lead" column.
	RoleNames []string
	Rows      []PublishedRotaRow
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

// unknownRoleHeading names the columns for people the app cannot say the Role
// of. They are published — they worked the shift — and the heading says why
// there is nothing more to say about them.
const unknownRoleHeading = "Unknown role"

// writeRotaData writes rota rows to an existing tab, then clears any stale rows
// below. Layout: rows 1-2 empty, row 3 header, row 4+ data.
func (c *Client) writeRotaData(spreadsheetID, tabTitle string, publishedRota *PublishedRota) error {
	allRows := rotaValues(publishedRota)

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

// rotaValues lays the rota out as the cells that go on the tab: rows 1-2 empty,
// row 3 the header, row 4+ one row per shift.
//
// Columns are Date, then each configured Role in priority order, then any
// unknown-Role columns, then the two hand-typed trailing ones. One person per
// cell throughout: a Role takes as many columns as the rota's fullest shift
// needs of it, and always at least one even when nothing fills it, so a rota of
// nothing but closed shifts still has somewhere to say so. Unknown-Role columns
// exist only when somebody is in one.
func rotaValues(publishedRota *PublishedRota) [][]interface{} {
	roleWidths := make(map[string]int, len(publishedRota.RoleNames))
	for _, name := range publishedRota.RoleNames {
		roleWidths[name] = 1
		for _, row := range publishedRota.Rows {
			if len(row.Roles[name]) > roleWidths[name] {
				roleWidths[name] = len(row.Roles[name])
			}
		}
	}
	unknownWidth := 0
	for _, row := range publishedRota.Rows {
		if len(row.UnknownRole) > unknownWidth {
			unknownWidth = len(row.UnknownRole)
		}
	}

	header := []interface{}{"Date"}
	for _, name := range publishedRota.RoleNames {
		header = append(header, headings(name, roleWidths[name])...)
	}
	header = append(header, headings(unknownRoleHeading, unknownWidth)...)
	header = append(header, "Hot food", "Collection")

	dataRows := make([][]interface{}, 0, len(publishedRota.Rows))
	for _, row := range publishedRota.Rows {
		sheetRow := []interface{}{row.Date}
		closedCellWritten := false
		fill := func(names []string, width int) {
			for i := 0; i < width; i++ {
				switch {
				case row.Closed && !closedCellWritten:
					// The closure is announced in the row's first cell, whatever
					// column that turns out to be.
					sheetRow = append(sheetRow, closedCell)
					closedCellWritten = true
				case i < len(names):
					sheetRow = append(sheetRow, names[i])
				default:
					sheetRow = append(sheetRow, "")
				}
			}
		}
		for _, name := range publishedRota.RoleNames {
			fill(row.Roles[name], roleWidths[name])
		}
		fill(row.UnknownRole, unknownWidth)
		sheetRow = append(sheetRow, row.HotFood, row.Collection)
		dataRows = append(dataRows, sheetRow)
	}

	allRows := [][]interface{}{{}, {}, header}
	return append(allRows, dataRows...)
}

// headings names one group of columns: the group's own name when it is a
// single column, numbered when there are several. Returns nothing for a group
// of no columns, which is how an unused Unknown role group disappears.
func headings(name string, width int) []interface{} {
	if width == 1 {
		return []interface{}{name}
	}
	out := make([]interface{}, 0, width)
	for i := 0; i < width; i++ {
		out = append(out, fmt.Sprintf("%s %d", name, i+1))
	}
	return out
}
