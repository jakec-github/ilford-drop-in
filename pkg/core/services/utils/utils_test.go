package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

func TestFilterActiveVolunteers(t *testing.T) {
	volunteers := []model.Volunteer{
		{ID: "vol-1", Status: "Active"},
		{ID: "vol-2", Status: "active"},
		{ID: "vol-3", Status: "Inactive"},
		{ID: "vol-4", Status: "ACTIVE"},
		{ID: "vol-5", Status: "On Leave"},
	}

	active := FilterActiveVolunteers(volunteers)

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

	ids := GetVolunteerIDs(volunteers)

	require.Len(t, ids, 3)
	assert.Equal(t, "vol-1", ids[0])
	assert.Equal(t, "vol-2", ids[1])
	assert.Equal(t, "vol-3", ids[2])
}

func TestGetVolunteerIDs_Empty(t *testing.T) {
	volunteers := []model.Volunteer{}

	ids := GetVolunteerIDs(volunteers)

	assert.Empty(t, ids)
}
