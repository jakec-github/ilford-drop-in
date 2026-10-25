package api

import (
	"context"
	"net/http"

	"go.uber.org/zap"
)

// VolunteerSyncFunc repopulates the volunteer roster from the volunteer sheet,
// using the server's own service account credential. Injected by the
// composition root, one per Organisation, so a Handler can trigger a sync
// without importing the Sheets client. A nil value disables the sync endpoint.
type VolunteerSyncFunc func(ctx context.Context) error

// handleSync repopulates the volunteer roster from the sheet. It is gated by
// requireLevel, so only a logged-in Organiser reaches it. Unlike login there is no
// OAuth round-trip: the server reads the sheet with its own service account, so
// the Organiser only needs to be authorised — no token is taken from them. Reads
// with the current (pre-sync) roster keep working while a sync is in flight,
// since the store swaps the slice wholesale only once the fetch succeeds.
func (h *Handler) handleSync(w http.ResponseWriter, r *http.Request) {
	if h.syncVolunteers == nil {
		h.logger.Error("Sync requested but no sync function configured")
		h.writeError(w, http.StatusServiceUnavailable, "sync unavailable")
		return
	}

	if err := h.syncVolunteers(r.Context()); err != nil {
		h.logger.Warn("Volunteer sync failed", zap.Error(err))
		h.writeError(w, http.StatusBadGateway, "sync failed")
		return
	}

	h.logger.Info("Volunteers synced", zap.String("by", sessionEmail(r.Context())))
	w.WriteHeader(http.StatusNoContent)
}
