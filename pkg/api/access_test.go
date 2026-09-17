package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// gatedRoute is one gated endpoint and the least level that may use it.
type gatedRoute struct {
	method string
	target string
	min    Level
}

// gatedRoutes is every route behind a session, with the level each one needs —
// the table in issue #204. A Rota Editor changes shifts: Alterations and Cover
// on an allocated rota, Preallocations on the one in flight, and the reads those
// need. Everything else is an Organiser's.
var gatedRoutes = []gatedRoute{
	// A Rota Editor's.
	{http.MethodPost, "/api/alterations", LevelRotaEditor},
	{http.MethodGet, "/api/preallocations", LevelRotaEditor},
	{http.MethodPost, "/api/preallocations", LevelRotaEditor},
	{http.MethodDelete, "/api/preallocations/p1", LevelRotaEditor},
	{http.MethodGet, "/api/rotations/in-flight", LevelRotaEditor},
	{http.MethodGet, "/api/volunteers", LevelRotaEditor},

	// An Organiser's. Times and Closed are shift edits, but not a Rota
	// Editor's: Closed is an allocator input, and the times go with it.
	{http.MethodPatch, "/api/shifts/s1", LevelOrganiser},
	{http.MethodPut, "/api/shifts/s1/shape", LevelOrganiser},
	{http.MethodPost, "/api/roles", LevelOrganiser},
	{http.MethodPut, "/api/roles/r1", LevelOrganiser},
	{http.MethodGet, "/api/rota-defaults", LevelOrganiser},
	{http.MethodPut, "/api/rota-defaults/shift-times", LevelOrganiser},
	{http.MethodPut, "/api/rota-defaults/shape", LevelOrganiser},
	{http.MethodPut, "/api/rota-defaults/allocation-settings", LevelOrganiser},
	{http.MethodGet, "/api/standing-preallocations", LevelOrganiser},
	{http.MethodPost, "/api/standing-preallocations", LevelOrganiser},
	{http.MethodDelete, "/api/standing-preallocations/sp1", LevelOrganiser},
	{http.MethodPost, "/api/rotations", LevelOrganiser},
	{http.MethodGet, "/api/rotations/proposed", LevelOrganiser},
	{http.MethodDelete, "/api/rotations/rota1", LevelOrganiser},
	{http.MethodPost, "/api/rotations/in-flight/allocation", LevelOrganiser},
	// Not even to read: a draft is the Organisers' working, not a Rota
	// Editor's business.
	{http.MethodGet, "/api/draft-rota-allocation", LevelOrganiser},
	{http.MethodPost, "/api/draft-rota-allocation", LevelOrganiser},
	{http.MethodPost, "/api/availability-rounds", LevelOrganiser},
	{http.MethodGet, "/api/availability-rounds", LevelOrganiser},
	{http.MethodGet, "/api/availability-sends/send1", LevelOrganiser},
	{http.MethodGet, "/auth/gmail", LevelOrganiser},
	{http.MethodPost, "/auth/sync", LevelOrganiser},
}

// Nobody without a session gets past any gate, whatever level it asks for.
func TestGatedRoutes_RefuseNoSession(t *testing.T) {
	handler := newTestHandler(&mockStore{}, testVolunteers())
	for _, route := range gatedRoutes {
		rec := doRequest(t, handler, route.method, route.target, "{}")
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s", route.method, route.target)
	}
}

// A signed session for someone on neither allowlist is no session at all.
func TestGatedRoutes_RefuseSomeoneOnNeitherList(t *testing.T) {
	handler := newTestHandler(&mockStore{}, testVolunteers())
	for _, route := range gatedRoutes {
		rec := doRequest(t, handler, route.method, route.target, "{}", sessionCookie("stranger@example.com"))
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s", route.method, route.target)
	}
}

// A Rota Editor is turned away from every Organiser route with a 403 — they are
// signed in, just not senior enough — and let through every route of their own.
// "Let through" is anything but the gate's two refusals: the requests carry no
// real body, so what the handler makes of them is not this test's business.
func TestGatedRoutes_RotaEditor(t *testing.T) {
	handler := newTestHandler(&mockStore{}, testVolunteers())
	for _, route := range gatedRoutes {
		rec := doRequest(t, handler, route.method, route.target, "{}", rotaEditorCookie())
		if route.min == LevelOrganiser {
			assert.Equal(t, http.StatusForbidden, rec.Code, "%s %s", route.method, route.target)
		} else {
			assert.NotContains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, rec.Code, "%s %s", route.method, route.target)
		}
	}
}

// An Organiser can do everything an Admin could.
func TestGatedRoutes_Organiser(t *testing.T) {
	handler := newTestHandler(&mockStore{}, testVolunteers())
	for _, route := range gatedRoutes {
		rec := doRequest(t, handler, route.method, route.target, "{}", organiserCookie())
		assert.NotContains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, rec.Code, "%s %s", route.method, route.target)
	}
}
