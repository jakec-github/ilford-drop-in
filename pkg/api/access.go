package api

import (
	"context"
	"net/http"

	"github.com/jakechorley/ilford-drop-in/internal/config"
)

// access is one Organisation's allowlists, folded for comparison. A session
// proves who someone is everywhere; this is what they may do here (ADR 0011).
type access struct {
	organisers  map[string]struct{}
	rotaEditors map[string]struct{}
}

// accessFor folds an Organisation's configured allowlists.
func accessFor(org *config.Organisation) access {
	return access{
		organisers:  allowlist(org.OrganiserEmails),
		rotaEditors: allowlist(org.RotaEditorEmails),
	}
}

// allowlist folds configured addresses into the lookup set levelOf checks.
func allowlist(emails []string) map[string]struct{} {
	folded := make(map[string]struct{}, len(emails))
	for _, e := range emails {
		folded[normaliseEmail(e)] = struct{}{}
	}
	return folded
}

// levelOf reports the level email holds (case-insensitive), and false when it
// is on neither allowlist. Someone on both is an Organiser: that list already
// allows everything the other does.
func (a access) levelOf(email string) (Level, bool) {
	folded := normaliseEmail(email)
	if _, ok := a.organisers[folded]; ok {
		return LevelOrganiser, true
	}
	if _, ok := a.rotaEditors[folded]; ok {
		return LevelRotaEditor, true
	}
	return "", false
}

// session returns who the request comes from and what they may do in this
// Organisation. A valid cookie for someone on none of its lists is no session
// here at all: being signed in for one Organisation says nothing about another.
func (h *Handler) session(r *http.Request) (Session, bool) {
	email, ok := h.auth.emailFromRequest(r)
	if !ok {
		return Session{}, false
	}
	level, ok := h.access.levelOf(email)
	if !ok {
		return Session{}, false
	}
	return Session{Email: email, Level: level}, true
}

// sessionEmailContextKey keys the verified email stashed in a request's context
// by requireLevel. Unexported so only this package can set or read it.
type sessionEmailContextKey struct{}

// requireLevel wraps a handler, allowing it through only for a session at min
// or above in this Organisation. No session is a 401; a session too junior for
// the route is a 403, because the person is known and simply may not. It
// re-checks the allowlists on every request, so removing someone from config —
// or moving them from one list to the other — takes effect on their
// still-valid cookie on the next request after reload. The verified email is
// stashed in the request context so gated handlers can attribute the action
// without re-parsing the cookie.
func (h *Handler) requireLevel(min Level, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := h.session(r)
		if !ok {
			http.Error(w, "not authorised", http.StatusUnauthorized)
			return
		}
		if !session.Level.atLeast(min) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		ctx := context.WithValue(r.Context(), sessionEmailContextKey{}, session.Email)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// sessionEmail returns the verified email requireLevel stashed in ctx. It is
// only present on requests that passed through requireLevel; the empty string
// means the handler was not gated.
func sessionEmail(ctx context.Context) string {
	email, _ := ctx.Value(sessionEmailContextKey{}).(string)
	return email
}

// handleMe reports who is signed in and at what level in this Organisation, or
// 401 if nobody is — including someone signed in who is on none of its lists.
// Used by the frontend to show signed-in state and to decide which screens and
// controls to offer.
func (h *Handler) handleMe(w http.ResponseWriter, r *http.Request) {
	session, ok := h.session(r)
	if !ok {
		h.writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	h.writeJSON(w, http.StatusOK, session)
}

// handleLogin sends the browser to the shared login, saying which Organisation
// to come back to. Each Organisation's pages link here, so the page needs to
// know nothing about where login lives or how it is told the way back.
// Anything else on the query — dev mode's ?level= — goes along with it.
func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	query.Set("org", h.org.ID)
	http.Redirect(w, r, "/auth/login?"+query.Encode(), http.StatusFound)
}
