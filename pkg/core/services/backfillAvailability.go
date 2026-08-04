package services

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/clients/formsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services/utils"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
	pkgutils "github.com/jakechorley/ilford-drop-in/pkg/utils"
)

// AvailabilityBackfillStore is what the one-off backfill needs of the database:
// the legacy Forms-era requests on one side, the stored round on the other.
type AvailabilityBackfillStore interface {
	GetRotations(ctx context.Context) ([]db.Rotation, error)
	GetAvailabilityRequests(ctx context.Context) ([]db.AvailabilityRequest, error)
	GetShiftsByRotaID(ctx context.Context, rotaID string) ([]db.Shift, error)
	MintAvailabilityRequests(ctx context.Context, requests []db.AvailabilityRequestV2) (int, error)
	GetAvailabilityRequestsV2ByRotaID(ctx context.Context, rotaID string) ([]db.AvailabilityRequestV2, error)
	InsertBackfilledAvailabilityResponse(ctx context.Context, requestID string, submittedAt time.Time, answers []db.ShiftAnswer) (bool, error)
}

// FormResponseLister reads every answer a form holds, with its timestamp.
type FormResponseLister interface {
	ListFormResponses(formID string, volunteerName string, shiftDates []time.Time) ([]formsclient.SubmittedFormResponse, error)
}

// BackfillResult is what one run of the backfill did, or — under a dry run —
// what it would have done.
type BackfillResult struct {
	Rotas              int
	FormsRead          int
	RequestsMinted     int
	GenerationsWritten int
	// Generations the store already held, which is what a re-run consists
	// entirely of.
	GenerationsSkipped int
	// Responses that could not be imported, one line each. Never fatal, always
	// printed: a silently dropped answer is a hole in a report nobody will think
	// to check.
	Warnings []string
}

// BackfillAvailabilityFromForms copies the availability history out of Google
// Forms and into the stored round, so the longitudinal report survives the Forms
// client being deleted (issue #80). It reads Forms and writes nothing back
// there.
//
// It runs once, against production, immediately before the thing it reads from
// is removed — so it is built to be re-runnable rather than to be got right
// first time. Every write is keyed on data Forms already fixed: a volunteer
// keeps the request they were minted, and a response keeps the moment it was
// submitted, so a second run offers exactly the same rows and writes none of
// them.
//
// Fidelity, and its one accepted caveat: a legacy request that was never emailed
// is skipped entirely, because the report read that as "not asked" and minting a
// request would promote it to "asked and never replied". But `parseFormResponse`
// *infers* "available for all" from a single-answer response, and this bakes
// that inference into permanent rows rather than re-deriving it per read.
func BackfillAvailabilityFromForms(
	ctx context.Context,
	database AvailabilityBackfillStore,
	forms FormResponseLister,
	logger *zap.Logger,
	dryRun bool,
) (*BackfillResult, error) {
	legacyByRota, err := sentLegacyRequestsByRota(ctx, database)
	if err != nil {
		return nil, err
	}

	rotaOrder, err := backfillRotaOrder(ctx, database, legacyByRota)
	if err != nil {
		return nil, err
	}

	result := &BackfillResult{}
	for _, rotaID := range rotaOrder {
		if err := backfillRota(ctx, database, forms, logger, dryRun, rotaID, legacyByRota[rotaID], result); err != nil {
			return nil, fmt.Errorf("rota %s: %w", rotaID, err)
		}
		result.Rotas++
	}

	logger.Info("Availability backfill finished",
		zap.Bool("dry_run", dryRun),
		zap.Int("rotas", result.Rotas),
		zap.Int("forms_read", result.FormsRead),
		zap.Int("requests_minted", result.RequestsMinted),
		zap.Int("generations_written", result.GenerationsWritten),
		zap.Int("generations_skipped", result.GenerationsSkipped),
		zap.Int("warnings", len(result.Warnings)))

	return result, nil
}

