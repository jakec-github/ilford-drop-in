package devmode

import (
	"context"
	"fmt"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// DefaultShapeSeedStore is the slice of the database the default-Shape seed
// needs. It reads the Roles because a Seat is stored against a Role id, and the
// seed only knows the names.
type DefaultShapeSeedStore interface {
	ListRoles(ctx context.Context) ([]db.Role, error)
	GetDefaultShape(ctx context.Context) ([]db.DefaultShapeSeat, error)
	SaveDefaultShape(ctx context.Context, shape []db.DefaultShapeSeat) error
}

// seedShape is how many Seats of each Role the dev stack's Shifts ask for, by
// Role name: the shape of the real drop-in, one lead and four others.
var seedShape = map[string]int{
	"Team lead":         1,
	"Service volunteer": 4,
}

// SeedDefaultShape gives a dev database the Shape its Shifts ask for, once. No
// migration seeds one (ADR 0006) — it is an admin's to state on the Settings
// screen — but the credential-free dev stack has no admin, and a Shape asking
// for nobody is the one unset setting that would let a rota be allocated empty.
//
// It has its own guard rather than riding on SeedRoles, and the guard is this
// table's own emptiness: a dev database seeded before this ticket already has
// Roles, so a Shape hung off "are there Roles yet?" would never arrive.
//
// It is a seed, not a reset: a Shape that already has any Seat is left exactly
// as it is, so an edit made by hand survives a restart. A Role the seed names
// and the database does not have is skipped — the Roles are somebody's own by
// then, and this is not the place to add to them.
func SeedDefaultShape(ctx context.Context, store DefaultShapeSeedStore) (bool, error) {
	existing, err := store.GetDefaultShape(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to read the default shape before seeding: %w", err)
	}
	if len(existing) > 0 {
		return false, nil
	}

	roles, err := store.ListRoles(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to read roles before seeding the default shape: %w", err)
	}

	shape := make([]db.DefaultShapeSeat, 0, len(roles))
	for _, role := range roles {
		if seats, asked := seedShape[role.Name]; asked {
			shape = append(shape, db.DefaultShapeSeat{RoleID: role.ID, Seats: seats})
		}
	}
	if len(shape) == 0 {
		return false, nil
	}

	if err := store.SaveDefaultShape(ctx, shape); err != nil {
		return false, fmt.Errorf("failed to seed the default shape: %w", err)
	}
	return true, nil
}
