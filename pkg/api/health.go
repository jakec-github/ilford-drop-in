package api

import (
	"context"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// healthCheckTimeout bounds the database probe so a hung connection returns an
// answer instead of holding the poller open.
const healthCheckTimeout = 2 * time.Second

// handleHealth reports whether the server can actually serve requests, for
// deploy checks and the local stack runner, which polls it to know the app is
// up before driving it. It is deliberately the only route that answers this
// question: the frontend's SPA fallback returns index.html with a 200 for every
// unmatched path, so polling any other URL proves nothing.
//
// Readiness is a live database probe rather than a flag set at boot, because the
// database is the one dependency whose loss makes every data-bearing route fail.
// The endpoint is public and says nothing beyond up or down.
func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), healthCheckTimeout)
	defer cancel()

	if err := h.store.Ping(ctx); err != nil {
		h.logger.Warn("Health check failed: database unreachable", zap.Error(err))
		h.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
