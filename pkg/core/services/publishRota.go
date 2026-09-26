package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/sheetsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/rotasheet"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services/utils"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// missingVolunteer stands in for somebody allocated who is no longer on the
// roster. They worked the shift, so the row still shows somebody there, and one
// missing name must not stop the rest of the rota being published.
const missingVolunteer = "[unknown volunteer]"

// PublishRotaStore defines the database operations needed for publishing a rota
type PublishRotaStore interface {
	RoleStore
	GetRotations(ctx context.Context) ([]db.Rotation, error)
	GetShiftsByRotaID(ctx context.Context, rotaID string) ([]db.Shift, error)
	GetAllocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Allocation, error)
	GetAlterationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Alteration, error)
	GetPublishedRotaSheet(ctx context.Context) (layout []byte, stale bool, err error)
	SavePublishedRotaSheet(ctx context.Context, rotaID string, layout []byte) error
	MarkPublishedRotaSheetStale(ctx context.Context) error
}

// SheetsClient defines the sheets operations needed for publishing a rota
type SheetsClient interface {
	ApplyRotaPlan(spreadsheetID string, plan rotasheet.Plan, archiveTitle string) (latestWasMissing bool, err error)
}

// PublishRota brings the rota sheet's Latest tab up to date with the rota
// allocated most recently (issue #191).
//
// It is the one way the sheet is written. The server runs it after every change
// that could show there, and the publishRota CLI command runs it by hand. It
// always publishes the latest allocated rota, whatever prompted it: a change to
// an older rota, already archived, publishes nothing new.
//
// Returns what it published, or nil when nothing has been allocated yet.
func PublishRota(
	ctx context.Context,
	database PublishRotaStore,
	sheetsClient SheetsClient,
	volunteerClient VolunteerClient,
	cfg *config.Config,
	logger *zap.Logger,
) (*rotasheet.Sheet, error) {
	rotations, err := database.GetRotations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch rotations: %w", err)
	}
	target := latestAllocated(rotations)
	if target == nil {
		logger.Info("No rota has been allocated, so there is nothing to publish")
		return nil, nil
	}

	rota, err := sheetRota(ctx, database, volunteerClient, cfg, target)
	if err != nil {
		return nil, err
	}
	sheet := rotasheet.Lay(rota)

	last, err := publishedLayout(ctx, database)
	if err != nil {
		return nil, err
	}
	plan := rotasheet.PlanEdits(last, sheet)

	archiveTitle := ""
	if plan.Archive {
		archiveTitle = archiveTabTitle(rotations, last, target, logger)
	}

	latestWasMissing, err := sheetsClient.ApplyRotaPlan(cfg.RotaSheetID, plan, archiveTitle)
	if err != nil {
		// Google can apply a batch and lose the answer, so the record may no
		// longer describe Latest. Marking it makes the next publish rebuild
		// rather than edit from a layout the sheet may not have.
		if markErr := database.MarkPublishedRotaSheetStale(ctx); markErr != nil {
			logger.Error("Failed to mark the published rota sheet stale", zap.Error(markErr))
		}
		return nil, fmt.Errorf("failed to publish to Google Sheets: %w", err)
	}
	if latestWasMissing {
		logger.Warn("The Latest tab was missing from the rota sheet, so it was rebuilt from scratch",
			zap.String("rota_id", target.ID))
	}

	layout, err := json.Marshal(sheet.Layout)
	if err != nil {
		return nil, fmt.Errorf("failed to encode the published layout: %w", err)
	}
	if err := database.SavePublishedRotaSheet(ctx, target.ID, layout); err != nil {
		return nil, err
	}

	logger.Info("Rota published to Google Sheets",
		zap.String("rota_id", target.ID),
		zap.Bool("archived", plan.Archive),
		zap.Bool("rebuilt", plan.Rebuild || latestWasMissing),
		zap.Int("structural_edits", len(plan.Ops)))

	return &sheet, nil
}

// latestAllocated is the rota Latest shows: the one allocated most recently,
// whatever its dates. Nil when nothing has been allocated.
func latestAllocated(rotations []db.Rotation) *db.Rotation {
	var latest *db.Rotation
	for i := range rotations {
		r := &rotations[i]
		if r.AllocatedDatetime == "" {
			continue
		}
		// RFC 3339 in UTC, so the strings order as the instants do.
		if latest == nil || r.AllocatedDatetime > latest.AllocatedDatetime {
			latest = r
		}
	}
	return latest
}

// publishedLayout is the record of what Latest holds, nil when there is none.
func publishedLayout(ctx context.Context, database PublishRotaStore) (*rotasheet.Layout, error) {
	raw, stale, err := database.GetPublishedRotaSheet(ctx)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, nil
	}
	var layout rotasheet.Layout
	if err := json.Unmarshal(raw, &layout); err != nil {
		return nil, fmt.Errorf("failed to decode the published layout: %w", err)
	}
	layout.Stale = stale
	return &layout, nil
}

