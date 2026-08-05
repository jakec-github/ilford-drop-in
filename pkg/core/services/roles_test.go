package services

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

type stubRoleStore struct {
	roles []db.Role
	err   error
}

func (s stubRoleStore) ListRoles(context.Context) ([]db.Role, error) {
	return s.roles, s.err
}

// RoleTable is the one place database rows become the domain's lookup table, so
// it owns the whole conversion: the ordering, the identity, and the default
// colour a Role stored without one is drawn in.
func TestRoleTable(t *testing.T) {
	store := stubRoleStore{roles: []db.Role{
		{ID: "r-service", Name: "Service volunteer", Priority: 2, Colour: "teal"},
		{ID: "r-lead", Name: "Team lead", Max: intPtr(1), Priority: 1, Colour: "violet"},
		{ID: "r-food", Name: "Food collector", Max: intPtr(2), Priority: 3},
	}}

	roles, err := RoleTable(context.Background(), store)
	require.NoError(t, err)

	lead, ok := roles.ByName("Team lead")
	require.True(t, ok)
	assert.Equal(t, "r-lead", lead.ID, "the id is what a rename cannot break")
	require.NotNil(t, lead.Max)
	assert.Equal(t, 1, *lead.Max)

	uncapped, ok := roles.Uncapped()
	require.True(t, ok)
	assert.Equal(t, "Service volunteer", uncapped.Name)

	assert.Equal(t, []string{"Team lead", "Service volunteer", "Food collector"},
		roleNames(roles.ByPriority()), "in the order their Seats are filled")

	assert.Equal(t, "slate", roles.ByPriority()[2].Colour,
		"a Role stored without a colour reads back as the default, never as empty")
}

// A database nobody has created Roles in is what a fresh deployment looks like.
// It answers the empty table rather than an error, so the pages that merely name
// Roles still render; allocation is the path that refuses.
func TestRoleTableWithNoRoles(t *testing.T) {
	roles, err := RoleTable(context.Background(), stubRoleStore{})
	require.NoError(t, err)
	assert.Empty(t, roles.ByPriority())

	_, ok := roles.ByName("Team lead")
	assert.False(t, ok)
}

// A failed read is not the same as no Roles, and collapsing the two would show
// an admin an empty settings screen when the database is unreachable.
func TestRoleTableReportsAReadFailure(t *testing.T) {
	_, err := RoleTable(context.Background(), stubRoleStore{err: errors.New("connection refused")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

func roleNames(roles []model.Role) []string {
	names := make([]string, 0, len(roles))
	for _, role := range roles {
		names = append(names, role.Name)
	}
	return names
}
