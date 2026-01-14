package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/sheetsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

func TestPublishRota_Success(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	// Setup mock store with rota and allocations
	store := &mockPublishRotaStore{
		rotations: []db.Rotation{
			{
				ID:         "rota-1",
				Start:      "2025-01-05", // Sunday, Jan 5, 2025
				ShiftCount: 2,
			},
		},
		allocations: []db.Allocation{
			// Shift 1 - Jan 5
			{ID: "alloc-1", RotaID: "rota-1", ShiftDate: "2025-01-05", Role: string(model.RoleTeamLead), VolunteerID: "alice"},
			{ID: "alloc-2", RotaID: "rota-1", ShiftDate: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
			{ID: "alloc-3", RotaID: "rota-1", ShiftDate: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "charlie"},
			// Shift 2 - Jan 12
			{ID: "alloc-4", RotaID: "rota-1", ShiftDate: "2025-01-12", Role: string(model.RoleTeamLead), VolunteerID: "dave"},
			{ID: "alloc-5", RotaID: "rota-1", ShiftDate: "2025-01-12", Role: string(model.RoleVolunteer), VolunteerID: "eve"},
		},
	}

	volunteerClient := &mockVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "Smith"},
			{ID: "bob", FirstName: "Bob", LastName: "Jones"},
			{ID: "charlie", FirstName: "Charlie", LastName: "Brown"},
			{ID: "dave", FirstName: "Dave", LastName: "Wilson"},
			{ID: "eve", FirstName: "Eve", LastName: "Davis"},
		},
	}

	cfg := &config.Config{}
	sheetsClient := &mockSheetsClient{}

	// Call PublishRota
	result, err := PublishRota(ctx, store, sheetsClient, volunteerClient, cfg, logger, "rota-1")
	require.NoError(t, err)
	require.NotNil(t, result)

	// Assertions
	assert.Equal(t, "2025-01-05", result.StartDate)
	assert.Equal(t, 2, result.ShiftCount)
	require.Len(t, result.Rows, 2)

	// Check first shift
	shift1 := result.Rows[0]
	assert.Equal(t, "Sun Jan 05 2025", shift1.Date)
	assert.Equal(t, "Alice Smith", shift1.TeamLead)
	assert.Len(t, shift1.Volunteers, 2)
	assert.Contains(t, shift1.Volunteers, "Bob Jones")
	assert.Contains(t, shift1.Volunteers, "Charlie Brown")
	assert.Equal(t, "", shift1.HotFood)
	assert.Equal(t, "", shift1.Collection)

	// Check second shift
	shift2 := result.Rows[1]
	assert.Equal(t, "Sun Jan 12 2025", shift2.Date)
	assert.Equal(t, "Dave Wilson", shift2.TeamLead)
	assert.Len(t, shift2.Volunteers, 1)
	assert.Contains(t, shift2.Volunteers, "Eve Davis")
}

func TestPublishRota_WithCustomEntries(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockPublishRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 1},
		},
		allocations: []db.Allocation{
			{ID: "alloc-1", RotaID: "rota-1", ShiftDate: "2025-01-05", Role: string(model.RoleTeamLead), VolunteerID: "alice"},
			{ID: "alloc-2", RotaID: "rota-1", ShiftDate: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
			// Custom entry (external volunteer)
			{ID: "alloc-3", RotaID: "rota-1", ShiftDate: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "", CustomEntry: "External John"},
		},
	}

	volunteerClient := &mockVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "Smith"},
			{ID: "bob", FirstName: "Bob", LastName: "Jones"},
		},
	}

	cfg := &config.Config{}
	sheetsClient := &mockSheetsClient{}

	result, err := PublishRota(ctx, store, sheetsClient, volunteerClient, cfg, logger, "rota-1")
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Len(t, result.Rows, 1)
	shift := result.Rows[0]
	assert.Equal(t, "Alice Smith", shift.TeamLead)
	assert.Len(t, shift.Volunteers, 2)
	assert.Contains(t, shift.Volunteers, "Bob Jones")
	assert.Contains(t, shift.Volunteers, "External John")
}

