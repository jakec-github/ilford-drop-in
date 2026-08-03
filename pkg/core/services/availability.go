package services

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services/utils"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
	pkgutils "github.com/jakechorley/ilford-drop-in/pkg/utils"
)

// AvailabilityStore defines the database operations the availability round needs.
type AvailabilityStore interface {
	GetRotations(ctx context.Context) ([]db.Rotation, error)
	GetShiftsByRotaID(ctx context.Context, rotaID string) ([]db.Shift, error)
	MintAvailabilityRequests(ctx context.Context, requests []db.AvailabilityRequestV2) (int, error)
	GetAvailabilityRequestsV2ByRotaID(ctx context.Context, rotaID string) ([]db.AvailabilityRequestV2, error)
	GetAvailabilityRequestByToken(ctx context.Context, token string) (*db.AvailabilityRequestV2, error)
	GetLatestAvailability(ctx context.Context, requestIDs []string, cutoff *time.Time) (map[string]db.AvailabilityGeneration, error)
	InsertAvailabilityResponse(ctx context.Context, requestID string, answers []db.ShiftAnswer) (*db.AvailabilityGeneration, error)
	// Pins hold seats the answers coming in do not have to fill, so the round's
	// coverage cannot be read without them.
	GetManualPreallocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.ManualPreallocation, error)
}

// AvailabilityShift is one of a rota's shifts as both the volunteer's form and
// the admin roster present it. Closed shifts are carried rather than filtered
// out: a volunteer seeing the date listed and shut knows the drop-in is not
// running, where a missing date just looks like a mistake.
type AvailabilityShift struct {
	ID     string
	Date   string // YYYY-MM-DD
	Closed bool
}

// AvailabilityEntry is one volunteer's place in a round, as an admin sees it.
//
// Replied is the whole point of the roster: it says who still needs chasing.
// CoveredBy softens that for a volunteer whose group partner has already
// answered — the group has an answer, so this is not really a gap.
type AvailabilityEntry struct {
	VolunteerID   string
	VolunteerName string
	Token         string
	Replied       bool
	SubmittedAt   time.Time // zero when they have not replied
	// The shifts the latest generation said yes to. Empty for a volunteer who
	// replied "none of these", which Replied still reports as an answer.
	AvailableShiftIDs []string
	CoveredBy         []string
}

// AvailabilityRound is a rota's round: how each of its shifts is looking, and
// where everyone asked has got to. Allocated marks the round closed — links stop
// working then, so an admin looking at an allocated round is reading history.
//
// Groups, not volunteers, are the top level: a group is allocated as a unit and
// answers as a unit, so it is the grain an admin reads a round at. Each group
// carries its members, whose links are the per-volunteer grain a request is
// actually minted at.
type AvailabilityRound struct {
	RotaID    string
	RotaStart string
	RotaEnd   string
	Allocated bool
	Shifts    []ShiftCoverage
	Groups    []AvailabilityGroup
}

// AvailabilityForm is what a volunteer sees behind their link.
//
// SelectedShiftIDs is the form's landing state, not a record of anything: before
// a first submission it holds every open shift, because the model is opt-out
// (ADR 0004). Afterwards it holds what they last said, so re-opening the link
// shows their answer rather than silently offering to overwrite it with a full
// set.
type AvailabilityForm struct {
	VolunteerName    string
	GroupMembers     []string
	Shifts           []AvailabilityShift
	SelectedShiftIDs []string
	Submitted        bool
	SubmittedAt      time.Time // zero until they submit
}

// MintAvailabilityRound creates an availability request, with its own link, for
// every active volunteer on a rota. rotaID empty means the latest rota.
//
// It is idempotent by design rather than by luck: the store skips volunteers who
// already hold a request, so running it again after the roster changes tops the
// round up without re-tokening — and therefore without invalidating links
// already sent. Minting is deliberately separate from sending, so this needs no
// Google credential at all.
func MintAvailabilityRound(
	ctx context.Context,
	database AvailabilityStore,
	volunteerClient VolunteerClient,
	cfg *config.Config,
	logger *zap.Logger,
	rotaID string,
) (*AvailabilityRound, error) {
	rota, err := resolveRota(ctx, database, rotaID)
	if err != nil {
		return nil, err
	}

	// Links stop working at allocation, so minting one for an allocated rota
	// would hand out a link that is dead on arrival.
	if rota.AllocatedDatetime != "" {
		return nil, wrapf(ErrConflict, "rota %s is already allocated, so its availability links would not work", rota.ID)
	}

	volunteers, err := volunteerClient.ListVolunteers(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch volunteers: %w", err)
	}

	// Only active volunteers are asked. Someone who has stopped volunteering is
	// kept on the roster but is not part of a round.
	active := utils.FilterActiveVolunteers(volunteers)
	requests := make([]db.AvailabilityRequestV2, 0, len(active))
	for _, v := range active {
		token, err := pkgutils.RandomToken()
		if err != nil {
			return nil, fmt.Errorf("failed to generate availability token: %w", err)
		}
		requests = append(requests, db.AvailabilityRequestV2{
			ID:          uuid.New().String(),
			RotaID:      rota.ID,
			VolunteerID: v.ID,
			Token:       token,
		})
	}

	minted, err := database.MintAvailabilityRequests(ctx, requests)
	if err != nil {
		return nil, fmt.Errorf("failed to mint availability requests: %w", err)
	}

	logger.Info("Minted availability round",
		zap.String("rota_id", rota.ID),
		zap.Int("active_volunteers", len(active)),
		zap.Int("requests_created", minted))

	return buildRound(ctx, database, rota, volunteers, cfg, logger)
}

