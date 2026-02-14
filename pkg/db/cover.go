package db

import (
	"context"
	"fmt"
)

// InsertCover is not supported in SheetsSQL
func (db *DB) InsertCover(ctx context.Context, cover *Cover) error {
	return fmt.Errorf("covers not supported in SheetsSQL")
}

// InsertAlterations is not supported in SheetsSQL
func (db *DB) InsertAlterations(ctx context.Context, alterations []Alteration) error {
	return fmt.Errorf("alterations not supported in SheetsSQL")
}

// GetAlterations is not supported in SheetsSQL
func (db *DB) GetAlterations(ctx context.Context) ([]Alteration, error) {
	return nil, fmt.Errorf("alterations not supported in SheetsSQL")
}
