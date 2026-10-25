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

// healthHandler reports whether the server can actually serve requests, for
// deploy checks and the local stack runner, which polls it to know the app is
// up before driving it. It is deliberately the only route that answers this
// question: each Organisation's SPA fallback returns index.html with a 200 for
// every unmatched path under it, so polling any other URL proves nothing. It
// belongs to no Organisation: the database is shared, and so is the answer.
//
// Readiness is a live database probe rather than a flag set at boot, because the
// database is the one dependency whose loss makes every data-bearing route fail.
// The endpoint is public and says nothing beyond up or down.
func healthHandler(database Pinger, logger *zap.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), healthCheckTimeout)
		defer cancel()

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := database.Ping(ctx); err != nil {
			logger.Warn("Health check failed: database unreachable", zap.Error(err))
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"unavailable"}` + "\n"))
			return
		}

		_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
	})
}
