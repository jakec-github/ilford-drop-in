package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// Store defines the database operations the API needs (satisfied by *db.DB)
type Store interface {
	services.ChangeRotaStore
	services.ListShiftsStore
	services.PreallocationStore
}

// Handler serves the HTTP API
type Handler struct {
	store      Store
	volunteers services.VolunteerClient
	cfg        *config.Config
	auth       *Authenticator
	logger     *zap.Logger
}

// NewHandler creates an API handler with its dependencies
func NewHandler(store Store, volunteers services.VolunteerClient, cfg *config.Config, auth *Authenticator, logger *zap.Logger) *Handler {
	return &Handler{
		store:      store,
		volunteers: volunteers,
		cfg:        cfg,
		auth:       auth,
		logger:     logger,
	}
}

// Routes returns the API's route table
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /shifts", h.handleListShifts)
	mux.Handle("POST /alterations", h.auth.requireAdmin(http.HandlerFunc(h.handleCreateAlteration)))
	mux.HandleFunc("GET /preallocations", h.handleListPreallocations)
	mux.Handle("POST /preallocations", h.auth.requireAdmin(http.HandlerFunc(h.handleCreatePreallocation)))
	mux.Handle("DELETE /preallocations/{id}", h.auth.requireAdmin(http.HandlerFunc(h.handleDeletePreallocation)))
	mux.HandleFunc("GET /calendars/{filename}", h.handleCalendar)
	h.auth.registerRoutes(mux)
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
	default:
		h.logger.Error("Internal error handling request", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "internal server error")
	}
}
