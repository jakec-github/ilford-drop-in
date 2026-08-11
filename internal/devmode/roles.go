package devmode

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// RoleSeedStore is the slice of the database the Role seed needs.
type RoleSeedStore interface {
	ListRoles(ctx context.Context) ([]db.Role, error)
	InsertRole(ctx context.Context, role db.Role) error
}

// seedRoles are the Roles the dev stack starts with. They are the pair
// test_data/volunteers.csv names in its Roles column, because a roster whose
// Roles the app does not know parses to volunteers holding nothing, and a
// drop-in nobody holds a Role in allocates nobody.
//
// Names and colours match the production config these Roles came out of, so the
// dev stack looks like the real thing rather than like a fixture. How many of
// each a Shift asks for is the default Shape's business, not a Role's.
var seedRoles = []model.Role{
	{Name: "Team lead", Priority: 1, Colour: model.ColourViolet},
	{Name: "Service volunteer", Priority: 2, Colour: model.ColourTeal},
}

// SeedRoles gives a dev database its Roles, once. No migration seeds Roles
// (ADR 0006) — they are an admin's to choose, and the settings screen that lets
// them is the next ticket — but the credential-free dev stack has no admin, and
// `scripts/dev-stack.sh start` is supposed to hand over a usable app.
//
// It is a seed, not a reset: a database that already has any Role is left
// exactly as it is, so a Role added or edited by hand survives a restart.
func SeedRoles(ctx context.Context, store RoleSeedStore) (int, error) {
	existing, err := store.ListRoles(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to read roles before seeding: %w", err)
	}
	if len(existing) > 0 {
		return 0, nil
	}

	for _, role := range seedRoles {
		if err := store.InsertRole(ctx, db.Role{
			ID:       uuid.New().String(),
			Name:     role.Name,
			Priority: role.Priority,
			Colour:   role.Colour,
		}); err != nil {
			return 0, fmt.Errorf("failed to seed role %q: %w", role.Name, err)
		}
	}

	return len(seedRoles), nil
}
