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

// Incomplete settings block allocation and nothing else (ADR 0006). The refusal
// has to name what is missing: an admin who has not filled the settings in has
// nothing else telling them which box is empty, and this fires before the
// solver so nothing is written.
func TestAllocateRotaRefusesWhenTheShiftTimesAreNotSet(t *testing.T) {
	cases := map[string]struct {
		defaults db.RotaDefaults
		names    []string
	}{
		"nothing set": {
			defaults: db.RotaDefaults{},
			names:    []string{"start time", "end time"},
		},
		"only the start set": {
			defaults: db.RotaDefaults{ShiftStartTime: "19:30"},
			names:    []string{"end time"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			store := &mockAllocateRotaStore{
				rotations: []db.Rotation{{ID: "rota-1", Start: "2026-08-02", ShiftCount: 2}},
				shifts:    sundayShifts("rota-1", "2026-08-02", 2),
			}
			store.rotaDefaults = &tc.defaults

			result, err := AllocateRota(
				context.Background(),
				store,
				&mockVolClient{},
				&config.Config{},
				zap.NewNop(),
				false, // dryRun
				false, // forceCommit
				"",    // pythonFlag
			)

			require.ErrorIs(t, err, ErrInvalidInput)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), "settings screen", "the refusal says where to go")
			for _, named := range tc.names {
				assert.Contains(t, err.Error(), named)
			}
			assert.Empty(t, store.insertedAllocations, "nothing is written")
		})
	}
}

// A Shift asking for nobody is the one gap that would not fail loudly: the
// solve succeeds and staffs nobody. Since Shifts own their Shapes the question
// is asked of the rota's Shifts rather than of the settings (issue #137), and
// the refusal names the dates so an admin can see which rota it means.
func TestAllocateRotaRefusesWhenAShiftAsksForNobody(t *testing.T) {
	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{{ID: "rota-1", Start: "2026-08-02", ShiftCount: 2}},
		shifts:    sundayShifts("rota-1", "2026-08-02", 2),
	}
	store.noShape = true

	result, err := AllocateRota(
		context.Background(),
		store,
		&mockVolClient{},
		&config.Config{},
		zap.NewNop(),
		false, // dryRun
		false, // forceCommit
		"",    // pythonFlag
	)

	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "2026-08-02")
	assert.Contains(t, err.Error(), "2026-08-09")
	assert.Contains(t, err.Error(), "ask for nobody")
	assert.Empty(t, store.insertedAllocations, "nothing is written")
}

// Nobody works a day the drop-in is shut, so a closed Shift with no Seats is
// not a gap in anything and must not stop the rota being allocated.
func TestAllocateRotaIgnoresAClosedShiftWithNoShape(t *testing.T) {
	shifts := sundayShifts("rota-1", "2026-08-02", 2)
	shifts[1].Closed = true
	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{{ID: "rota-1", Start: "2026-08-02", ShiftCount: 2}},
		shifts:    shifts,
	}
	// The open Shift has its Shape; the closed one was never given any Seats.
	store.shiftShapes = map[string][]db.DefaultShapeSeat{
		"2026-08-09": {},
	}

	_, err := AllocateRota(
		context.Background(),
		store,
		&mockVolClient{},
		&config.Config{},
		zap.NewNop(),
		false, // dryRun
		false, // forceCommit
		"",    // pythonFlag
	)

	require.Error(t, err, "there is no availability round, so it still refuses")
	assert.NotContains(t, err.Error(), "for nobody")
}

// The mirror of the above: settings an admin has filled in do not stop
// allocation before it starts. Whatever this run fails on afterwards, it is not
// the settings gate.
func TestAllocateRotaPastTheSettingsGate(t *testing.T) {
	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{{ID: "rota-1", Start: "2026-08-02", ShiftCount: 2}},
		shifts:    sundayShifts("rota-1", "2026-08-02", 2),
	}

	_, err := AllocateRota(
		context.Background(),
		store,
		&mockVolClient{},
		&config.Config{},
		zap.NewNop(),
		false, // dryRun
		false, // forceCommit
		"",    // pythonFlag
	)

	require.Error(t, err, "there is no availability round, so it still refuses")
	assert.NotContains(t, err.Error(), "settings screen")
}
