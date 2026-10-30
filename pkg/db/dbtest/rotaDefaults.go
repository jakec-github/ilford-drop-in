package dbtest

import (
	"context"
	"testing"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// SeedPattern is the Shift pattern a test database runs to: every Sunday at
// 19:30 for two hours, the first on Sunday 4 January 2026, which is where the
// first rota a test defines starts.
const SeedPattern = "BEGIN:VEVENT\nDTSTART:20260104T193000\nDURATION:PT2H\nRRULE:FREQ=WEEKLY;BYDAY=SU\nEND:VEVENT"

// SeedRotaDefaults gives a test database the Shift pattern an Organisation
// runs to: every Sunday at 19:30, for two hours.
//
// No migration seeds it (ADR 0006), so a database straight out of New has an
// empty settings record, and a rota cannot be defined against one (ADR 0013).
// Every integration test that mints a rota starts here, so the hours are the
// same everywhere rather than re-typed per test, and they match the times
// dbtest.Shift builds a fixture at.
func SeedRotaDefaults(t *testing.T, database *db.DB) {
	t.Helper()

	ctx := context.Background()
	if err := database.SaveTimezone(ctx, "Europe/London"); err != nil {
		t.Fatalf("failed to seed the time zone: %v", err)
	}
	if err := database.SaveShiftPattern(ctx, SeedPattern); err != nil {
		t.Fatalf("failed to seed rota defaults: %v", err)
	}
}
