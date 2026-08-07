package dbtest

import (
	"context"
	"testing"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// SeedRotaDefaults gives a test database the shift times a drop-in runs at.
//
// No migration seeds them (ADR 0006), so a database straight out of New has an
// empty settings record — and since a Shift's date is the date of its start
// (ADR 0007), a rota cannot be defined against one. Every integration test that
// mints a rota starts here, so the hours are the same everywhere rather than
// re-typed per test, and they match the times dbtest.Shift builds a fixture at.
func SeedRotaDefaults(t *testing.T, database *db.DB) {
	t.Helper()

	err := database.SaveRotaDefaults(context.Background(), db.RotaDefaults{
		ShiftStartTime: "19:30",
		ShiftEndTime:   "21:30",
		ShiftTimezone:  "Europe/London",
	})
	if err != nil {
		t.Fatalf("failed to seed rota defaults: %v", err)
	}
}
