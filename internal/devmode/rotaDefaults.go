package devmode

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// RotaDefaultsSeedStore is the slice of the database the Rota Defaults seed
// needs.
type RotaDefaultsSeedStore interface {
	GetRotaDefaults(ctx context.Context) (db.RotaDefaults, error)
	SaveRotaDefaults(ctx context.Context, defaults db.RotaDefaults) error
	SaveAllocationSettings(ctx context.Context, settings string) error
}

// seedRotaDefaults are the settings the dev stack starts with: the evening
// session the real drop-in runs, in the zone it runs in.
var seedRotaDefaults = db.RotaDefaults{
	ShiftStartTime: "19:30",
	ShiftEndTime:   "21:30",
	ShiftTimezone:  model.DefaultShiftTimezone,
}

// seedAllocationSettings are the optional allocator rules the dev stack starts
// with: the three that were the solver's hardcoded default list before they
// became an admin's choice, at the frequency the config file used to carry.
// one_shift_per_month stays off, as it always was — it is regularly
// unsatisfiable at real volunteer numbers.
var seedAllocationSettings = model.AllocationSettings{
	Enabled: map[string]bool{
		model.MaxFrequencyConstraint: true,
		"male_required":              true,
		"no_back_to_back":            true,
	},
	MaxFrequency: 0.34,
}

// SeedRotaDefaults gives a dev database its shift times and its allocation
// settings, once. No migration seeds them (ADR 0006) — they are an admin's to
// choose on the Settings screen — but the credential-free dev stack has no
// admin, and `scripts/dev-stack.sh start` is supposed to hand over an app that
// can allocate a rota.
//
// It is a seed, not a reset: settings that have already been set are left
// exactly as they are, so a time changed by hand survives a restart. Half-set
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

	if err := store.SaveRotaDefaults(ctx, seedRotaDefaults); err != nil {
		return false, fmt.Errorf("failed to seed rota defaults: %w", err)
	}

	document, err := json.Marshal(seedAllocationSettings)
	if err != nil {
		return false, fmt.Errorf("failed to encode seed allocation settings: %w", err)
	}
	if err := store.SaveAllocationSettings(ctx, string(document)); err != nil {
		return false, fmt.Errorf("failed to seed allocation settings: %w", err)
	}

	return true, nil
}
