package devmode

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// RotaDefaultsSeedStore is the slice of the database the Rota Defaults seed
// needs.
type RotaDefaultsSeedStore interface {
	GetRotaDefaults(ctx context.Context) (db.RotaDefaults, error)
	SaveShiftPattern(ctx context.Context, defaults db.RotaDefaults) error
	UpdateAllocationSettings(ctx context.Context, edit func(current string) (string, error)) error
}

// seedRotaDefaults are the settings the dev stack starts with: the evening
// session the real Organisation runs, in the zone it runs in, counted from the
// day the stack is first started — so its first rota starts the coming Sunday.
func seedRotaDefaults(now time.Time) db.RotaDefaults {
	loc, err := time.LoadLocation(model.DefaultShiftTimezone)
	if err != nil {
		loc = time.UTC
	}
	return db.RotaDefaults{
		ShiftPattern:       "RRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30",
		PatternStartFrom:   now.In(loc).Format("2006-01-02"),
		ShiftLengthMinutes: 120,
		ShiftTimezone:      model.DefaultShiftTimezone,
	}
}

// seedAllocationSettings are the Allocation Rules the dev stack starts with: the
// three that were the solver's hardcoded default list before they became an
// Organiser's choice, at the frequency the config file used to carry. A gap of
// one is what no_back_to_back was. No monthly cap — one a month was regularly
// unsatisfiable at real volunteer numbers. The ids are fixed so a dev database
// reads the same on every machine.
var seedAllocationSettings = model.AllocationSettings{Rules: []model.AllocationRule{
	{ID: "seed-max-frequency", Kind: model.MaxFrequencyKind, Value: 34},
	{ID: "seed-shift-gap", Kind: model.ShiftGapKind, Value: 1},
	{ID: "seed-shift-requires", Kind: model.ShiftRequiresKind, Option: "Male"},
}}

// SeedRotaDefaults gives a dev database its Shift pattern and its allocation
// settings, once. No migration seeds them (ADR 0006) — they are an Organiser's to
// choose on the Settings screen — but the credential-free dev stack has no
// Organiser, and `scripts/dev-stack.sh start` is supposed to hand over an app that
// can allocate a rota.
//
// It is a seed, not a reset: settings that have already been set are left
// exactly as they are, so a pattern changed by hand survives a restart. Half-set
// settings count as set, because clearing half of somebody's edit is a stranger
// thing to do than leaving it.
func SeedRotaDefaults(ctx context.Context, store RotaDefaultsSeedStore) (bool, error) {
	existing, err := store.GetRotaDefaults(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to read rota defaults before seeding: %w", err)
	}
	if existing != (db.RotaDefaults{}) {
		return false, nil
	}

	if err := store.SaveShiftPattern(ctx, seedRotaDefaults(time.Now())); err != nil {
		return false, fmt.Errorf("failed to seed rota defaults: %w", err)
	}

	document, err := json.Marshal(seedAllocationSettings)
	if err != nil {
		return false, fmt.Errorf("failed to encode seed allocation settings: %w", err)
	}
	err = store.UpdateAllocationSettings(ctx, func(string) (string, error) {
		return string(document), nil
	})
	if err != nil {
		return false, fmt.Errorf("failed to seed allocation settings: %w", err)
	}

	return true, nil
}
