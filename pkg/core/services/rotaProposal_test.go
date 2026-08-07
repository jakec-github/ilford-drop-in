package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// mockProposalStore is a deployment as the define form finds it: the rotas that
// exist and the settings the form starts from.
type mockProposalStore struct {
	testRoleStore

	rotations []db.Rotation
	defaults  db.RotaDefaults
	shape     []db.DefaultShapeSeat

	defaultsErr error
	rotationErr error
}

func (m *mockProposalStore) GetRotaDefaults(ctx context.Context) (db.RotaDefaults, error) {
	if m.defaultsErr != nil {
		return db.RotaDefaults{}, m.defaultsErr
	}
	return m.defaults, nil
}

func (m *mockProposalStore) GetDefaultShape(ctx context.Context) ([]db.DefaultShapeSeat, error) {
	return m.shape, nil
}

func (m *mockProposalStore) GetRotations(ctx context.Context) ([]db.Rotation, error) {
	if m.rotationErr != nil {
		return nil, m.rotationErr
	}
	return m.rotations, nil
}

// configured is a deployment an admin has told when the drop-in runs and what a
// shift asks for.
func configured() *mockProposalStore {
	return &mockProposalStore{
		defaults: db.RotaDefaults{ShiftStartTime: "19:30", ShiftEndTime: "21:30", ShiftTimezone: "Europe/London"},
		shape: []db.DefaultShapeSeat{
			{RoleID: "role-team-lead", Seats: 1},
			{RoleID: "role-service-volunteer", Seats: 4},
		},
	}
}

// The proposal is the Rota Defaults plus the one thing they do not hold: where
// the next rota would begin.
func TestProposeRota_FollowsTheLatestRota(t *testing.T) {
	store := configured()
	store.rotations = []db.Rotation{
		{ID: "older", Start: "2026-06-07", End: "2026-06-28", ShiftCount: 4, AllocatedDatetime: "2026-05-31T09:00:00Z"},
		{ID: "latest", Start: "2026-07-05", End: "2026-07-26", ShiftCount: 4, AllocatedDatetime: "2026-06-28T09:00:00Z"},
	}

	proposal, err := ProposeRota(context.Background(), store)
	require.NoError(t, err)

	assert.Equal(t, "2026-08-02", proposal.StartDate, "the Sunday after the latest rota's last shift")
	assert.Equal(t, "19:30", proposal.ShiftStartTime)
	assert.Equal(t, "21:30", proposal.ShiftEndTime)
	require.Len(t, proposal.Shape, 2)
	assert.Equal(t, "Team lead", proposal.Shape[0].Role.Name, "in the order the Seats are filled")
	assert.Equal(t, 1, proposal.Shape[0].Count)
	assert.Equal(t, "Service volunteer", proposal.Shape[1].Role.Name)
	assert.Equal(t, 4, proposal.Shape[1].Count)
}

// A deployment with no rotas at all starts from the next Sunday, which is the
// only answer available: there is nothing to count forward from.
func TestProposeRota_WithNoRotas(t *testing.T) {
	proposal, err := ProposeRota(context.Background(), configured())
	require.NoError(t, err)

	start, err := time.Parse("2006-01-02", proposal.StartDate)
	require.NoError(t, err)
	assert.Equal(t, time.Sunday, start.Weekday())
	assert.True(t, start.After(time.Now()), "the next Sunday, never today")
}

// A rota in flight is still a rota to count forward from. The proposal is
// arithmetic rather than permission — defining is what refuses, and the screen
// shows the rota in flight rather than the form — so this answers either way.
func TestProposeRota_AnswersWhileARotaIsInFlight(t *testing.T) {
	store := configured()
	store.rotations = []db.Rotation{
		{ID: "in-flight", Start: "2026-07-05", End: "2026-07-26", ShiftCount: 4},
	}

	proposal, err := ProposeRota(context.Background(), store)
	require.NoError(t, err)
	assert.Equal(t, "2026-08-02", proposal.StartDate)
}

// Settings nobody has filled in are the ordinary first state of a deployment,
// not a refusal: the form renders empty boxes and an admin fills them in
// (ADR 0006). Defining is where blank hours are turned away.
func TestProposeRota_UnsetSettings(t *testing.T) {
	proposal, err := ProposeRota(context.Background(), &mockProposalStore{})
	require.NoError(t, err)

	assert.Empty(t, proposal.ShiftStartTime)
	assert.Empty(t, proposal.ShiftEndTime)
	assert.Empty(t, proposal.Shape)
	assert.NotEmpty(t, proposal.StartDate, "a date is arithmetic, and needs no settings")
}

// Settings that cannot be read are a fault rather than an empty answer.
func TestProposeRota_ReadFailures(t *testing.T) {
	_, err := ProposeRota(context.Background(), &mockProposalStore{defaultsErr: errors.New("boom")})
	assert.Error(t, err)

	_, err = ProposeRota(context.Background(), &mockProposalStore{rotationErr: errors.New("boom")})
	assert.Error(t, err)
}

// A rota that ended on a Sunday is followed by the week after, never by the day
// it finished — the case a naive "days until Sunday" gets wrong.
func TestNextSunday(t *testing.T) {
	for _, tt := range []struct {
		name     string
		from     time.Time
		expected string
	}{
		{"from Tuesday", time.Date(2025, 10, 7, 15, 30, 0, 0, time.UTC), "2025-10-12"},
		{"from Sunday", time.Date(2025, 10, 5, 10, 0, 0, 0, time.UTC), "2025-10-12"},
		{"from Saturday", time.Date(2025, 10, 11, 0, 0, 0, 0, time.UTC), "2025-10-12"},
		{"from late Saturday night", time.Date(2025, 10, 11, 23, 59, 59, 0, time.UTC), "2025-10-12"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := nextSunday(tt.from)
			assert.Equal(t, tt.expected, result.Format("2006-01-02"))
			assert.Equal(t, time.Sunday, result.Weekday())
		})
	}
}
