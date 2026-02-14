package db

import (
	"context"
	"time"
)

// RotationStore defines the interface for rotation database operations
type RotationStore interface {
	GetRotations(ctx context.Context) ([]Rotation, error)
	InsertRotation(rotation *Rotation) error
}

// Database defines the interface for all database operations.
// Both the SheetsSQL-backed db.DB and postgres.DB implement this interface.
type Database interface {
	GetRotations(ctx context.Context) ([]Rotation, error)
	InsertRotation(rotation *Rotation) error
	SetRotationAllocatedDatetime(ctx context.Context, rotaID string, datetime time.Time) error
	GetAvailabilityRequests(ctx context.Context) ([]AvailabilityRequest, error)
	InsertAvailabilityRequests(requests []AvailabilityRequest) error
	GetAllocations(ctx context.Context) ([]Allocation, error)
	InsertAllocations(allocations []Allocation) error
	InsertCover(ctx context.Context, cover *Cover) error
	InsertAlterations(ctx context.Context, alterations []Alteration) error
	GetAlterations(ctx context.Context) ([]Alteration, error)
}
