package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// Store defines the database operations the API needs (satisfied by *db.DB)
type Store interface {
	services.AvailabilityStore
	services.ChangeRotaStore
	services.DefineRotaStore
	services.ListShiftsStore
	services.PreallocationStore
	// Ping reports whether the database is reachable, for GET /health.
	Ping(ctx context.Context) error
}

// Handler serves the HTTP API
type Handler struct {
	store      Store
	volunteers services.VolunteerClient
	cfg        *config.Config
	auth       *Authenticator
	frontend   fs.FS
	logger     *zap.Logger
}

// NewHandler creates an API handler with its dependencies. frontend is the
// embedded frontend build; pass nil (or a build-less placeholder) to serve the
// API only.
func NewHandler(store Store, volunteers services.VolunteerClient, cfg *config.Config, auth *Authenticator, frontend fs.FS, logger *zap.Logger) *Handler {
	return &Handler{
		store:      store,
		volunteers: volunteers,
		cfg:        cfg,
		auth:       auth,
		frontend:   frontend,
		logger:     logger,
	}
}

// Routes returns the API's route table
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.handleHealth)
	mux.HandleFunc("GET /shifts", h.handleListShifts)
	mux.Handle("POST /rotations", h.auth.requireAdmin(http.HandlerFunc(h.handleDefineRota)))
	mux.Handle("POST /alterations", h.auth.requireAdmin(http.HandlerFunc(h.handleCreateAlteration)))
	mux.HandleFunc("GET /preallocations", h.handleListPreallocations)
	mux.Handle("POST /preallocations", h.auth.requireAdmin(http.HandlerFunc(h.handleCreatePreallocation)))
	mux.Handle("DELETE /preallocations/{id}", h.auth.requireAdmin(http.HandlerFunc(h.handleDeletePreallocation)))
	mux.HandleFunc("GET /calendars/{filename}", h.handleCalendar)
	mux.Handle("GET /volunteers", h.auth.requireAdmin(http.HandlerFunc(h.handleListVolunteers)))
	// Rounds are admin-only: the roster hands out every volunteer's link, which
	// is a bearer credential for their availability.
	mux.Handle("POST /availability-rounds", h.auth.requireAdmin(http.HandlerFunc(h.handleMintAvailabilityRound)))
	mux.Handle("GET /availability-rounds", h.auth.requireAdmin(http.HandlerFunc(h.handleGetAvailabilityRound)))
	// The volunteer's own link, public by design — the link is the identity and
	// volunteers never log in. Registered under a separate prefix from the
	// admin rounds above so neither path can shadow the other.
	mux.HandleFunc("GET /availability/{token}", h.handleAvailabilityForm)
	mux.HandleFunc("POST /availability/{token}", h.handleSubmitAvailability)
	h.auth.registerRoutes(mux)
	if hasFrontend(h.frontend) {
		mux.Handle("GET /", frontendHandler(h.frontend))
	} else {
		h.logger.Info("No frontend build embedded; serving API only")
	}
	return mux
}

// writeJSON writes v as a JSON response with the given status
func (h *Handler) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		h.logger.Error("Failed to encode response", zap.Error(err))
	}
}

// writeError writes a JSON error body with the given status
func (h *Handler) writeError(w http.ResponseWriter, status int, msg string) {
	h.writeJSON(w, status, map[string]string{"error": msg})
}

// writeServiceError maps a service error to an HTTP response. Unclassified
// errors are treated as internal: logged in full, reported generically.
func (h *Handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrInvalidInput):
		h.writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, services.ErrNotFound):
		h.writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, services.ErrConflict):
		h.writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, services.ErrGone):
		h.writeError(w, http.StatusGone, err.Error())
	default:
		h.logger.Error("Internal error handling request", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "internal server error")
	}
}
