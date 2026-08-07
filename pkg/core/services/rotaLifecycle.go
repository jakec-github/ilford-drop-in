package services

import (
	"context"
	"errors"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// RotaLifecycleStore defines the database operations the one-rota-in-flight rule
// and its release valve need: reading the rota being worked on, and destroying
// it.
type RotaLifecycleStore interface {
	GetRotaInFlight(ctx context.Context) (*db.RotaInFlight, error)
	DiscardRota(ctx context.Context, rotaID string) (bool, error)
}

// RotaInFlight reports the rota being worked on — the one unallocated Rotation —
// or nil when there is none.
//
// Nil is the ordinary state between one rota going out and the next being
// defined, and it is exactly the state in which a rota may be defined, so a
// caller can read this as "may I define one" as well as "what am I working on".
func RotaInFlight(ctx context.Context, database RotaLifecycleStore) (*db.RotaInFlight, error) {
	return database.GetRotaInFlight(ctx)
}

// DiscardRota destroys an unallocated Rotation and everything hanging off it:
// its Shifts, their Shapes, its Preallocations, its availability round and every
// response given to it.
//
// It is the release valve the one-rota-in-flight rule requires. Allocation is
// otherwise the only thing that ends a rota's life, so without this one mistyped
// shift count wedges the system — and the case an admin is most likely to hit is
// realising the rota is wrong *because* a volunteer replied to say so, which is
// why answers already given are destroyed rather than protected.
//
// An allocated rota is never discarded. It is the rota people are turning up to,
// and its Allocations are the record of what was decided; unmaking that is not a
// correction but an erasure, and the tool for changing an allocated rota is an
// Alteration.
func DiscardRota(ctx context.Context, database RotaLifecycleStore, logger *zap.Logger, rotaID string) error {
	if rotaID == "" {
		return wrapf(ErrInvalidInput, "a rota id is required to discard a rota")
	}

	discarded, err := database.DiscardRota(ctx, rotaID)
	if errors.Is(err, db.ErrRotaAllocated) {
		return wrapf(ErrConflict, "rota %s has already been allocated, and an allocated rota is never discarded", rotaID)
	}
	if err != nil {
		return err
	}
	if !discarded {
		return wrapf(ErrNotFound, "no rota with id %s", rotaID)
	}

	logger.Info("Discarded rota", zap.String("rota_id", rotaID))
	return nil
}
