package dbtest

import (
	"context"
	"testing"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// SeedPatternStartFrom is the date the seeded Shift pattern counts from, a
// Thursday, so the first rota a test defines starts on Sunday 4 January 2026.
const SeedPatternStartFrom = "2026-01-01"

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

	err := database.SaveShiftPattern(context.Background(), db.RotaDefaults{
		ShiftPattern:       "RRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30",
		PatternStartFrom:   SeedPatternStartFrom,
		ShiftLengthMinutes: 120,
		ShiftTimezone:      "Europe/London",
	})
	if err != nil {
		t.Fatalf("failed to seed rota defaults: %v", err)
	}
}