// sheetRota reads the rota's shifts and who is on each, alterations applied, as
// the sheet shows them.
func sheetRota(
	ctx context.Context,
	database PublishRotaStore,
	volunteerClient VolunteerClient,
	cfg *config.Config,
	target *db.Rotation,
) (rotasheet.Rota, error) {
	// A rota always has at least one shift (ADR 0001); none is a broken
	// invariant and fails loudly.
	shifts, err := database.GetShiftsByRotaID(ctx, target.ID)
	if err != nil {
		return rotasheet.Rota{}, fmt.Errorf("failed to fetch shifts: %w", err)
	}
	if len(shifts) == 0 {
		return rotasheet.Rota{}, fmt.Errorf("rota %s has no shifts", target.ID)
	}
	shiftIDs := make([]string, len(shifts))
	for i, s := range shifts {
		shiftIDs[i] = s.ID
	}

	allocations, err := database.GetAllocationsByShiftIDs(ctx, shiftIDs)
	if err != nil {
		return rotasheet.Rota{}, fmt.Errorf("failed to fetch allocations: %w", err)
	}
	alterations, err := database.GetAlterationsByShiftIDs(ctx, shiftIDs)
	if err != nil {
		return rotasheet.Rota{}, fmt.Errorf("failed to fetch alterations: %w", err)
	}
	byShift := make(map[string][]db.Allocation)
	for _, a := range allocations {
		byShift[a.ShiftID] = append(byShift[a.ShiftID], a)
	}
	byShift = utils.ApplyAlterations(byShift, alterations)

	roles, err := RoleTable(ctx, database)
	if err != nil {
		return rotasheet.Rota{}, err
	}
	volunteers, err := volunteerClient.ListVolunteers(cfg, roles)
	if err != nil {
		return rotasheet.Rota{}, fmt.Errorf("failed to fetch volunteers: %w", err)
	}
	// The server's roster is empty until its first sync lands, and stays
	// empty if that sync failed. Publishing from it would name everybody on
	// the sheet as unknown.
	if len(volunteers) == 0 {
		return rotasheet.Rota{}, fmt.Errorf("the volunteer roster is empty, so the rota cannot be published until it has synced")
	}
	volunteersByID := make(map[string]model.Volunteer, len(volunteers))
	for _, v := range volunteers {
		volunteersByID[v.ID] = v
	}

	rota := rotasheet.Rota{ID: target.ID}
	for _, role := range roles.ByPriority() {
		rota.Roles = append(rota.Roles, rotasheet.Role{ID: role.ID, Name: role.Name})
	}

	for _, shift := range shifts {
		date, err := time.Parse("2006-01-02", shift.Date)
		if err != nil {
			return rotasheet.Rota{}, fmt.Errorf("invalid shift date %q: %w", shift.Date, err)
		}
		row := rotasheet.Shift{
			ID:     shift.ID,
			Date:   date.Format("Mon Jan 02 2006"),
			Closed: shift.Closed,
			Names:  map[string][]string{},
		}

		for _, a := range byShift[shift.ID] {
			// Pinned free text is bracketed, so a reader can tell it from a
			// volunteer the app knows.
			name := "[" + a.CustomEntry + "]"
			if a.VolunteerID != "" {
				name = missingVolunteer
				if v, ok := volunteersByID[a.VolunteerID]; ok {
					name = v.DisplayName
				}
			}
			// A Role the app does not know, or none, goes under Unknown role
			// rather than being dropped: somebody worked the shift.
			key := rotasheet.UnknownRoleKey
			if role, ok := roles.ByName(a.Role); ok {
				key = role.ID
			}
			row.Names[key] = append(row.Names[key], name)
		}
		for key := range row.Names {
			sort.Strings(row.Names[key])
		}

		rota.Shifts = append(rota.Shifts, row)
	}

	return rota, nil
}

// archiveTabTitle names the tab Latest is copied to: the dates of the rota it
// holds. With no record of which that is — the first publish since this was
// deployed — it is the rota before the one being published, which is what the
// command that used to publish left on Latest.
func archiveTabTitle(rotations []db.Rotation, last *rotasheet.Layout, target *db.Rotation, logger *zap.Logger) string {
	var archived *db.Rotation
	if last != nil {
		for i := range rotations {
			if rotations[i].ID == last.RotaID {
				archived = &rotations[i]
			}
		}
	} else {
		sorted := make([]db.Rotation, len(rotations))
		copy(sorted, rotations)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start < sorted[j].Start })
		for i := range sorted {
			if sorted[i].ID == target.ID && i > 0 {
				archived = &sorted[i-1]
			}
		}
	}
	if archived == nil {
		return ""
	}

	title, err := sheetsclient.GenerateTabTitle(archived.Start, archived.End)
	if err != nil {
		logger.Warn("Failed to name the archive tab", zap.Error(err))
		return ""
	}
	return title
}
