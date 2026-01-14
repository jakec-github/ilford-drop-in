package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

func TestFilterRequestsByRotaID(t *testing.T) {
	requests := []db.AvailabilityRequest{
		{ID: "req-1", RotaID: "rota-1", VolunteerID: "vol-1"},
		{ID: "req-2", RotaID: "rota-2", VolunteerID: "vol-2"},
		{ID: "req-3", RotaID: "rota-1", VolunteerID: "vol-3"},
	}

	filtered := filterRequestsByRotaID(requests, "rota-1")

	require.Len(t, filtered, 2)
	assert.Equal(t, "req-1", filtered[0].ID)
	assert.Equal(t, "req-3", filtered[1].ID)
}

func TestFilterActiveVolunteers(t *testing.T) {
	volunteers := []model.Volunteer{
		{ID: "vol-1", Status: "Active"},
		{ID: "vol-2", Status: "active"},
		{ID: "vol-3", Status: "Inactive"},
		{ID: "vol-4", Status: "ACTIVE"},
		{ID: "vol-5", Status: "On Leave"},
	}

	active := filterActiveVolunteers(volunteers)

	require.Len(t, active, 3)
	assert.Equal(t, "vol-1", active[0].ID)
	assert.Equal(t, "vol-2", active[1].ID)
	assert.Equal(t, "vol-4", active[2].ID)
}

func TestGetVolunteerIDs(t *testing.T) {
	volunteers := []model.Volunteer{
		{ID: "vol-1", FirstName: "John"},
		{ID: "vol-2", FirstName: "Jane"},
		{ID: "vol-3", FirstName: "Bob"},
	}

	ids := getVolunteerIDs(volunteers)

	require.Len(t, ids, 3)
	assert.Equal(t, "vol-1", ids[0])
	assert.Equal(t, "vol-2", ids[1])
	assert.Equal(t, "vol-3", ids[2])
}

func TestGetVolunteerIDs_Empty(t *testing.T) {
	volunteers := []model.Volunteer{}

	ids := getVolunteerIDs(volunteers)

	assert.Empty(t, ids)
}
