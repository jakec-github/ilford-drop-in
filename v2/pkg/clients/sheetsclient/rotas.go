package sheetsclient

import (
	"fmt"
	"strings"
	"time"

	"google.golang.org/api/sheets/v4"
)

// PublishedRotaRow represents a single row in the published rota
type PublishedRotaRow struct {
	Date       string   // Format: "Mon Jan 02 2006"
	TeamLead   string   // Full name of team lead
	Volunteers []string // Full names of volunteers
	HotFood    string   // Leave blank for now
	Collection string   // Leave blank for now
}

// PublishedRota represents the complete published rota data
type PublishedRota struct {
	StartDate  string // Format: "2006-01-02"
	ShiftCount int
	Rows       []PublishedRotaRow
}

// PublishRota publishes a rota to Google Sheets
// If the tab doesn't exist, it creates a new tab with the format "Sun Aug 24 2025 - Sun Nov 09 2025"
// If the tab exists, it overwrites the Date, Team lead, and Volunteer columns while preserving
// Hot food, Collection, and any other custom columns
func (c *Client) PublishRota(
	spreadsheetID string,
	publishedRota *PublishedRota,
) error {
	// Calculate date range for tab title
	tabTitle, err := generateTabTitle(publishedRota.StartDate, publishedRota.ShiftCount)
	if err != nil {
		return fmt.Errorf("failed to generate tab title: %w", err)
	}

	// Check if tab exists
	spreadsheet, err := c.service.Spreadsheets.Get(spreadsheetID).Do()
	if err != nil {
		return fmt.Errorf("failed to get spreadsheet metadata: %w", err)
	}

	var existingSheet *sheets.Sheet
	for _, sheet := range spreadsheet.Sheets {
		if sheet.Properties.Title == tabTitle {
			existingSheet = sheet
			break
		}
	}

	if existingSheet == nil {
		// Create new tab
		return c.createNewRotaTab(spreadsheetID, tabTitle, publishedRota)
	}

	// Update existing tab
	return c.updateExistingRotaTab(spreadsheetID, tabTitle, publishedRota)
}

// generateTabTitle creates a tab title in the format "Sun Aug 24 2025 - Sun Nov 09 2025"
func generateTabTitle(startDate string, shiftCount int) (string, error) {
	// Parse start date
	start, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		return "", fmt.Errorf("invalid start date: %w", err)
	}

	// Calculate end date (start + (shiftCount-1) * 7 days)
	end := start.AddDate(0, 0, (shiftCount-1)*7)

	// Format: "Mon Jan 02 2006"
	return fmt.Sprintf("%s - %s",
		start.Format("Mon Jan 02 2006"),
		end.Format("Mon Jan 02 2006"),
	), nil
}

// createNewRotaTab creates a new tab and writes the rota data with a 2-row gap at the top
func (c *Client) createNewRotaTab(
	spreadsheetID string,
	tabTitle string,
	publishedRota *PublishedRota,
) error {
	// Create the tab
	_, err := c.CreateSheet(spreadsheetID, tabTitle)
	if err != nil {
		return fmt.Errorf("failed to create tab: %w", err)
	}

	// Find the maximum number of volunteers in any shift
	maxVolunteers := 0
	for _, row := range publishedRota.Rows {
		if len(row.Volunteers) > maxVolunteers {
			maxVolunteers = len(row.Volunteers)
		}
	}

	// Build header row
	header := []interface{}{"Date", "Team lead"}
	for i := 0; i < maxVolunteers; i++ {
		header = append(header, fmt.Sprintf("Volunteer %d", i+1))
	}
	header = append(header, "Hot food", "Collection")

	// Build data rows
	dataRows := make([][]interface{}, 0, len(publishedRota.Rows))
	for _, row := range publishedRota.Rows {
		sheetRow := []interface{}{row.Date, row.TeamLead}

		// Add volunteers
		for i := 0; i < maxVolunteers; i++ {
			if i < len(row.Volunteers) {
				sheetRow = append(sheetRow, row.Volunteers[i])
			} else {
				sheetRow = append(sheetRow, "")
			}
		}

		// Add Hot food and Collection (empty for now)
		sheetRow = append(sheetRow, row.HotFood, row.Collection)
		dataRows = append(dataRows, sheetRow)
	}

	// Write to sheet with 2-row gap (write starting at A3)
	allRows := [][]interface{}{
		{}, // Row 1 (empty)
		{}, // Row 2 (empty)
		header,
	}
	allRows = append(allRows, dataRows...)

	valueRange := &sheets.ValueRange{
		Values: allRows,
	}

	_, err = c.service.Spreadsheets.Values.Update(
		spreadsheetID,
		fmt.Sprintf("%s!A1", tabTitle),
		valueRange,
	).ValueInputOption("RAW").Do()
	if err != nil {
		return fmt.Errorf("failed to write data to new tab: %w", err)
	}

	return nil
}

