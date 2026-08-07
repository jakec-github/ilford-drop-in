package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strings"

	"go.uber.org/zap"
	"golang.org/x/oauth2"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// Store defines the database operations the API needs (satisfied by *db.DB)
type Store interface {
	services.AvailabilityStore
	services.ChangeRotaStore
	services.DefaultShapeWriteStore
	services.DefineRotaStore
	services.ListShiftsStore
	services.PreallocationStore
	services.RoleWriteStore
	services.RotaDefaultsStore
	services.RotaDefaultsWriteStore
	services.ShiftShapeWriteStore
	services.UpdateShiftStore
	services.StandingPreallocationStore
	// Ping reports whether the database is reachable, for GET /health.
	Ping(ctx context.Context) error
}

// MailerFunc builds a mail client from an admin's freshly-granted Gmail access
// token. It is injected rather than constructed here so the server's Google
// dependencies stay in the composition root, and so dev mode can hand over a
// client that writes emails to the log instead of sending them.
//
// The token is short-lived and carries no refresh token: it is used for one send
// and discarded. It is nil in dev mode, where nothing granted anything.
type MailerFunc func(ctx context.Context, token *oauth2.Token) (services.GmailClient, error)

// Handler serves the HTTP API
type Handler struct {
	store      Store
	volunteers services.VolunteerClient
	cfg        *config.Config
	auth       *Authenticator
	frontend   fs.FS
	logger     *zap.Logger
	// newMailer builds the client a send mails through. Never nil: NewHandler
	// substitutes one that refuses, so a server wired without mail says so on
	// the first send rather than panicking.
	newMailer MailerFunc
	// sends holds the availability sends in flight. They are jobs rather than
	// requests because a round takes about ninety seconds — see sendjobs.go.
	sends *sendJobs
}

// NewHandler creates an API handler with its dependencies. frontend is the
// embedded frontend build; pass nil (or a build-less placeholder) to serve the
// API only. newMailer is how availability sends reach Gmail; nil disables
// sending rather than failing at startup, because everything else works without
// it.
func NewHandler(store Store, volunteers services.VolunteerClient, cfg *config.Config, auth *Authenticator, frontend fs.FS, newMailer MailerFunc, logger *zap.Logger) *Handler {
	if newMailer == nil {
		newMailer = func(context.Context, *oauth2.Token) (services.GmailClient, error) {
			return nil, errors.New("this server is not configured to send mail")
		}
	}

	h := &Handler{
		store:      store,
		volunteers: volunteers,
		cfg:        cfg,
		auth:       auth,
		frontend:   frontend,
		logger:     logger,
		newMailer:  newMailer,
		sends:      newSendJobs(),
	}

	// The gmail.send grant comes back through the login callback, which the
	// Authenticator owns, but completing it needs the store and the roster,
	// which only the Handler has. This is the join between the two.
	auth.completeSend = h.completeGmailSend

	return h
}

// apiPrefix is the namespace the JSON API owns. Everything outside it belongs to
// the frontend, so the two can name things freely without colliding.
const apiPrefix = "/api"

