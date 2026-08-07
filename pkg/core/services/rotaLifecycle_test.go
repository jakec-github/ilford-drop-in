package services

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// lifecycleStore is the store the one-rota-in-flight rule reads and its release
// valve writes to. It answers with what it was told rather than deriving
// anything: the derivation is the real store's, and it has its own tests
// against Postgres.
type lifecycleStore struct {
	inFlight    *db.RotaInFlight
	inFlightErr error
	// discardErr is what the database says to a discard: db.ErrRotaAllocated for
	// one it refuses, anything else for a failure.
	discardErr error
	// missing is a rota id the store holds no rotation for.
	missing   string
	discarded []string
}

func (s *lifecycleStore) GetRotaInFlight(context.Context) (*db.RotaInFlight, error) {
	return s.inFlight, s.inFlightErr
}

func (s *lifecycleStore) DiscardRota(_ context.Context, rotaID string) (bool, error) {
	if s.discardErr != nil {
		return false, s.discardErr
	}
	if rotaID == s.missing {
		return false, nil
	}
	s.discarded = append(s.discarded, rotaID)
	return true, nil
}

func TestRotaInFlight(t *testing.T) {
	store := &lifecycleStore{inFlight: &db.RotaInFlight{
		Rotation: db.Rotation{ID: "live", Start: "2026-08-02", End: "2026-08-23", ShiftCount: 4},
		Asked:    12,
		Sent:     12,
		Replied:  9,
	}}

	inFlight, err := RotaInFlight(context.Background(), store)
	require.NoError(t, err)
	require.NotNil(t, inFlight)
	assert.Equal(t, "live", inFlight.ID)
	assert.Equal(t, 9, inFlight.Replied)
}

// Nothing in flight is an answer rather than a miss: it is the state in which a
// rota may be defined, so a caller reads it as permission as well as absence.
func TestRotaInFlight_Nothing(t *testing.T) {
	inFlight, err := RotaInFlight(context.Background(), &lifecycleStore{})
	require.NoError(t, err)
	assert.Nil(t, inFlight)
}

func TestDiscardRota(t *testing.T) {
	store := &lifecycleStore{}

	require.NoError(t, DiscardRota(context.Background(), store, zap.NewNop(), "live"))
	assert.Equal(t, []string{"live"}, store.discarded)
}

// An allocated rota is never discarded. The refusal comes back from the store,
// which decided it under the rota's row lock, and is classified as a conflict:
// nothing about the request is malformed.
func TestDiscardRota_RefusesAnAllocatedRota(t *testing.T) {
	store := &lifecycleStore{discardErr: db.ErrRotaAllocated}

	err := DiscardRota(context.Background(), store, zap.NewNop(), "done")
	require.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "never discarded")
	assert.Empty(t, store.discarded)
}

func TestDiscardRota_UnknownRota(t *testing.T) {
	store := &lifecycleStore{missing: "ghost"}

	err := DiscardRota(context.Background(), store, zap.NewNop(), "ghost")
	require.ErrorIs(t, err, ErrNotFound)
	assert.Empty(t, store.discarded)
}

func TestDiscardRota_NoRotaID(t *testing.T) {
	store := &lifecycleStore{}

	err := DiscardRota(context.Background(), store, zap.NewNop(), "")
	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Empty(t, store.discarded, "an unaddressed discard never reaches the store")
}

// A store that fails is a fault, not a refusal: it must not read as a rota that
// was not there.
func TestDiscardRota_StoreFailure(t *testing.T) {
	store := &lifecycleStore{discardErr: errors.New("connection refused")}

	err := DiscardRota(context.Background(), store, zap.NewNop(), "live")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNotFound)
	assert.NotErrorIs(t, err, ErrConflict)
}