// updateExistingRotaTab updates an existing tab, preserving Hot food, Collection, and other columns
func (c *Client) updateExistingRotaTab(
	spreadsheetID string,
	tabTitle string,
	publishedRota *PublishedRota,
) error {
	// Read existing data to determine current structure
	existingRange := fmt.Sprintf("%s!A1:ZZ", tabTitle)
	existingData, err := c.GetValues(spreadsheetID, existingRange)
	if err != nil {
		return fmt.Errorf("failed to read existing tab data: %w", err)
	}

	if len(existingData) < 3 {
		return fmt.Errorf("existing tab has insufficient rows (expected at least 3 rows with 2-row gap)")
	}

	// Header is at row 3 (index 2)
	existingHeader := existingData[2]

	// Find column indices
	dateCol := findColumnIndex(existingHeader, "Date")
	teamLeadCol := findColumnIndex(existingHeader, "Team lead")
	hotFoodCol := findColumnIndex(existingHeader, "Hot food")
	collectionCol := findColumnIndex(existingHeader, "Collection")

	if dateCol == -1 || teamLeadCol == -1 {
		return fmt.Errorf("existing tab missing required columns (Date or Team lead)")
	}

	// Find existing volunteer columns
	volunteerCols := findVolunteerColumns(existingHeader)

	// Determine how many volunteer columns we need
	maxVolunteers := 0
	for _, row := range publishedRota.Rows {
		if len(row.Volunteers) > maxVolunteers {
			maxVolunteers = len(row.Volunteers)
		}
	}

	// Build new header if we need more volunteer columns
	needsHeaderUpdate := maxVolunteers > len(volunteerCols)
	var newHeader []interface{}

	if needsHeaderUpdate {
		// Rebuild header with enough volunteer columns
		newHeader = []interface{}{"Date", "Team lead"}
		for i := 0; i < maxVolunteers; i++ {
			newHeader = append(newHeader, fmt.Sprintf("Volunteer %d", i+1))
		}

		// Preserve Hot food and Collection columns
		if hotFoodCol != -1 {
			newHeader = append(newHeader, "Hot food")
		}
		if collectionCol != -1 {
			newHeader = append(newHeader, "Collection")
		}

		// Preserve any additional columns beyond Collection
		if collectionCol != -1 {
			for i := collectionCol + 1; i < len(existingHeader); i++ {
				if val, ok := existingHeader[i].(string); ok && val != "" {
					newHeader = append(newHeader, val)
				}
			}
		} else if hotFoodCol != -1 {
			for i := hotFoodCol + 1; i < len(existingHeader); i++ {
				if val, ok := existingHeader[i].(string); ok && val != "" {
					newHeader = append(newHeader, val)
				}
			}
		}
	}

	// Build data rows
	dataRows := make([][]interface{}, 0, len(publishedRota.Rows))
	for rowIdx, row := range publishedRota.Rows {
		// Start with existing row data if it exists
		var existingRow []interface{}
		if rowIdx+3 < len(existingData) {
			existingRow = existingData[rowIdx+3]
		}

		// Build new row
		var sheetRow []interface{}
		if needsHeaderUpdate {
			// Rebuild entire row based on new header
			sheetRow = []interface{}{row.Date, row.TeamLead}

			// Add volunteers
			for i := 0; i < maxVolunteers; i++ {
				if i < len(row.Volunteers) {
					sheetRow = append(sheetRow, row.Volunteers[i])
				} else {
					sheetRow = append(sheetRow, "")
				}
			}

			// Preserve Hot food value
			if hotFoodCol != -1 && hotFoodCol < len(existingRow) {
				sheetRow = append(sheetRow, existingRow[hotFoodCol])
			} else {
				sheetRow = append(sheetRow, "")
			}

			// Preserve Collection value
			if collectionCol != -1 && collectionCol < len(existingRow) {
				sheetRow = append(sheetRow, existingRow[collectionCol])
			} else {
				sheetRow = append(sheetRow, "")
			}

			// Preserve additional column values
			startCol := collectionCol
			if startCol == -1 {
				startCol = hotFoodCol
			}
			if startCol != -1 {
				for i := startCol + 1; i < len(existingRow); i++ {
					sheetRow = append(sheetRow, existingRow[i])
				}
			}
		} else {
			// Update only the relevant columns
			sheetRow = make([]interface{}, len(existingHeader))

			// Copy existing row
			for i := range existingHeader {
				if i < len(existingRow) {
					sheetRow[i] = existingRow[i]
				} else {
					sheetRow[i] = ""
				}
			}

			// Update Date and Team lead
			sheetRow[dateCol] = row.Date
			sheetRow[teamLeadCol] = row.TeamLead

			// Update volunteers
			for i, vol := range row.Volunteers {
				if i < len(volunteerCols) {
					sheetRow[volunteerCols[i]] = vol
				}
			}

			// Clear any volunteer columns beyond what we have
			for i := len(row.Volunteers); i < len(volunteerCols); i++ {
				sheetRow[volunteerCols[i]] = ""
			}
		}

		dataRows = append(dataRows, sheetRow)
	}

	// Write updated data
	allRows := [][]interface{}{
		{}, // Row 1 (empty)
		{}, // Row 2 (empty)
	}

	if needsHeaderUpdate {
		allRows = append(allRows, newHeader)
	} else {
		allRows = append(allRows, existingHeader)
	}

	allRows = append(allRows, dataRows...)

	valueRange := &sheets.ValueRange{
		Values: allRows,
	}

	_, err = c.service.Spreadsheets.Values.Update(
		spreadsheetID,
		fmt.Sprintf("%s!A1", tabTitle),
		valueRange,
	).ValueInputOption("RAW").Do()
	if err != nil {
		return fmt.Errorf("failed to update existing tab: %w", err)
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
