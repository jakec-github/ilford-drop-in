package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const availabilityRequestColumns = `id, rota_id, shift_date, volunteer_id, form_id, form_url, form_sent`

// GetAvailabilityRequests retrieves all availability request records
func (d *DB) GetAvailabilityRequests(ctx context.Context) ([]AvailabilityRequest, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT `+availabilityRequestColumns+`
		FROM availability_request
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query availability requests: %w", err)
	}
	return scanAvailabilityRequests(rows)
}

// GetAvailabilityRequestsByRotaID retrieves the availability request records for a single rota
func (d *DB) GetAvailabilityRequestsByRotaID(ctx context.Context, rotaID string) ([]AvailabilityRequest, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT `+availabilityRequestColumns+`
		FROM availability_request
		WHERE rota_id = $1
	`, rotaID)
	if err != nil {
		return nil, fmt.Errorf("failed to query availability requests for rota %s: %w", rotaID, err)
	}
	return scanAvailabilityRequests(rows)
}

func scanAvailabilityRequests(rows pgx.Rows) ([]AvailabilityRequest, error) {
	defer rows.Close()

	var requests []AvailabilityRequest
	for rows.Next() {
		var req AvailabilityRequest
		var shiftDate time.Time
		if err := rows.Scan(&req.ID, &req.RotaID, &shiftDate, &req.VolunteerID, &req.FormID, &req.FormURL, &req.FormSent); err != nil {
			return nil, fmt.Errorf("failed to scan availability request: %w", err)
		}
		req.ShiftDate = shiftDate.Format("2006-01-02")
		requests = append(requests, req)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating availability requests: %w", err)
	}

	return requests, nil
}

// InsertAvailabilityRequests inserts multiple availability request records in a batch
func (d *DB) InsertAvailabilityRequests(ctx context.Context, requests []AvailabilityRequest) error {
	if len(requests) == 0 {
		return nil
	}

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, req := range requests {
		_, err := tx.Exec(ctx, `
			INSERT INTO availability_request (id, rota_id, shift_date, volunteer_id, form_id, form_url, form_sent)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, req.ID, req.RotaID, req.ShiftDate, req.VolunteerID, req.FormID, req.FormURL, req.FormSent)
		if err != nil {
			return fmt.Errorf("failed to insert availability request: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// MarkAvailabilityRequestsSent sets form_sent=true on the given request IDs
func (d *DB) MarkAvailabilityRequestsSent(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	tag, err := d.pool.Exec(ctx, `
		UPDATE availability_request SET form_sent = TRUE WHERE id = ANY($1)
	`, ids)
	if err != nil {
		return fmt.Errorf("failed to mark availability requests sent: %w", err)
	}
	if int(tag.RowsAffected()) != len(ids) {
		return fmt.Errorf("expected to mark %d availability requests sent, updated %d", len(ids), tag.RowsAffected())
	}

	return nil
}
