package devmode

import (
	"context"
	"fmt"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// RotaDefaultsSeedStore is the slice of the database the Rota Defaults seed
// needs.
type RotaDefaultsSeedStore interface {
	GetRotaDefaults(ctx context.Context) (db.RotaDefaults, error)
	SaveRotaDefaults(ctx context.Context, defaults db.RotaDefaults) error
}

// seedRotaDefaults are the settings the dev stack starts with: the evening
// session the real drop-in runs, in the zone it runs in.
var seedRotaDefaults = db.RotaDefaults{
	ShiftStartTime: "19:30",
	ShiftEndTime:   "21:30",
	ShiftTimezone:  model.DefaultShiftTimezone,
}

// SeedRotaDefaults gives a dev database its shift times, once. No migration
// seeds them (ADR 0006) — they are an admin's to choose on the Settings screen
// — but the credential-free dev stack has no admin, and `scripts/dev-stack.sh
// start` is supposed to hand over an app that can allocate a rota.
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
	return true, nil
}
