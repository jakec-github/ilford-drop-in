package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// TestAllocateRotaRefusesAlreadyAllocatedRota covers the double-allocation
// guard (issue #8): allocating a rota that has already been allocated (a set
// allocated_datetime) fails fast, naming the rota, before the solver runs and
// without writing anything.
func TestAllocateRotaRefusesAlreadyAllocatedRota(t *testing.T) {
	// Give the rota real shifts too, so that without the guard the flow would
	// proceed past the shift lookup and fail with a different error — pinning the
	// test to the guard rather than an incidental downstream failure.
	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2026-08-02", ShiftCount: 2, AllocatedDatetime: "2026-08-01T10:00:00Z"},
		},
		shifts: sundayShifts("rota-1", "2026-08-02", 2),
	}

	result, err := AllocateRota(
		context.Background(),
		store,
		&mockVolClient{},
		&mockFormsClientWithResponses{},
		&config.Config{},
		zap.NewNop(),
		false, // dryRun
		false, // forceCommit
		"",    // pythonFlag
	)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "rota-1", "error should name the rota")
	assert.Contains(t, err.Error(), "already allocated", "error should explain the guard fired")
	assert.Empty(t, store.insertedAllocations, "no allocations should be written")
}