// sentLegacyRequestsByRota collects the legacy requests worth backfilling,
// grouped by rota and ordered by volunteer so a run reads the same way twice.
//
// Only requests that were actually emailed count. An unsent one holds a form
// nobody ever saw, which every historical read treated as "not asked".
func sentLegacyRequestsByRota(ctx context.Context, database AvailabilityBackfillStore) (map[string][]db.AvailabilityRequest, error) {
	requests, err := database.GetAvailabilityRequests(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch legacy availability requests: %w", err)
	}

	byRota := make(map[string][]db.AvailabilityRequest)
	for _, req := range requests {
		if !req.FormSent || req.FormID == "" {
			continue
		}
		byRota[req.RotaID] = append(byRota[req.RotaID], req)
	}
	for rotaID := range byRota {
		rota := byRota[rotaID]
		sort.Slice(rota, func(i, j int) bool { return rota[i].VolunteerID < rota[j].VolunteerID })
	}
	return byRota, nil
}

// backfillRotaOrder puts the rotas holding legacy requests into start-date
// order, so the run's output reads chronologically and a partial run has made
// progress through history rather than through a hash map.
func backfillRotaOrder(ctx context.Context, database AvailabilityBackfillStore, legacyByRota map[string][]db.AvailabilityRequest) ([]string, error) {
	rotations, err := database.GetRotations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch rotations: %w", err)
	}

	startByRota := make(map[string]string, len(rotations))
	for _, rota := range rotations {
		startByRota[rota.ID] = rota.Start
	}

	order := make([]string, 0, len(legacyByRota))
	for rotaID := range legacyByRota {
		order = append(order, rotaID)
	}
	sort.Slice(order, func(i, j int) bool {
		if startByRota[order[i]] != startByRota[order[j]] {
			return startByRota[order[i]] < startByRota[order[j]]
		}
		return order[i] < order[j]
	})
	return order, nil
}

// backfillRota imports one rota's forms, accumulating into result.
func backfillRota(
	ctx context.Context,
	database AvailabilityBackfillStore,
	forms FormResponseLister,
	logger *zap.Logger,
	dryRun bool,
	rotaID string,
	legacy []db.AvailabilityRequest,
	result *BackfillResult,
) error {
	shifts, err := database.GetShiftsByRotaID(ctx, rotaID)
	if err != nil {
		return fmt.Errorf("failed to fetch shifts: %w", err)
	}
	if len(shifts) == 0 {
		return fmt.Errorf("no shifts are minted, so its form dates cannot be resolved")
	}

	// Every shift date, not just the open ones. The historical read passed the
	// whole rota to Forms, so "available for all" meant all of them — narrowing
	// it here would change the counts the report has always shown.
	shiftDates, err := utils.ShiftDatesFromShifts(shifts)
	if err != nil {
		return err
	}
	shiftIDByFormDate := make(map[string]string, len(shifts))
	for _, s := range shifts {
		date, err := time.Parse("2006-01-02", s.Date)
		if err != nil {
			return fmt.Errorf("invalid shift date %q: %w", s.Date, err)
		}
		shiftIDByFormDate[formDateKey(date)] = s.ID
	}

	requestIDByVolunteer, minted, err := backfillRequests(ctx, database, dryRun, rotaID, legacy)
	if err != nil {
		return err
	}
	result.RequestsMinted += minted

	for _, req := range legacy {
		responses, err := forms.ListFormResponses(req.FormID, req.VolunteerID, shiftDates)
		if err != nil {
			return fmt.Errorf("volunteer %s: %w", req.VolunteerID, err)
		}
		result.FormsRead++

		for _, response := range responses {
			if response.SubmittedAt.IsZero() {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("rota %s, volunteer %s: a response with no submission time was skipped", rotaID, req.VolunteerID))
				continue
			}

			answers, err := answersForDates(response.Response.AvailableDates, shiftIDByFormDate)
			if err != nil {
				return fmt.Errorf("volunteer %s: %w", req.VolunteerID, err)
			}

			if dryRun {
				result.GenerationsWritten++
				continue
			}

			written, err := database.InsertBackfilledAvailabilityResponse(
				ctx, requestIDByVolunteer[req.VolunteerID], response.SubmittedAt, answers)
			if err != nil {
				return fmt.Errorf("volunteer %s: %w", req.VolunteerID, err)
			}
			if written {
				result.GenerationsWritten++
			} else {
				result.GenerationsSkipped++
			}
		}
	}

	logger.Debug("Backfilled rota",
		zap.String("rota_id", rotaID),
		zap.Int("forms", len(legacy)),
		zap.Int("requests_minted", minted))

	return nil
}