// GetAvailabilityRound reads a rota's round back: who was asked, their link, and
// who has answered. rotaID empty means the latest rota.
//
// The links are part of the product, not a debug affordance — copying one is how
// an admin resends out of band, and how the loop is drivable over HTTP before
// sending exists at all.
func GetAvailabilityRound(
	ctx context.Context,
	database AvailabilityStore,
	volunteerClient VolunteerClient,
	cfg *config.Config,
	logger *zap.Logger,
	rotaID string,
) (*AvailabilityRound, error) {
	rota, err := resolveRota(ctx, database, rotaID)
	if err != nil {
		return nil, err
	}

	volunteers, err := volunteerClient.ListVolunteers(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch volunteers: %w", err)
	}

	return buildRound(ctx, database, rota, volunteers, cfg, logger)
}

// GetAvailabilityForm resolves a volunteer's link to the form behind it.
//
// An unknown token is ErrNotFound and an allocated rota is ErrGone: the two are
// kept apart because they mean different things to the person holding the link —
// "this was never a link" versus "you are too late, the rota is out".
func GetAvailabilityForm(
	ctx context.Context,
	database AvailabilityStore,
	volunteerClient VolunteerClient,
	cfg *config.Config,
	logger *zap.Logger,
	token string,
) (*AvailabilityForm, error) {
	request, rota, shifts, volunteers, err := resolveToken(ctx, database, volunteerClient, cfg, logger, token)
	if err != nil {
		return nil, err
	}

	latest, err := database.GetLatestAvailability(ctx, []string{request.ID}, rotaCutoff(rota))
	if err != nil {
		return nil, fmt.Errorf("failed to read availability: %w", err)
	}

	return buildForm(request, shifts, volunteers, latest[request.ID]), nil
}

// SubmitAvailability records one complete generation for a link and returns the
// form as it now stands.
//
// shiftIDs is the whole answer every time, never a delta: an absent shift is a
// no, so a partial write would silently record unavailability (ADR 0004). There
// is no idempotency key — a duplicate submission writes another generation with
// a later timestamp, and latest-wins makes the outcome identical.
func SubmitAvailability(
	ctx context.Context,
	database AvailabilityStore,
	volunteerClient VolunteerClient,
	cfg *config.Config,
	logger *zap.Logger,
	token string,
	shiftIDs []string,
) (*AvailabilityForm, error) {
	request, rota, shifts, volunteers, err := resolveToken(ctx, database, volunteerClient, cfg, logger, token)
	if err != nil {
		return nil, err
	}

	answers, err := validateAnswers(shiftIDs, shifts)
	if err != nil {
		return nil, err
	}

	generation, err := database.InsertAvailabilityResponse(ctx, request.ID, answers)
	if err != nil {
		return nil, fmt.Errorf("failed to record availability response: %w", err)
	}

	logger.Info("Recorded availability response",
		zap.String("rota_id", rota.ID),
		zap.String("volunteer_id", request.VolunteerID),
		zap.Int("shifts_available", len(answers)))

	return buildForm(request, shifts, volunteers, *generation), nil
}

