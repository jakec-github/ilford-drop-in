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

// A draft is a guess at a rota that has not been decided yet. Once it has, the
// allocation is the rota and a draft beside it could only contradict it — so a
// solve for an allocated Rotation is refused before it starts, and nothing is
// written. The store enforces the same thing under a row lock, where a rota
// allocated *during* a solve is caught too.
func TestSolveDraftRotaAllocationRefusesAnAllocatedRota(t *testing.T) {
	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2026-08-02", ShiftCount: 2, AllocatedDatetime: "2026-08-01T10:00:00Z"},
		},
		shifts: sundayShifts("rota-1", "2026-08-02", 2),
	}

	result, err := SolveDraftRotaAllocation(
		context.Background(),
		store,
		&mockVolClient{},
		&config.Config{},
		zap.NewNop(),
		"", // pythonFlag
	)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "rota-1", "the refusal names the rota")
	assert.Contains(t, err.Error(), "already allocated")
	assert.Empty(t, store.storedDrafts, "no draft is written")
	assert.Empty(t, store.insertedAllocations, "and nothing is allocated either")
}

// Drafting and allocating solve one problem, so they refuse over one set of
// inputs: a Shift asking for nobody stops a draft exactly as it stops an
// allocation, naming the same dates. A draft that quietly staffed nothing would
// be worse than no draft, because it looks like an answer.
func TestSolveDraftRotaAllocationSharesTheAllocationGates(t *testing.T) {
	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{{ID: "rota-1", Start: "2026-08-02", ShiftCount: 2}},
		shifts:    sundayShifts("rota-1", "2026-08-02", 2),
	}
	store.noShape = true

	result, err := SolveDraftRotaAllocation(
		context.Background(),
		store,
		&mockVolClient{},
		&config.Config{},
		zap.NewNop(),
		"", // pythonFlag
	)

	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "2026-08-02")
	assert.Contains(t, err.Error(), "ask for nobody")
	assert.Empty(t, store.storedDrafts, "no draft is written")
}
