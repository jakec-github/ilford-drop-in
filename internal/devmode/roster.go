// Package devmode holds the stubs that let the server run with no Google
// credentials, for local development and unattended agent runs. Everything here
// is gated behind the devMode config block, which only the dev environment may
// carry (see internal/config).
package devmode

import (
	"encoding/csv"
	"fmt"
	"os"

	"github.com/jakechorley/ilford-drop-in/pkg/clients/sheetsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

// LoadVolunteers reads a volunteer roster from a CSV export of the volunteer
// sheet, standing in for the Sheets fetch the server normally does with its
// service account. The file's header row and columns are the sheet's — the same
// parser reads both, so a CSV that loads here is one the real sheet would
// accept, and display names are disambiguated across the roster identically.
func LoadVolunteers(path string) ([]model.Volunteer, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open volunteer CSV: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	// Hand-edited exports have ragged rows where trailing cells are blank. The
	// shared parser already handles short rows, so accept them rather than
	// failing the whole file.
	reader.FieldsPerRecord = -1

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read volunteer CSV %s: %w", path, err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("volunteer CSV %s is empty", path)
	}

	// The parser takes the shape the Sheets API returns, so widen the rows.
	raw := make([][]any, len(records))
	for i, record := range records {
		row := make([]any, len(record))
		for j, cell := range record {
			row[j] = cell
		}
		raw[i] = row
	}

	volunteers, err := sheetsclient.ParseVolunteers(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to parse volunteer CSV %s: %w", path, err)
	}
	sheetsclient.ComputeDisplayNames(volunteers)

	return volunteers, nil
}
