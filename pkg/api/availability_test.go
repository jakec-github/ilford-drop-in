package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// TestAvailabilityRoundEndpointsRequireAdmin: a round's roster hands out every
// volunteer's link, which is a bearer credential for their availability. An
// anonymous caller must not be able to read one, nor mint one.
func TestAvailabilityRoundEndpointsRequireAdmin(t *testing.T) {
	store := &mockStore{
		rotations: []db.Rotation{{ID: "rota-1", Start: "2026-08-02", End: "2026-08-09", ShiftCount: 2}},
		shifts: []db.Shift{
			{ID: "shift-1", RotaID: "rota-1", Date: "2026-08-02"},
			{ID: "shift-2", RotaID: "rota-1", Date: "2026-08-09"},
		},
	}
	handler := newTestHandler(store, testVolunteers())

	rec := doRequest(t, handler, http.MethodPost, "/api/availability-rounds", `{}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, store.availabilityRequests, "an unauthenticated request must not mint anything")

	rec = doRequest(t, handler, http.MethodGet, "/api/availability-rounds", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestAvailabilityFormRejectsAnUnknownToken: a mistyped or retired link is a
// 404, and says nothing about whether any link would have worked.
func TestAvailabilityFormRejectsAnUnknownToken(t *testing.T) {
	handler := newTestHandler(&mockStore{
		rotations: []db.Rotation{{ID: "rota-1", Start: "2026-08-02", End: "2026-08-02", ShiftCount: 1}},
		shifts:    []db.Shift{{ID: "shift-1", RotaID: "rota-1", Date: "2026-08-02"}},
	}, testVolunteers())

	rec := doRequest(t, handler, http.MethodGet, "/api/availability/not-a-token", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec = doRequest(t, handler, http.MethodPost, "/api/availability/not-a-token", `{"shiftIds":[]}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestAvailabilityLinkIsGoneOnceAllocated: the rota is out, so a changed answer
// can no longer affect anything. 410 rather than 404 tells the holder they are
// late rather than wrong.
func TestAvailabilityLinkIsGoneOnceAllocated(t *testing.T) {
	store := &mockStore{
		rotations: []db.Rotation{{
			ID: "rota-1", Start: "2026-08-02", End: "2026-08-02", ShiftCount: 1,
			AllocatedDatetime: "2026-07-30T09:00:00Z",
		}},
		shifts: []db.Shift{{ID: "shift-1", RotaID: "rota-1", Date: "2026-08-02"}},
		availabilityRequests: []db.AvailabilityRequestV2{
			{ID: "req-1", RotaID: "rota-1", VolunteerID: "bob", Token: "bob-token"},
		},
	}
	handler := newTestHandler(store, testVolunteers())

	rec := doRequest(t, handler, http.MethodGet, "/api/availability/bob-token", "")
	assert.Equal(t, http.StatusGone, rec.Code)

	rec = doRequest(t, handler, http.MethodPost, "/api/availability/bob-token", `{"shiftIds":["shift-1"]}`)
	assert.Equal(t, http.StatusGone, rec.Code)
}

// TestAvailabilityRoundLinksAreAbsolute: the roster's whole job is to be copied
// out of, so an entry has to carry a link a volunteer can paste into a browser,
// not a token they would have to assemble one from.
func TestAvailabilityRoundLinksAreAbsolute(t *testing.T) {
	store := &mockStore{
		rotations: []db.Rotation{{ID: "rota-1", Start: "2026-08-02", End: "2026-08-02", ShiftCount: 1}},
		shifts:    []db.Shift{{ID: "shift-1", RotaID: "rota-1", Date: "2026-08-02"}},
	}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, "/api/availability-rounds", `{}`, adminCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	require.NotEmpty(t, store.availabilityRequests)
	assert.Contains(t, rec.Body.String(), "http://example.com/availability/"+store.availabilityRequests[0].Token)
}
