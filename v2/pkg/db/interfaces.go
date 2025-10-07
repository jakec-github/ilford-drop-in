package db

import "context"

// RotationStore defines the interface for rotation database operations
type RotationStore interface {
	GetRotations(ctx context.Context) ([]Rotation, error)
	InsertRotation(rotation *Rotation) error
}