// backfillRequests gives every emailed volunteer a request in the stored round
// and returns the request id to hang their generations off.
//
// The token is the schema's, not the volunteer's: these rotas are long
// allocated, so every link it mints is dead the moment it exists. Minting skips
// volunteers who already hold a request, which is what lets a re-run leave a
// live round alone.
func backfillRequests(
	ctx context.Context,
	database AvailabilityBackfillStore,
	dryRun bool,
	rotaID string,
	legacy []db.AvailabilityRequest,
) (requestIDByVolunteer map[string]string, minted int, err error) {
	requests := make([]db.AvailabilityRequestV2, 0, len(legacy))
	for _, req := range legacy {
		token, err := pkgutils.RandomToken()
		if err != nil {
			return nil, 0, fmt.Errorf("failed to generate availability token: %w", err)
		}
		requests = append(requests, db.AvailabilityRequestV2{
			ID:          uuid.New().String(),
			RotaID:      rotaID,
			VolunteerID: req.VolunteerID,
			Token:       token,
		})
	}

	if dryRun {
		// Count what minting would create, without asking for it. The store is
		// the only thing that knows which volunteers already hold a request.
		held, err := database.GetAvailabilityRequestsV2ByRotaID(ctx, rotaID)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to read the stored round: %w", err)
		}
		heldBy := make(map[string]bool, len(held))
		for _, r := range held {
			heldBy[r.VolunteerID] = true
		}
		for _, req := range legacy {
			if !heldBy[req.VolunteerID] {
				minted++
			}
		}
		return nil, minted, nil
	}

	minted, err = database.MintAvailabilityRequests(ctx, requests)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to mint availability requests: %w", err)
	}

	stored, err := database.GetAvailabilityRequestsV2ByRotaID(ctx, rotaID)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read the stored round: %w", err)
	}
	requestIDByVolunteer = make(map[string]string, len(stored))
	for _, r := range stored {
		requestIDByVolunteer[r.VolunteerID] = r.ID
	}
	for _, req := range legacy {
		if requestIDByVolunteer[req.VolunteerID] == "" {
			return nil, 0, fmt.Errorf("volunteer %s has no availability request after minting", req.VolunteerID)
		}
	}

	return requestIDByVolunteer, minted, nil
}

// answersForDates turns a form response's available dates into a generation's
// positive rows.
//
// A date with no shift aborts the run. These rows are permanent and
// positive-only, so dropping one would record unavailability the volunteer never
// expressed — and it would look exactly like a legitimate answer. In practice it
// cannot happen: the dates come back from the same shift list they went in as.
func answersForDates(availableDates []string, shiftIDByFormDate map[string]string) ([]db.ShiftAnswer, error) {
	seen := make(map[string]bool, len(availableDates))
	answers := make([]db.ShiftAnswer, 0, len(availableDates))
	for _, date := range availableDates {
		shiftID, ok := shiftIDByFormDate[date]
		if !ok {
			return nil, fmt.Errorf("form answer %q matches no shift on this rota", date)
		}
		if seen[shiftID] {
			continue
		}
		seen[shiftID] = true
		answers = append(answers, db.ShiftAnswer{ShiftID: shiftID, Answer: db.AnswerYes})
	}
	return answers, nil
}

// formDateKey renders a date the way the Forms questions did, which is the only
// handle a stored answer gives us on which shift it meant.
func formDateKey(date time.Time) string {
	return date.Format("Mon Jan 2 2006")
}