func TestPublishRota_VolunteersSorted(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockPublishRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 1},
		},
		allocations: []db.Allocation{
			{ID: "alloc-1", RotaID: "rota-1", ShiftDate: "2025-01-05", Role: string(model.RoleTeamLead), VolunteerID: "alice"},
			// Volunteers in reverse alphabetical order
			{ID: "alloc-2", RotaID: "rota-1", ShiftDate: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "zebra"},
			{ID: "alloc-3", RotaID: "rota-1", ShiftDate: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
			{ID: "alloc-4", RotaID: "rota-1", ShiftDate: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "mike"},
		},
	}

	volunteerClient := &mockVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "Smith"},
			{ID: "zebra", FirstName: "Zebra", LastName: "Last"},
			{ID: "bob", FirstName: "Bob", LastName: "Jones"},
			{ID: "mike", FirstName: "Mike", LastName: "Anderson"},
		},
	}

	cfg := &config.Config{}
	sheetsClient := &mockSheetsClient{}

	result, err := PublishRota(ctx, store, sheetsClient, volunteerClient, cfg, logger, "rota-1")
	require.NoError(t, err)
	require.Len(t, result.Rows, 1)

	// Volunteers should be sorted alphabetically
	volunteers := result.Rows[0].Volunteers
	require.Len(t, volunteers, 3)
	assert.Equal(t, "Bob Jones", volunteers[0])
	assert.Equal(t, "Mike Anderson", volunteers[1])
	assert.Equal(t, "Zebra Last", volunteers[2])
}

func TestPublishRota_RotaNotFound(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockPublishRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 1},
		},
	}

	volunteerClient := &mockVolClient{volunteers: []model.Volunteer{}}
	cfg := &config.Config{}
	sheetsClient := &mockSheetsClient{}

	// Try to publish non-existent rota
	result, err := PublishRota(ctx, store, sheetsClient, volunteerClient, cfg, logger, "rota-999")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "rota not found")
}

func TestPublishRota_NoAllocations(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockPublishRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 2},
		},
		allocations: []db.Allocation{}, // No allocations
	}

	volunteerClient := &mockVolClient{volunteers: []model.Volunteer{}}
	cfg := &config.Config{}
	sheetsClient := &mockSheetsClient{}

	result, err := PublishRota(ctx, store, sheetsClient, volunteerClient, cfg, logger, "rota-1")
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should have rows but with empty data
	require.Len(t, result.Rows, 2)
	assert.Equal(t, "", result.Rows[0].TeamLead)
	assert.Empty(t, result.Rows[0].Volunteers)
	assert.Equal(t, "", result.Rows[1].TeamLead)
	assert.Empty(t, result.Rows[1].Volunteers)
}

func TestPublishRota_MissingVolunteer(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockPublishRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 1},
		},
		allocations: []db.Allocation{
			{ID: "alloc-1", RotaID: "rota-1", ShiftDate: "2025-01-05", Role: string(model.RoleTeamLead), VolunteerID: "alice"},
			// Bob doesn't exist in volunteer list
			{ID: "alloc-2", RotaID: "rota-1", ShiftDate: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
			{ID: "alloc-3", RotaID: "rota-1", ShiftDate: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "charlie"},
		},
	}

	volunteerClient := &mockVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "Smith"},
			{ID: "charlie", FirstName: "Charlie", LastName: "Brown"},
			// Bob is missing
		},
	}

	cfg := &config.Config{}
	sheetsClient := &mockSheetsClient{}

	result, err := PublishRota(ctx, store, sheetsClient, volunteerClient, cfg, logger, "rota-1")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "volunteer not found")
	assert.Contains(t, err.Error(), "bob")
}

func TestPublishRota_DefaultsToLatestRota(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockPublishRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 1},
			{ID: "rota-2", Start: "2025-01-19", ShiftCount: 1}, // Latest rota
			{ID: "rota-3", Start: "2025-01-12", ShiftCount: 1},
		},
		allocations: []db.Allocation{
			{ID: "alloc-1", RotaID: "rota-2", ShiftDate: "2025-01-19", Role: string(model.RoleTeamLead), VolunteerID: "alice"},
			{ID: "alloc-2", RotaID: "rota-2", ShiftDate: "2025-01-19", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
		},
	}

	volunteerClient := &mockVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "Smith"},
			{ID: "bob", FirstName: "Bob", LastName: "Jones"},
		},
	}

	cfg := &config.Config{}
	sheetsClient := &mockSheetsClient{}

	// Call with empty rotaID to trigger default behavior
	result, err := PublishRota(ctx, store, sheetsClient, volunteerClient, cfg, logger, "")
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should use rota-2 (the latest)
	assert.Equal(t, "2025-01-19", result.StartDate)
	assert.Equal(t, 1, result.ShiftCount)
	require.Len(t, result.Rows, 1)
	assert.Equal(t, "Sun Jan 19 2025", result.Rows[0].Date)
	assert.Equal(t, "Alice Smith", result.Rows[0].TeamLead)
	assert.Contains(t, result.Rows[0].Volunteers, "Bob Jones")
}

