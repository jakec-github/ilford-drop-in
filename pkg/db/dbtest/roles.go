package dbtest

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// SeedRoles gives a test database the two Roles the app ships with: one Team
// lead Seat ahead of the uncapped Service volunteer.
//
// No migration seeds Roles (ADR 0006), so a database straight out of New has
// none — and a drop-in with no Roles has nothing anyone can be allocated to.
// Every integration test that exercises a path naming a Role starts here, so
// the fixture is the same one everywhere rather than re-typed per test.
func SeedRoles(t *testing.T, database *db.DB) {
	t.Helper()
	ctx := context.Background()

	max := 1
	roles := []db.Role{
		{ID: uuid.New().String(), Name: "Team lead", Max: &max, Priority: 1, Colour: "violet"},
		{ID: uuid.New().String(), Name: "Service volunteer", Priority: 2, Colour: "teal"},
	}
	for _, role := range roles {
		if err := database.InsertRole(ctx, role); err != nil {
			t.Fatalf("failed to seed role %s: %v", role.Name, err)
		}
	}
}
