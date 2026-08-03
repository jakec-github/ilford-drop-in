package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptr(i int) *int { return &i }

func TestNewRoles_OrdersByPriority(t *testing.T) {
	roles := NewRoles([]Role{
		{Name: "Service volunteer", Priority: 4},
		{Name: "Team lead", Max: ptr(1), Priority: 1},
		{Name: "Food collector", Max: ptr(1), Priority: 3},
	})

	names := make([]string, 0, 3)
	for _, r := range roles.ByPriority() {
		names = append(names, r.Name)
	}
	assert.Equal(t, []string{"Team lead", "Food collector", "Service volunteer"}, names)
}

func TestRoles_ByName(t *testing.T) {
	roles := NewRoles([]Role{
		{Name: "Team lead", Max: ptr(1), Priority: 1},
		{Name: "Service volunteer", Priority: 2},
	})

	lead, ok := roles.ByName("Team lead")
	require.True(t, ok)
	require.NotNil(t, lead.Max)
	assert.Equal(t, 1, *lead.Max)

	_, ok = roles.ByName("Hot food")
	assert.False(t, ok, "a Role config does not name is not a Role")
}

func TestRoles_Uncapped(t *testing.T) {
	roles := NewRoles([]Role{
		{Name: "Team lead", Max: ptr(1), Priority: 1},
		{Name: "Service volunteer", Priority: 2},
	})

	uncapped, ok := roles.Uncapped()
	require.True(t, ok)
	assert.Equal(t, "Service volunteer", uncapped.Name)

	_, ok = NewRoles([]Role{{Name: "Team lead", Max: ptr(1), Priority: 1}}).Uncapped()
	assert.False(t, ok)
}

// The zero Roles is what a caller with no config in scope holds; it must answer
// rather than panic.
func TestRoles_ZeroValue(t *testing.T) {
	var roles Roles

	_, ok := roles.ByName("Team lead")
	assert.False(t, ok)
	_, ok = roles.Uncapped()
	assert.False(t, ok)
	assert.Empty(t, roles.ByPriority())
}

// ByPriority hands out the ordering, not the storage behind it.
func TestRoles_ByPriorityDoesNotAliasStorage(t *testing.T) {
	roles := NewRoles([]Role{
		{Name: "Team lead", Max: ptr(1), Priority: 1},
		{Name: "Service volunteer", Priority: 2},
	})

	roles.ByPriority()[0] = Role{Name: "Tampered"}

	assert.Equal(t, "Team lead", roles.ByPriority()[0].Name)
}