func TestPublishRota_NoRotations(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockPublishRotaStore{
		rotations:   []db.Rotation{}, // No rotations
		allocations: []db.Allocation{},
	}

	volunteerClient := &mockVolClient{volunteers: []model.Volunteer{}}
	cfg := &config.Config{}
	sheetsClient := &mockSheetsClient{}

	result, err := PublishRota(ctx, store, sheetsClient, volunteerClient, cfg, logger, "")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no rotations found")
}

// mockPublishRotaStore implements PublishRotaStore for testing
type mockPublishRotaStore struct {
	rotations   []db.Rotation
	allocations []db.Allocation
}

func (m *mockPublishRotaStore) GetRotations(ctx context.Context) ([]db.Rotation, error) {
	return m.rotations, nil
}

func (m *mockPublishRotaStore) GetAllocations(ctx context.Context) ([]db.Allocation, error) {
	return m.allocations, nil
}

// mockSheetsClient implements SheetsClient for testing
type mockSheetsClient struct {
	publishRotaError error
}

func (m *mockSheetsClient) PublishRota(spreadsheetID string, publishedRota *sheetsclient.PublishedRota) error {
	return m.publishRotaError
}

func TestPublishRota_ClosedShifts(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockPublishRotaStore{
		rotations: []db.Rotation{
			{
				ID:         "rota-1",
				Start:      "2025-01-05", // Sunday, Jan 5, 2025
				ShiftCount: 3,
			},
		},
		allocations: []db.Allocation{
			// Shift 1 - Jan 5 (open shift)
			{ID: "alloc-1", RotaID: "rota-1", ShiftDate: "2025-01-05", Role: string(model.RoleTeamLead), VolunteerID: "alice"},
			{ID: "alloc-2", RotaID: "rota-1", ShiftDate: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
			// Shift 2 - Jan 12 (closed - no allocations in DB)
			// Shift 3 - Jan 19 (open shift)
			{ID: "alloc-3", RotaID: "rota-1", ShiftDate: "2025-01-19", Role: string(model.RoleTeamLead), VolunteerID: "charlie"},
			{ID: "alloc-4", RotaID: "rota-1", ShiftDate: "2025-01-19", Role: string(model.RoleVolunteer), VolunteerID: "dave"},
		},
	}

	volunteerClient := &mockVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "Smith"},
			{ID: "bob", FirstName: "Bob", LastName: "Jones"},
			{ID: "charlie", FirstName: "Charlie", LastName: "Brown"},
			{ID: "dave", FirstName: "Dave", LastName: "Wilson"},
		},
	}

	// Configure closed shift for Jan 12
	cfg := &config.Config{
		RotaOverrides: []config.RotaOverride{
			{
				RRule:  "FREQ=YEARLY;BYMONTH=1;BYMONTHDAY=12", // January 12 every year
				Closed: true,
			},
		},
	}
	sheetsClient := &mockSheetsClient{}

	result, err := PublishRota(ctx, store, sheetsClient, volunteerClient, cfg, logger, "rota-1")
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Len(t, result.Rows, 3)

	// Check first shift (open)
	shift1 := result.Rows[0]
	assert.Equal(t, "Sun Jan 05 2025", shift1.Date)
	assert.Equal(t, "Alice Smith", shift1.TeamLead)
	assert.Len(t, shift1.Volunteers, 1)
	assert.Contains(t, shift1.Volunteers, "Bob Jones")

	// Check second shift (closed)
	shift2 := result.Rows[1]
	assert.Equal(t, "Sun Jan 12 2025", shift2.Date)
	assert.Equal(t, "CLOSED", shift2.TeamLead, "Closed shift should display 'CLOSED' in TeamLead column")
	assert.Empty(t, shift2.Volunteers, "Closed shift should have no volunteers")
	assert.Equal(t, "", shift2.HotFood)
	assert.Equal(t, "", shift2.Collection)

	// Check third shift (open)
	shift3 := result.Rows[2]
	assert.Equal(t, "Sun Jan 19 2025", shift3.Date)
	assert.Equal(t, "Charlie Brown", shift3.TeamLead)
	assert.Len(t, shift3.Volunteers, 1)
	assert.Contains(t, shift3.Volunteers, "Dave Wilson")
}
