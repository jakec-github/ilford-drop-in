package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// allocateRotaRequest states which rota is being allocated: not by id — there is
// only one in flight — but by the fingerprint of the draft the admin was looking
// at when they decided.
//
// Required, and deliberately not defaultable. A request that named no draft
// would mean "allocate whatever the solver says now", which is the one thing
// this endpoint exists to make impossible (ADR 0008).
type allocateRotaRequest struct {
	DraftHash string `json:"draftHash"`
}

// allocateRotaResponse is what came of the attempt: whether the rota was
// committed, and the rota the solve produced either way.
type allocateRotaResponse struct {
	// Allocated false means nothing was written at all — no allocations, no
	// stamp on the Rotation. It travels in the body as well as in the status
	// code because it is the fact the screen branches on.
	Allocated bool `json:"allocated"`
	// AllocatedAt is the stamp on the Rotation, absent when nothing was
	// allocated.
	AllocatedAt string `json:"allocatedAt,omitempty"`
	// Rota is the solve this attempt ran, in the same shape the draft is read
	// in — because that is what it was until the moment it was committed. When
	// the allocation was refused it is the fresh draft that replaced the one
	// confirmed, which is what the admin reads and confirms instead.
	Rota draftRotaAllocationResponse `json:"rota"`
}

// handleAllocateRotaInFlight allocates the rota in flight — the one the admin
// was shown, and no other.
//
// It re-solves, and commits only if the answer fingerprints identically to the
// draft named in the request (ADR 0008). A different answer means an allocator
// input moved while the admin was reading, so nothing is committed: the fresh
// solve becomes the draft and comes back as a 409, for the admin to read and
// confirm instead. That is a conflict rather than a failure — the request was
// understood and answered — and the body is the same shape either way.
//
// POST rather than PUT on some /allocation resource, and not idempotent in any
// sense: the answer depends on inputs moving underneath it, which is the whole
// point. Retrying the same request after a 409 with the same hash gets the same
// 409, because the hash names a rota that is no longer what solving produces.
//
// It takes the same solve slot draft solves take, waiting its turn for it. A
// solve is a subprocess, and two of them over one rota would race to replace
// each other's draft — and one of them could be replacing the draft this one is
// confirming.
//
// Waiting rather than refusing (issue #179) has one consequence worth stating:
// a solve landing while this request is queued may have moved the rota, so the
// hash the admin confirmed no longer matches and nothing is committed. That
// makes the change report an ordinary outcome of allocating during a burst of
// edits rather than a rare one — which is correct, and already what the report
// is for.
//
// Admin-only, like everything else about the rota being decided. This is the
// act that publishes it: after this the rota reaches GET /api/shifts and the
// calendar feed volunteers subscribe to.
func (h *Handler) handleAllocateRotaInFlight(w http.ResponseWriter, r *http.Request) {
	var req allocateRotaRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if err := h.drafts.acquire(r.Context()); err != nil {
		// The admin closed the tab or gave up while queueing. Nothing is
		// written, which is the whole answer: allocating is the one thing here
		// that must not happen without somebody watching it.
		h.logger.Debug("An allocation left the solve queue", zap.Error(err))
		return
	}
	defer h.drafts.release()

	// Under the same ceiling a draft solve runs under, and for the same reason:
	// a wedged subprocess holding this slot holds up every reader of the rota.
	// It bounds the commit as well as the solve, which is safe — the write is
	// one transaction, so a cancelled one rolls back rather than half-allocating
	// the rota.
	ctx, cancel := context.WithTimeout(r.Context(), solveCeiling)
	defer cancel()

	outcome, err := services.AllocateRotaInFlight(ctx, h.store, h.volunteers, h.cfg, h.logger, req.DraftHash, "")
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	response := allocateRotaResponse{
		Allocated: outcome.Allocated,
		Rota:      draftStatus(outcome.Solve),
	}
	if !outcome.Allocated {
		h.writeJSON(w, http.StatusConflict, response)
		return
	}
	response.AllocatedAt = outcome.AllocatedAt.Format(time.RFC3339)
	h.writeJSON(w, http.StatusOK, response)
}
