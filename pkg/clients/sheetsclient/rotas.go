package sheetsclient

import (
	"fmt"
	"time"

	"google.golang.org/api/sheets/v4"

	"github.com/jakechorley/ilford-drop-in/pkg/core/rotasheet"
)

const latestTabTitle = "Latest"

// ApplyRotaPlan carries out a publish on the Latest tab. What to do was decided
// by rotasheet.PlanEdits; this only says it in the Sheets API's words.
//
// Everything goes in one batchUpdate, which Sheets applies atomically: the
// archive copy, the column and row edits and the cell writes all land or none
// do. That matters because the app remembers the layout it published and diffs
// the next publish against it — a half-applied publish would leave the sheet
// and the record disagreeing, and every publish after it would edit the wrong
// columns.
//
// archiveTitle names the tab Latest is copied to when the plan archives it; a
// title already taken gets " (2)" and so on.
//
// If Latest is missing — deleted or renamed by hand — it is created and built
// from scratch whatever the plan said, and latestWasMissing reports it so the
// caller can say so.
func (c *Client) ApplyRotaPlan(spreadsheetID string, plan rotasheet.Plan, archiveTitle string) (latestWasMissing bool, err error) {
	spreadsheet, err := c.service.Spreadsheets.Get(spreadsheetID).Do()
	if err != nil {
		return false, fmt.Errorf("failed to get spreadsheet metadata: %w", err)
	}

	latestID, found := sheetIDByTitle(spreadsheet, latestTabTitle)

	var requests []*sheets.Request
	switch {
	case !found:
		latestID = unusedSheetID(spreadsheet)
		requests = append(requests, &sheets.Request{AddSheet: &sheets.AddSheetRequest{
			Properties: &sheets.SheetProperties{SheetId: latestID, Title: latestTabTitle},
		}})
		requests = append(requests, writeCells(latestID, 0, plan.Sheet.FullValues()))

	case plan.Rebuild:
		if plan.Archive && archiveTitle != "" {
			requests = append(requests, &sheets.Request{DuplicateSheet: &sheets.DuplicateSheetRequest{
				SourceSheetId: latestID,
				NewSheetName:  resolveUniqueTitle(spreadsheet, archiveTitle),
			}})
		}
		// Every value on the tab, not only the app's columns: a new rota's
		// Latest starts clean, and the archive holds what was there.
		requests = append(requests, &sheets.Request{UpdateCells: &sheets.UpdateCellsRequest{
			Range:  &sheets.GridRange{SheetId: latestID},
			Fields: "userEnteredValue",
		}})
		requests = append(requests, writeCells(latestID, 0, plan.Sheet.FullValues()))

	default:
		for _, op := range plan.Ops {
			requests = append(requests, structuralRequest(latestID, op))
		}
		requests = append(requests, writeCells(latestID, rotasheet.HeaderRow, plan.Sheet.OwnedValues()))
	}

	_, err = c.service.Spreadsheets.BatchUpdate(spreadsheetID, &sheets.BatchUpdateSpreadsheetRequest{
		Requests: requests,
	}).Do()
	if err != nil {
		return !found, fmt.Errorf("failed to update the %s tab: %w", latestTabTitle, err)
	}
	return !found, nil
}

func structuralRequest(sheetID int64, op rotasheet.Op) *sheets.Request {
	switch op.Kind {
	case rotasheet.InsertColumns:
		return &sheets.Request{InsertDimension: &sheets.InsertDimensionRequest{
			Range: columns(sheetID, op.Index, op.Count),
			// A new column takes the formatting of the one it follows, so a
			// Role growing looks like the Role it grew from.
			InheritFromBefore: op.Index > 0,
		}}
	case rotasheet.DeleteColumns:
		return &sheets.Request{DeleteDimension: &sheets.DeleteDimensionRequest{
			Range: columns(sheetID, op.Index, op.Count),
		}}
	case rotasheet.MoveColumns:
		return &sheets.Request{MoveDimension: &sheets.MoveDimensionRequest{
			Source:           columns(sheetID, op.Index, op.Count),
			DestinationIndex: int64(op.To),
		}}
	case rotasheet.MoveRows:
		return &sheets.Request{MoveDimension: &sheets.MoveDimensionRequest{
			Source: &sheets.DimensionRange{
				SheetId: sheetID, Dimension: "ROWS",
				StartIndex: int64(op.Index), EndIndex: int64(op.Index + op.Count),
			},
			DestinationIndex: int64(op.To),
		}}
	}
	panic(fmt.Sprintf("unknown rota sheet op %q", op.Kind))
}

func columns(sheetID int64, index, count int) *sheets.DimensionRange {
	return &sheets.DimensionRange{
		SheetId: sheetID, Dimension: "COLUMNS",
		StartIndex: int64(index), EndIndex: int64(index + count),
	}
}

// writeCells sets values from column A of startRow down. An empty string clears
// its cell, which is how a name that has left a shift disappears.
func writeCells(sheetID int64, startRow int, values [][]string) *sheets.Request {
	rows := make([]*sheets.RowData, 0, len(values))
	for _, row := range values {
		cells := make([]*sheets.CellData, 0, len(row))
		for _, value := range row {
			cell := &sheets.CellData{}
			if value != "" {
				v := value
				cell.UserEnteredValue = &sheets.ExtendedValue{StringValue: &v}
			}
			cells = append(cells, cell)
		}
		rows = append(rows, &sheets.RowData{Values: cells})
	}
	return &sheets.Request{UpdateCells: &sheets.UpdateCellsRequest{
		Start:  &sheets.GridCoordinate{SheetId: sheetID, RowIndex: int64(startRow)},
		Rows:   rows,
		Fields: "userEnteredValue",
	}}
}

func sheetIDByTitle(spreadsheet *sheets.Spreadsheet, title string) (int64, bool) {
	for _, sheet := range spreadsheet.Sheets {
		if sheet.Properties.Title == title {
			return sheet.Properties.SheetId, true
		}
	}
	return 0, false
}

// unusedSheetID picks an id for a tab about to be created in the same batch
// that writes to it — the API lets the caller choose one, which is what keeps
// creating Latest and filling it atomic.
func unusedSheetID(spreadsheet *sheets.Spreadsheet) int64 {
	var highest int64
	for _, sheet := range spreadsheet.Sheets {
		highest = max(highest, sheet.Properties.SheetId)
	}
	return highest + 1
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
	_, found := sheetIDByTitle(spreadsheet, title)
	return found
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
