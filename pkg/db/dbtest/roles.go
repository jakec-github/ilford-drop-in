package dbtest

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// SeedRoles gives a test database the two Roles the app ships with: Team lead
// ahead of Service volunteer.
//
// No migration seeds Roles (ADR 0006), so a database straight out of New has
// none — and a drop-in with no Roles has nothing anyone can be allocated to.
// Every integration test that exercises a path naming a Role starts here, so
// the fixture is the same one everywhere rather than re-typed per test.
//
// It seeds Roles and nothing else. What a Shift asks for is the default Shape,
// which is a separate decision and has its own helper — a test about Roles
// should not silently acquire one.
func SeedRoles(t *testing.T, database *db.DB) {
	t.Helper()
	ctx := context.Background()

	roles := []db.Role{
		{ID: uuid.New().String(), Name: "Team lead", Priority: 1, Colour: "violet"},
		{ID: uuid.New().String(), Name: "Service volunteer", Priority: 2, Colour: "teal"},
	}
	for _, role := range roles {
		if err := database.InsertRole(ctx, role); err != nil {
			t.Fatalf("failed to seed role %s: %v", role.Name, err)
		}
	}
}

// SeedDefaultShape gives a test database the Shape the app ships with — one
// Team lead and four Service volunteers — against the Roles SeedRoles just
// inserted, which it reads back because a Seat is written against a Role id.
//
// It is separate from SeedRoles because it is a separate setting, and only the
// paths that ask what a Shift needs — allocation and the availability round —
// have anything to do with it.
func SeedDefaultShape(t *testing.T, database *db.DB) {
	t.Helper()
	ctx := context.Background()

	roles, err := database.ListRoles(ctx)
	if err != nil {
		t.Fatalf("failed to read roles before seeding the default shape: %v", err)
	}

	seats := map[string]int{"Team lead": 1, "Service volunteer": 4}
	shape := make([]db.DefaultShapeSeat, 0, len(roles))
	for _, role := range roles {
		if count, asked := seats[role.Name]; asked {
			shape = append(shape, db.DefaultShapeSeat{RoleID: role.ID, Seats: count})
		}
	}
	if len(shape) == 0 {
		t.Fatal("no roles to build a default shape from - call SeedRoles first")
	}

	if err := database.SaveDefaultShape(ctx, shape); err != nil {
		t.Fatalf("failed to seed the default shape: %v", err)
	}
}
