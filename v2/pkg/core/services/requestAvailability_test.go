package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

// Helper function tests
func TestFilterVolunteersWithoutRequests(t *testing.T) {
	volunteers := []model.Volunteer{
		{ID: "vol-1"},
		{ID: "vol-2"},
		{ID: "vol-3"},
	}
	withRequests := map[string]bool{
		"vol-1": true,
		"vol-3": true,
	}

	without := filterVolunteersWithoutRequests(volunteers, withRequests)

	require.Len(t, without, 1)
	assert.Equal(t, "vol-2", without[0].ID)
}

// TODO: Add integration tests with mocked Forms API client