// validateAnswers turns the submitted shift ids into the generation's positive
// rows, rejecting anything that is not an open shift of this rota.
//
// Closed shifts are rejected rather than dropped: the form does not offer them,
// so a submission naming one is a client that has gone wrong, and quietly
// discarding it would record an answer the volunteer did not give. Duplicates
// are collapsed instead — a repeated tick means the same thing as one.
func validateAnswers(shiftIDs []string, shifts []AvailabilityShift) ([]db.ShiftAnswer, error) {
	open := make(map[string]bool, len(shifts))
	for _, s := range shifts {
		if !s.Closed {
			open[s.ID] = true
		}
	}

	seen := make(map[string]bool, len(shiftIDs))
	answers := make([]db.ShiftAnswer, 0, len(shiftIDs))
	for _, id := range shiftIDs {
		if !open[id] {
			return nil, wrapf(ErrInvalidInput, "shift %s is not an open shift on this rota", id)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		answers = append(answers, db.ShiftAnswer{ShiftID: id, Answer: db.AnswerYes})
	}
	return answers, nil
}

// resolveToken performs the lookups every link-holder request shares: the
// request behind the token, its rota, that rota's shifts and the roster. It is
// also where the two ways a link stops being usable are decided, so GET and POST
// cannot drift apart on them.
func resolveToken(
	ctx context.Context,
	database AvailabilityStore,
	volunteerClient VolunteerClient,
	cfg *config.Config,
	logger *zap.Logger,
	token string,
) (*db.AvailabilityRequestV2, *db.Rotation, []AvailabilityShift, []model.Volunteer, error) {
	request, err := database.GetAvailabilityRequestByToken(ctx, token)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to look up availability token: %w", err)
	}
	if request == nil {
		return nil, nil, nil, nil, wrapf(ErrNotFound, "no availability request for this link")
	}

	rota, err := resolveRota(ctx, database, request.RotaID)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	// Allocation, not any advertised deadline, is the real cutoff: once the rota
	// is out, a changed answer cannot affect it.
	if rota.AllocatedDatetime != "" {
		return nil, nil, nil, nil, wrapf(ErrGone, "the rota for this link has already been allocated")
	}

	shifts, err := rotaAvailabilityShifts(ctx, database, cfg, rota.ID, logger)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	volunteers, err := volunteerClient.ListVolunteers(cfg)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to fetch volunteers: %w", err)
	}

	return request, rota, shifts, volunteers, nil
}

// buildForm assembles the volunteer's view. generation is the zero value when
// they have not replied, which is what selects the opt-out landing state.
func buildForm(
	request *db.AvailabilityRequestV2,
	shifts []AvailabilityShift,
	volunteers []model.Volunteer,
	generation db.AvailabilityGeneration,
) *AvailabilityForm {
	form := &AvailabilityForm{
		Shifts:    shifts,
		Submitted: generation.ResponseID != "",
	}

	volunteer, known := findVolunteer(volunteers, request.VolunteerID)
	if known {
		form.VolunteerName = volunteerName(volunteer)
		form.GroupMembers = partnerNames(groupPartners(volunteers, volunteer, utils.IsActive))
	} else {
		// A volunteer dropped from the sheet mid-round still holds a working
		// link; degrade to the id rather than 404 a link that was legitimately
		// sent. They will notice the name is wrong, which is the point of it.
		form.VolunteerName = request.VolunteerID
	}

	if form.Submitted {
		form.SubmittedAt = generation.SubmittedAt
		// Intersect with the open shifts rather than echoing the stored rows: a
		// shift closed since they answered must not come back pre-ticked.
		open := make(map[string]bool, len(shifts))
		for _, s := range shifts {
			if !s.Closed {
				open[s.ID] = true
			}
		}
		form.SelectedShiftIDs = make([]string, 0, len(generation.Answers))
		for _, a := range generation.Answers {
			if open[a.ShiftID] {
				form.SelectedShiftIDs = append(form.SelectedShiftIDs, a.ShiftID)
			}
		}
		return form
	}

	// Opt-out: land with every open shift ticked, matching the Forms behaviour
	// this replaces. A mis-tap then records full availability, which is benign;
	// starting blank would let one record zero availability, which is
	// indistinguishable from a genuine "I can't do any of these".
	form.SelectedShiftIDs = make([]string, 0, len(shifts))
	for _, s := range shifts {
		if !s.Closed {
			form.SelectedShiftIDs = append(form.SelectedShiftIDs, s.ID)
		}
	}
	return form
}