// Routes returns the API's route table.
//
// The data endpoints live under /api and the frontend gets everything else,
// which is what lets a page and its payload share a name — /availability/{token}
// is the volunteer's page, /api/availability/{token} is the JSON behind it — and
// what makes a mistyped endpoint a 404 rather than index.html with a 200.
//
// Three paths deliberately stay unprefixed: /health, which the deploy tooling
// and scripts/dev-stack.sh poll; /auth, a browser redirect flow whose callback
// URI is registered with Google; and /calendars/{filename}, whose URLs are
// subscribed to from volunteers' calendar apps and so cannot be moved without
// breaking subscriptions that live outside this app.
func (h *Handler) Routes() http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("GET /shifts", h.handleListShifts)
	// Editing a Shift is admin-only, and admin-only for a reason the listing is
	// not: closing one is an allocator input, and the rota is solved around it.
	api.Handle("PATCH /shifts/{id}", h.auth.requireAdmin(http.HandlerFunc(h.handleUpdateShift)))
	// A Shape is its own resource under the Shift rather than another field of
	// the PATCH above, because it is written whole: a Role left out is a Role
	// the Shift no longer asks for, which a patch of the Shift could not say
	// without "an absent field means unchanged" and "an absent Role means gone"
	// meaning opposite things in one body.
	api.Handle("PUT /shifts/{id}/shape", h.auth.requireAdmin(http.HandlerFunc(h.handleSaveShiftShape)))
	// Public alongside the rota: it is what tells a client which Roles exist
	// and what each is drawn in, and the rota names Roles on every chip. The
	// writes beside it are admin-only — which Roles exist is a decision about
	// how the drop-in runs — and there is no DELETE, because a Role is
	// permanent (ADR 0006).
	api.HandleFunc("GET /roles", h.handleListRoles)
	api.Handle("POST /roles", h.auth.requireAdmin(http.HandlerFunc(h.handleCreateRole)))
	api.Handle("PUT /roles/{id}", h.auth.requireAdmin(http.HandlerFunc(h.handleUpdateRole)))
	// The settings record, admin-only throughout. Unlike the Roles beside it on
	// the same screen, nothing a logged-out visitor sees needs it: the shift
	// times already reach the public on GET /shifts, and the sections joining
	// this one are an admin's business.
	//
	// One GET for the record and a PUT per section, each named. The screen
	// draws every section from the one read; a section is written whole — the
	// times are one idea, a Shape has no partial edit, and the toggles are one
	// list — but the record is not, because a save of one section must not
	// blank another, and a single PUT on the record would be exactly the
	// endpoint that could.
	api.Handle("GET /rota-defaults", h.auth.requireAdmin(http.HandlerFunc(h.handleGetRotaDefaults)))
	api.Handle("PUT /rota-defaults/shift-times", h.auth.requireAdmin(http.HandlerFunc(h.handleSaveShiftTimeDefaults)))
	api.Handle("PUT /rota-defaults/shape", h.auth.requireAdmin(http.HandlerFunc(h.handleSaveDefaultShape)))
	api.Handle("PUT /rota-defaults/allocation-settings", h.auth.requireAdmin(http.HandlerFunc(h.handleSaveAllocationSettings)))
	api.Handle("POST /rotations", h.auth.requireAdmin(http.HandlerFunc(h.handleDefineRota)))
	api.Handle("POST /alterations", h.auth.requireAdmin(http.HandlerFunc(h.handleCreateAlteration)))
	// Reading pins is admin-only alongside writing them: a listing names people
	// against dates whose rota has not been allocated, let alone published, and
	// nothing outside the admin UI has any use for it.
	// The Standing Preallocations, part of the Rota Defaults: the pins an admin
	// expects to make every rota, which seed ordinary Preallocations when one is
	// defined. Admin-only throughout, like the settings screen they live on.
	// There is no PUT — a promise is made or it is not, and editing one is
	// removing it and making the one that was meant.
	api.Handle("GET /standing-preallocations", h.auth.requireAdmin(http.HandlerFunc(h.handleListStandingPreallocations)))
	api.Handle("POST /standing-preallocations", h.auth.requireAdmin(http.HandlerFunc(h.handleCreateStandingPreallocation)))
	api.Handle("DELETE /standing-preallocations/{id}", h.auth.requireAdmin(http.HandlerFunc(h.handleDeleteStandingPreallocation)))
	api.Handle("GET /preallocations", h.auth.requireAdmin(http.HandlerFunc(h.handleListPreallocations)))
	api.Handle("POST /preallocations", h.auth.requireAdmin(http.HandlerFunc(h.handleCreatePreallocation)))
	api.Handle("DELETE /preallocations/{id}", h.auth.requireAdmin(http.HandlerFunc(h.handleDeletePreallocation)))
	api.Handle("GET /volunteers", h.auth.requireAdmin(http.HandlerFunc(h.handleListVolunteers)))
	// Rounds are admin-only: the roster hands out every volunteer's link, which
	// is a bearer credential for their availability.
	api.Handle("POST /availability-rounds", h.auth.requireAdmin(http.HandlerFunc(h.handleMintAvailabilityRound)))
	api.Handle("GET /availability-rounds", h.auth.requireAdmin(http.HandlerFunc(h.handleGetAvailabilityRound)))
	// A send is watched, never started, from under /api: starting one is a
	// browser redirect to Google for the gmail.send grant, which lives at
	// /auth/gmail alongside the rest of the OAuth flow.
	api.Handle("GET /availability-sends/{id}", h.auth.requireAdmin(http.HandlerFunc(h.handleGetSend)))
	// The volunteer's own link, public by design — the link is the identity and
	// volunteers never log in. Registered under a separate prefix from the
	// admin rounds above so neither path can shadow the other.
	api.HandleFunc("GET /availability/{token}", h.handleAvailabilityForm)
	api.HandleFunc("POST /availability/{token}", h.handleSubmitAvailability)

	mux := http.NewServeMux()
	mux.Handle(apiPrefix+"/", http.StripPrefix(apiPrefix, h.apiRouter(api)))
	mux.HandleFunc("GET /health", h.handleHealth)
	mux.HandleFunc("GET /calendars/{filename}", h.handleCalendar)
	h.auth.registerRoutes(mux)
	// Sits under /auth rather than /api because it is the same browser redirect
	// dance as login and shares its registered callback URI — it is an OAuth
	// endpoint that happens to start a send, not a data endpoint.
	mux.Handle("GET /auth/gmail", h.auth.requireAdmin(http.HandlerFunc(h.handleGmailConsent)))
	if hasFrontend(h.frontend) {
		// Registered without a method: a pattern matching fewer methods than
		// /api/ but more paths conflicts with it. frontendHandler turns away
		// anything but GET and HEAD itself.
		mux.Handle("/", frontendHandler(h.frontend))
	} else {
		h.logger.Info("No frontend build embedded; serving API only")
	}
	return mux
}

// apiRouter serves the API mux, answering anything it does not route in JSON.
// A client under /api is parsing JSON, so it must not be handed net/http's
// plain-text 404 or 405 — the error body is the one place an unknown endpoint
// gets to explain itself.
func (h *Handler) apiRouter(api *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := api.Handler(r); pattern != "" {
			api.ServeHTTP(w, r)
			return
		}
		// No pattern matched, which the mux reports the same way whether the
		// path is unknown or only the method is. Asking it about the other
		// methods tells the two apart.
		if allowed := allowedMethods(api, r); len(allowed) > 0 {
			w.Header().Set("Allow", strings.Join(allowed, ", "))
			h.writeError(w, http.StatusMethodNotAllowed, r.Method+" is not allowed on "+apiPrefix+r.URL.Path)
			return
		}
		h.writeError(w, http.StatusNotFound, "unknown endpoint: "+apiPrefix+r.URL.Path)
	})
}

// allowedMethods reports which methods the mux would route this request's path
// under, in the order they would be listed in an Allow header.
func allowedMethods(api *http.ServeMux, r *http.Request) []string {
	var allowed []string
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		// Only the method, URL and Host take part in matching, so a probe needs
		// nothing else — and carrying no body keeps it free of the real one.
		probe := &http.Request{Method: method, URL: r.URL, Host: r.Host}
		if _, pattern := api.Handler(probe); pattern != "" {
			allowed = append(allowed, method)
		}
	}
	return allowed
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
