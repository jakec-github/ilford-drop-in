package services

import (
	"errors"
	"fmt"
)

// Sentinel errors used to classify service failures. Callers (e.g. the HTTP
// API) can map them to appropriate responses with errors.Is.
var (
	// ErrInvalidInput indicates the caller supplied malformed or missing input
	ErrInvalidInput = errors.New("invalid input")
	// ErrNotFound indicates a referenced entity (volunteer, date) does not exist
	ErrNotFound = errors.New("not found")
	// ErrConflict indicates the request contradicts the current rota state
	ErrConflict = errors.New("conflict")
)

// wrapf builds an error that matches sentinel via errors.Is without altering
// the message text (the "%.0w" verb wraps invisibly).
func wrapf(sentinel error, format string, args ...any) error {
	args = append(args, sentinel)
	return fmt.Errorf(format+"%.0w", args...)
}
