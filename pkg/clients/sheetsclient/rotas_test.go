package sheetsclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/sheets/v4"

	"github.com/jakechorley/ilford-drop-in/pkg/core/rotasheet"
)

func TestGenerateTabTitle(t *testing.T) {
	tests := []struct {
		name      string
		startDate string
		endDate   string
		want      string
		wantErr   bool
	}{
		{
			name:      "single shift",
			startDate: "2025-01-05",
			endDate:   "2025-01-05",
			want:      "Jan 05 - Jan 05",
			wantErr:   false,
		},
		{
			name:      "multiple shifts",
			startDate: "2025-08-24",
			endDate:   "2025-11-09",
			want:      "Aug 24 - Nov 09",
			wantErr:   false,
		},
		{
			name:      "two shifts",
			startDate: "2025-01-05",
			endDate:   "2025-01-12",
			want:      "Jan 05 - Jan 12",
			wantErr:   false,
		},
		{
			name:      "invalid start date",
			startDate: "invalid",
			endDate:   "2025-01-05",
			want:      "",
			wantErr:   true,
		},
		{
			name:      "invalid end date",
			startDate: "2025-01-05",
			endDate:   "invalid",
			want:      "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateTabTitle(tt.startDate, tt.endDate)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// spreadsheetWith is a spreadsheet holding the named tabs, numbered from 0.
func spreadsheetWith(titles ...string) *sheets.Spreadsheet {
	spreadsheet := &sheets.Spreadsheet{}
	for i, title := range titles {
		spreadsheet.Sheets = append(spreadsheet.Sheets, &sheets.Sheet{
			Properties: &sheets.SheetProperties{SheetId: int64(i), Title: title},
		})
	}
	return spreadsheet
}

func onePersonRota() rotasheet.Sheet {
	return rotasheet.Lay(rotasheet.Rota{
		ID:     "rota",
		Roles:  []rotasheet.Role{{ID: "lead", Name: "Team lead"}},
		Shifts: []rotasheet.Shift{{ID: "s1", Date: "Sun Jan 04 2026", Names: map[string][]string{"lead": {"Alice"}}}},
	})
}

// cells reads an UpdateCells request back as the strings it writes, "" for a
// cell it clears.
func cells(t *testing.T, request *sheets.Request) [][]string {
	t.Helper()
	require.NotNil(t, request.UpdateCells, "a cell write")
	var out [][]string
	for _, row := range request.UpdateCells.Rows {
		values := []string{}
		for _, cell := range row.Values {
			value := ""
			if cell.UserEnteredValue != nil {
				value = *cell.UserEnteredValue.StringValue
			}
			values = append(values, value)
		}
		out = append(out, values)
	}
	return out
}

// A new rota archives Latest under the old rota's dates, then clears it and
// builds it again — one batch, so the archive is taken before anything is
// cleared.
func TestRotaPlanRequestsArchiveAndRebuild(t *testing.T) {
	plan := rotasheet.Plan{Archive: true, Rebuild: true, Sheet: onePersonRota()}

	requests, missing := rotaPlanRequests(spreadsheetWith("Responses", "Latest"), plan, "Oct 06 - Dec 29")

	assert.False(t, missing)
	require.Len(t, requests, 3)
	assert.Equal(t, &sheets.DuplicateSheetRequest{SourceSheetId: 1, NewSheetName: "Oct 06 - Dec 29"}, requests[0].DuplicateSheet)
	assert.Equal(t, &sheets.UpdateCellsRequest{Range: &sheets.GridRange{SheetId: 1}, Fields: "userEnteredValue"}, requests[1].UpdateCells,
		"every value on the tab is cleared, not only the app's columns")
	assert.Equal(t, int64(0), requests[2].UpdateCells.Start.RowIndex)
	assert.Equal(t, onePersonRota().FullValues(), cells(t, requests[2]))
}

// An archive never overwrites a tab: a title already taken is numbered.
func TestRotaPlanRequestsNumberAnArchiveTitleAlreadyTaken(t *testing.T) {
	plan := rotasheet.Plan{Archive: true, Rebuild: true, Sheet: onePersonRota()}

	requests, _ := rotaPlanRequests(spreadsheetWith("Latest", "Oct 06 - Dec 29"), plan, "Oct 06 - Dec 29")

	assert.Equal(t, "Oct 06 - Dec 29 (2)", requests[0].DuplicateSheet.NewSheetName)
}

// With no rota to name it after — a deployment's first rota — Latest is still
// archived before it is cleared. Whatever is on it was somebody's.
func TestRotaPlanRequestsArchiveWithNoRotaToNameItAfter(t *testing.T) {
	plan := rotasheet.Plan{Archive: true, Rebuild: true, Sheet: onePersonRota()}

	requests, _ := rotaPlanRequests(spreadsheetWith("Latest"), plan, "")

	require.NotNil(t, requests[0].DuplicateSheet)
	assert.Equal(t, "Latest (archived)", requests[0].DuplicateSheet.NewSheetName)
}

// A rebuild that is not a new rota keeps no archive.
func TestRotaPlanRequestsRebuildWithoutArchive(t *testing.T) {
	plan := rotasheet.Plan{Rebuild: true, Sheet: onePersonRota()}

	requests, _ := rotaPlanRequests(spreadsheetWith("Latest"), plan, "")

	require.Len(t, requests, 2)
	assert.Nil(t, requests[0].DuplicateSheet)
}

// A missing Latest is created and built from scratch whatever the plan said,
// in the same batch — so its id is chosen here rather than by Google.
func TestRotaPlanRequestsCreateAMissingLatest(t *testing.T) {
	plan := rotasheet.Plan{Ops: []rotasheet.Op{{Kind: rotasheet.InsertColumns, Index: 2, Count: 1}}, Sheet: onePersonRota()}

	requests, missing := rotaPlanRequests(spreadsheetWith("Responses", "Oct 06 - Dec 29"), plan, "")

	assert.True(t, missing)
	require.Len(t, requests, 2)
	require.NotNil(t, requests[0].AddSheet)
	created := requests[0].AddSheet.Properties
	assert.Equal(t, "Latest", created.Title)
	assert.NotContains(t, []int64{0, 1}, created.SheetId, "an id no tab already has")
	assert.Equal(t, created.SheetId, requests[1].UpdateCells.Start.SheetId)
	assert.Equal(t, onePersonRota().FullValues(), cells(t, requests[1]))
}

// An edit in place is the plan's ops in order, then the app's columns
// rewritten from the header down — and nothing written to anybody else's.
func TestRotaPlanRequestsEditInPlace(t *testing.T) {
	plan := rotasheet.Plan{
		Ops: []rotasheet.Op{
			{Kind: rotasheet.DeleteColumns, Index: 3, Count: 1},
			{Kind: rotasheet.MoveColumns, Index: 2, Count: 2, To: 1},
			{Kind: rotasheet.InsertColumns, Index: 3, Count: 2},
			{Kind: rotasheet.MoveRows, Index: 5, Count: 1, To: 3},
		},
		Sheet: onePersonRota(),
	}

	requests, missing := rotaPlanRequests(spreadsheetWith("Latest"), plan, "")

	assert.False(t, missing)
	require.Len(t, requests, 5)
	assert.Equal(t, &sheets.DeleteDimensionRequest{Range: &sheets.DimensionRange{SheetId: 0, Dimension: "COLUMNS", StartIndex: 3, EndIndex: 4}}, requests[0].DeleteDimension)
	assert.Equal(t, &sheets.MoveDimensionRequest{Source: &sheets.DimensionRange{SheetId: 0, Dimension: "COLUMNS", StartIndex: 2, EndIndex: 4}, DestinationIndex: 1}, requests[1].MoveDimension)
	assert.Equal(t, &sheets.InsertDimensionRequest{Range: &sheets.DimensionRange{SheetId: 0, Dimension: "COLUMNS", StartIndex: 3, EndIndex: 5}, InheritFromBefore: true}, requests[2].InsertDimension)
	assert.Equal(t, &sheets.MoveDimensionRequest{Source: &sheets.DimensionRange{SheetId: 0, Dimension: "ROWS", StartIndex: 5, EndIndex: 6}, DestinationIndex: 3}, requests[3].MoveDimension)

	assert.Equal(t, int64(rotasheet.HeaderRow), requests[4].UpdateCells.Start.RowIndex)
	assert.Equal(t, onePersonRota().OwnedValues(), cells(t, requests[4]))
}

// An empty cell is written as a cleared one, which is how somebody who has left
// a shift disappears from it.
func TestRotaPlanRequestsClearEmptyCells(t *testing.T) {
	plan := rotasheet.Plan{Sheet: rotasheet.Sheet{Header: []string{"Date", "Team lead"}, Rows: [][]string{{"Sun Jan 04 2026", ""}}}}

	requests, _ := rotaPlanRequests(spreadsheetWith("Latest"), plan, "")

	cleared := requests[0].UpdateCells.Rows[1].Values[1]
	assert.Nil(t, cleared.UserEnteredValue)
	assert.Equal(t, "userEnteredValue", requests[0].UpdateCells.Fields, "so the cell is cleared rather than skipped")
}
