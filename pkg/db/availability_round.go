package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const availabilityRequestV2Columns = `id, rota_id, volunteer_id, token, sent_at`

// MintAvailabilityRequests inserts one request per volunteer, skipping any
// volunteer who already holds a request for that rota, and reports how many rows
// it actually created.
//
// Skipping rather than failing is what makes minting a round idempotent: the
// admin's job is "everyone active has a link", and running it again after the
// roster gains a volunteer must add that one link without re-tokening anyone.
// Re-tokening would silently break links already distributed, so an existing
// row always wins over the one offered here.
func (d *DB) MintAvailabilityRequests(ctx context.Context, requests []AvailabilityRequestV2) (int, error) {
	if len(requests) == 0 {
		return 0, nil
	}

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	inserted := 0
	for _, req := range requests {
		tag, err := tx.Exec(ctx, `
			INSERT INTO availability_request_v2 (id, rota_id, volunteer_id, token)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT ON CONSTRAINT availability_request_v2_rota_volunteer_key DO NOTHING
		`, req.ID, req.RotaID, req.VolunteerID, req.Token)
		if err != nil {
			return 0, fmt.Errorf("failed to mint availability request for volunteer %s: %w", req.VolunteerID, err)
		}
		inserted += int(tag.RowsAffected())
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return inserted, nil
}

// GetAvailabilityRequestsV2ByRotaID retrieves a rota's tokenised availability
// requests, ordered by volunteer id so the round reads the same way twice.
func (d *DB) GetAvailabilityRequestsV2ByRotaID(ctx context.Context, rotaID string) ([]AvailabilityRequestV2, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT `+availabilityRequestV2Columns+`
		FROM availability_request_v2
		WHERE rota_id = $1
		ORDER BY volunteer_id
	`, rotaID)
	if err != nil {
		return nil, fmt.Errorf("failed to query availability requests for rota %s: %w", rotaID, err)
	}
	defer rows.Close()

	var requests []AvailabilityRequestV2
	for rows.Next() {
		req, err := scanAvailabilityRequestV2(rows)
		if err != nil {
			return nil, err
		}
		requests = append(requests, req)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating availability requests: %w", err)
	}

	return requests, nil
}

// GetAvailabilityRequestByToken resolves a link's token to its request, or nil
// when no request carries it. An unknown token is a miss, not an error: it is
// the ordinary case of a mistyped or retired link, and the caller answers 404.
func (d *DB) GetAvailabilityRequestByToken(ctx context.Context, token string) (*AvailabilityRequestV2, error) {
	row := d.pool.QueryRow(ctx, `
		SELECT `+availabilityRequestV2Columns+`
		FROM availability_request_v2
		WHERE token = $1
	`, token)

	req, err := scanAvailabilityRequestV2(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// rowScanner is the shared shape of pgx.Row and pgx.Rows, so one scan serves
// both the single-token lookup and the per-rota listing.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanAvailabilityRequestV2(row rowScanner) (AvailabilityRequestV2, error) {
	var req AvailabilityRequestV2
	var sentAt *time.Time
	if err := row.Scan(&req.ID, &req.RotaID, &req.VolunteerID, &req.Token, &sentAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return req, err
		}
		return req, fmt.Errorf("failed to scan availability request: %w", err)
	}
	if sentAt != nil {
		req.SentAt = sentAt.UTC().Format(time.RFC3339)
	}
	return req, nil
}

// GetLatestAvailability returns the newest generation for each of the given
// requests, keyed by request id. A request with no generation before the cutoff
// is absent from the map — that is how "has not replied" is represented, and it
// is distinct from a present generation with no answers ("available for
// nothing").
//
// cutoff bounds the read to submissions at or before a moment, and is the rota's
// allocated_datetime in every real caller: once a rota is allocated, what
// matters is the answer that was in at the time, not what someone changed
// afterwards. Pass nil for no bound.
//
// This is the one place the latest-generation query lives (ADR 0004). Every
// consumer — the volunteer's own form, the admin roster, and the allocator when
// it switches over — calls it rather than reimplementing the ordering, which is
// how the group logic it replaces ended up written three times over.
func (d *DB) GetLatestAvailability(ctx context.Context, requestIDs []string, cutoff *time.Time) (map[string]AvailabilityGeneration, error) {
	generations := make(map[string]AvailabilityGeneration)
	if len(requestIDs) == 0 {
		return generations, nil
	}

	// DISTINCT ON picks the winning generation per request before the join, so
	// the limit binds to the generation and not to its shift rows. The LEFT JOIN
	// keeps a generation that ticked nothing. id DESC breaks submitted_at ties
	// deterministically — two submissions can share a timestamp.
	rows, err := d.pool.Query(ctx, `
		WITH latest AS (
			SELECT DISTINCT ON (availability_request_id)
				id, availability_request_id, submitted_at
			FROM availability_response
			WHERE availability_request_id = ANY($1)
			  AND submitted_at <= COALESCE($2, 'infinity'::timestamptz)
			ORDER BY availability_request_id, submitted_at DESC, id DESC
		)
		SELECT l.availability_request_id, l.id, l.submitted_at, sa.shift_id, sa.answer
		FROM latest l
		LEFT JOIN shift_availability sa ON sa.response_id = l.id
		ORDER BY l.availability_request_id, sa.shift_id
	`, requestIDs, cutoff)
	if err != nil {
		return nil, fmt.Errorf("failed to query latest availability: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var requestID, responseID string
		var submittedAt time.Time
		// Null when the generation ticked nothing, which the LEFT JOIN preserves.
		var shiftID, answer *string
		if err := rows.Scan(&requestID, &responseID, &submittedAt, &shiftID, &answer); err != nil {
			return nil, fmt.Errorf("failed to scan availability generation: %w", err)
		}

		generation, ok := generations[requestID]
		if !ok {
			generation = AvailabilityGeneration{
				RequestID:   requestID,
				ResponseID:  responseID,
				SubmittedAt: submittedAt,
			}
		}
		if shiftID != nil && answer != nil {
			generation.Answers = append(generation.Answers, ShiftAnswer{ShiftID: *shiftID, Answer: *answer})
		}
		generations[requestID] = generation
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating availability generations: %w", err)
	}

	return generations, nil
}

// InsertAvailabilityResponse writes one complete generation for a request and
// returns it as stored, including the timestamp the database stamped.
//
// The response row and all of its shift rows go in a single transaction because
// a generation is only meaningful whole: positives-only encoding cannot tell "no
// row because they said no" from "no row because the insert failed", so a
// half-written generation would silently record unavailability (ADR 0004).
// Callers pass the full set of positives every time, never a delta.
func (d *DB) InsertAvailabilityResponse(ctx context.Context, requestID string, answers []ShiftAnswer) (*AvailabilityGeneration, error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	responseID := uuid.New().String()
	var submittedAt time.Time
	err = tx.QueryRow(ctx, `
		INSERT INTO availability_response (id, availability_request_id)
		VALUES ($1, $2)
		RETURNING submitted_at
	`, responseID, requestID).Scan(&submittedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to insert availability response: %w", err)
	}

	batch := &pgx.Batch{}
	for _, a := range answers {
		batch.Queue(`
			INSERT INTO shift_availability (id, response_id, shift_id, answer)
			VALUES ($1, $2, $3, $4)
		`, uuid.New().String(), responseID, a.ShiftID, a.Answer)
	}
	results := tx.SendBatch(ctx, batch)
	for range answers {
		if _, err := results.Exec(); err != nil {
			results.Close()
			return nil, fmt.Errorf("failed to insert shift availability: %w", err)
		}
	}
	if err := results.Close(); err != nil {
		return nil, fmt.Errorf("failed to close shift availability batch: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &AvailabilityGeneration{
		RequestID:   requestID,
		ResponseID:  responseID,
		SubmittedAt: submittedAt,
		Answers:     answers,
	}, nil
}
