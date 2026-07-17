package api

import (
	"context"
	"net/http"

	"go.uber.org/zap"
)

// VolunteerSyncFunc repopulates the volunteer roster from the volunteer sheet,
// using the server's own service account credential. Injected by the
// composition root so the Authenticator can trigger a sync without importing
// the Sheets client. A nil value disables the sync endpoint.
type VolunteerSyncFunc func(ctx context.Context) error

// handleSync repopulates the volunteer roster from the sheet. It is gated by
// requireAdmin, so only a logged-in admin reaches it. Unlike login there is no
// OAuth round-trip: the server reads the sheet with its own service account, so
// the admin only needs to be authorised — no token is taken from them. Reads
// with the current (pre-sync) roster keep working while a sync is in flight,
// since the store swaps the slice wholesale only once the fetch succeeds.
func (a *Authenticator) handleSync(w http.ResponseWriter, r *http.Request) {
	if a.syncVolunteers == nil {
		a.logger.Error("Sync requested but no sync function configured")
		http.Error(w, "sync unavailable", http.StatusServiceUnavailable)
		return
	}

	if err := a.syncVolunteers(r.Context()); err != nil {
		a.logger.Warn("Volunteer sync failed", zap.Error(err))
		http.Error(w, "sync failed", http.StatusBadGateway)
		return
	}

	a.logger.Info("Volunteers synced", zap.String("by", adminEmail(r.Context())))
	w.WriteHeader(http.StatusNoContent)
}