// buildRound assembles the admin's view of a round from its requests and the
// latest generation behind each.
func buildRound(
	ctx context.Context,
	database AvailabilityStore,
	rota *db.Rotation,
	volunteers []model.Volunteer,
	cfg *config.Config,
	logger *zap.Logger,
) (*AvailabilityRound, error) {
	shifts, err := rotaAvailabilityShifts(ctx, database, cfg, rota.ID, logger)
	if err != nil {
		return nil, err
	}

	requests, err := database.GetAvailabilityRequestsV2ByRotaID(ctx, rota.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch availability requests: %w", err)
	}

	requestIDs := make([]string, 0, len(requests))
	for _, r := range requests {
		requestIDs = append(requestIDs, r.ID)
	}
	latest, err := database.GetLatestAvailability(ctx, requestIDs, rotaCutoff(rota))
	if err != nil {
		return nil, fmt.Errorf("failed to read availability: %w", err)
	}

	// Who has replied is settled before any entry is built, because covered-by
	// asks about other people's rows.
	replied := make(map[string]bool, len(requests))
	for _, r := range requests {
		if _, ok := latest[r.ID]; ok {
			replied[r.VolunteerID] = true
		}
	}

	entries := make([]AvailabilityEntry, 0, len(requests))
	for _, r := range requests {
		generation, hasReplied := latest[r.ID]
		entry := AvailabilityEntry{
			VolunteerID:       r.VolunteerID,
			VolunteerName:     r.VolunteerID,
			Token:             r.Token,
			Replied:           hasReplied,
			AvailableShiftIDs: make([]string, 0, len(generation.Answers)),
		}
		for _, a := range generation.Answers {
			entry.AvailableShiftIDs = append(entry.AvailableShiftIDs, a.ShiftID)
		}
		if hasReplied {
			entry.SubmittedAt = generation.SubmittedAt
		}

		if volunteer, known := findVolunteer(volunteers, r.VolunteerID); known {
			entry.VolunteerName = volunteerName(volunteer)
			if !hasReplied {
				entry.CoveredBy = partnerNames(groupPartners(volunteers, volunteer, func(other model.Volunteer) bool {
					return replied[other.ID]
				}))
			}
		} else {
			logger.Warn("Availability request for a volunteer no longer on the roster",
				zap.String("volunteer_id", r.VolunteerID))
		}

		entries = append(entries, entry)
	}

	volunteersByID := make(map[string]model.Volunteer, len(volunteers))
	for _, v := range volunteers {
		volunteersByID[v.ID] = v
	}

	shiftIDs := make([]string, 0, len(shifts))
	for _, s := range shifts {
		shiftIDs = append(shiftIDs, s.ID)
	}
	pins, err := database.GetManualPreallocationsByShiftIDs(ctx, shiftIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manual preallocations: %w", err)
	}

	groups := buildAvailabilityGroups(entries, volunteersByID, shifts)

	return &AvailabilityRound{
		RotaID:    rota.ID,
		RotaStart: rota.Start,
		RotaEnd:   rota.End,
		Allocated: rota.AllocatedDatetime != "",
		Shifts:    buildCoverage(shifts, groups, buildShiftSeats(cfg, shifts, pins, logger), volunteersByID),
		Groups:    groups,
	}, nil
}

// rotaAvailabilityShifts reads a rota's shifts with their closed state, in date
// order. Closed is resolved by isShiftClosed, the same check the publish and
// list paths use, so a date shut on the rota page is shut on the form too.
func rotaAvailabilityShifts(
	ctx context.Context,
	database AvailabilityStore,
	cfg *config.Config,
	rotaID string,
	logger *zap.Logger,
) ([]AvailabilityShift, error) {
	shifts, err := database.GetShiftsByRotaID(ctx, rotaID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch shifts: %w", err)
	}
	if len(shifts) == 0 {
		return nil, fmt.Errorf("rota %s has no shifts", rotaID)
	}

	dates, err := utils.ShiftDatesFromShifts(shifts)
	if err != nil {
		return nil, err
	}

	out := make([]AvailabilityShift, 0, len(shifts))
	for _, s := range shifts {
		out = append(out, AvailabilityShift{
			ID:     s.ID,
			Date:   s.Date,
			Closed: isShiftClosed(s.Date, cfg.RotaOverrides, dates, logger),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, nil
}

// resolveRota finds a rota by id, or the latest one when the id is empty.
func resolveRota(ctx context.Context, database AvailabilityStore, rotaID string) (*db.Rotation, error) {
	rotations, err := database.GetRotations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch rotations: %w", err)
	}
	if len(rotations) == 0 {
		return nil, wrapf(ErrNotFound, "no rotas have been defined yet")
	}

	if rotaID == "" {
		return utils.FindLatestRotation(rotations), nil
	}
	for i := range rotations {
		if rotations[i].ID == rotaID {
			return &rotations[i], nil
		}
	}
	return nil, wrapf(ErrNotFound, "rota %s not found", rotaID)
}

// rotaCutoff bounds availability reads at the moment the rota was allocated, so
// what is read back is the answer that was in when it mattered. Nil while the
// rota is still open, which is every read the volunteer's form makes.
func rotaCutoff(rota *db.Rotation) *time.Time {
	if rota.AllocatedDatetime == "" {
		return nil
	}
	allocated, err := time.Parse(time.RFC3339, rota.AllocatedDatetime)
	if err != nil {
		return nil
	}
	return &allocated
}

func findVolunteer(volunteers []model.Volunteer, id string) (model.Volunteer, bool) {
	for _, v := range volunteers {
		if v.ID == id {
			return v, true
		}
	}
	return model.Volunteer{}, false
}
