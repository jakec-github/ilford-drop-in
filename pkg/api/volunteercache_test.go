package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

func TestVolunteerStore_EmptyBeforeSync(t *testing.T) {
	store := NewVolunteerStore()

	volunteers, err := store.ListVolunteers(apiTestCfg, model.Roles{})
	require.NoError(t, err, "an unsynced store must serve empty, not error")
	assert.Empty(t, volunteers)
}

func TestVolunteerStore_ServesLastReplace(t *testing.T) {
	store := NewVolunteerStore()

	store.Replace(model.Roster{Volunteers: []model.Volunteer{
		{ID: "alice", DisplayName: "Alice"},
		{ID: "bob", DisplayName: "Bob"},
	}})

	volunteers, err := store.ListVolunteers(apiTestCfg, model.Roles{})
	require.NoError(t, err)
	require.Len(t, volunteers, 2)
	assert.Equal(t, "alice", volunteers[0].ID)
}

func TestVolunteerStore_ReplaceOverwritesWholesale(t *testing.T) {
	store := NewVolunteerStore()

	store.Replace(model.Roster{
		Volunteers:   []model.Volunteer{{ID: "alice"}, {ID: "bob"}},
		UnknownRoles: []model.UnknownRole{{Name: "Kitchen", Volunteers: 1}},
	})
	store.Replace(model.Roster{Volunteers: []model.Volunteer{{ID: "carol"}}})

	volunteers, err := store.ListVolunteers(apiTestCfg, model.Roles{})
	require.NoError(t, err)
	require.Len(t, volunteers, 1, "a sync replaces the roster, it does not merge")
	assert.Equal(t, "carol", volunteers[0].ID)
	assert.Empty(t, store.UnknownRoles(), "the names a sheet no longer uses go with it")
}

func TestVolunteerStore_ServesTheUnknownRolesOfTheLastSync(t *testing.T) {
	store := NewVolunteerStore()
	assert.Empty(t, store.UnknownRoles())

	store.Replace(model.Roster{UnknownRoles: []model.UnknownRole{{Name: "Kitchen", Volunteers: 2}}})
	assert.Equal(t, []model.UnknownRole{{Name: "Kitchen", Volunteers: 2}}, store.UnknownRoles())
}
