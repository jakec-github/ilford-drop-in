package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// A draft names people against Shifts on a rota nobody has decided yet, so an
// anonymous caller cannot ask for one to be solved — and the refusal comes
// before the solve, since starting a thirty-second subprocess for a stranger
// would be worth having even if it published nothing.
func TestSolveDraftRotaAllocationRequiresAdmin(t *testing.T) {
	store := &mockStore{}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, "/api/draft-rota-allocation", "")

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, store.storedDrafts, "nothing was solved, let alone stored")
}

// draftedRotaStore is one rota in flight of two Shifts, with a draft solved
// against it: Alice leading and Bob beside her on the first, nobody on the
// second. Each Shift asks for one Team lead and four Service volunteers, so the
// rota is asking for ten Seats and two of them are filled.
func draftedRotaStore() *mockStore {
	return &mockStore{
		rotations: []db.Rotation{{ID: "rota-1", Start: "2026-08-02", End: "2026-08-09", ShiftCount: 2}},
		shifts: []db.Shift{
			{ID: "shift-1", RotaID: "rota-1", Date: "2026-08-02", StartAt: "2026-08-02T19:30:00", EndAt: "2026-08-02T21:30:00"},
			{ID: "shift-2", RotaID: "rota-1", Date: "2026-08-09", StartAt: "2026-08-09T19:30:00", EndAt: "2026-08-09T21:30:00"},
		},
		draft: &db.DraftRotaAllocation{
			RotaID:       "rota-1",
			SolvedAt:     time.Date(2026, 8, 5, 6, 0, 0, 0, time.UTC),
			Success:      true,
			SolverStatus: "OPTIMAL",
		},
		draftSeats: []db.DraftAllocation{
			{ID: "seat-1", ShiftID: "shift-1", Role: "Service volunteer", VolunteerID: "bob"},
			{ID: "seat-2", ShiftID: "shift-1", Role: "Team lead", VolunteerID: "alice"},
		},
	}
}

// draftReadResponse is the read endpoint's body as a test reads it.
type draftReadResponse struct {
	Rota *struct {
		ID         string `json:"id"`
		SeatsAsked int    `json:"seatsAsked"`
	} `json:"rota"`
	Draft *struct {
		SolvedAt     string `json:"solvedAt"`
		Success      bool   `json:"success"`
		SolverStatus string `json:"solverStatus"`
		SeatsFilled  int    `json:"seatsFilled"`
		Shifts       []struct {
			ShiftID   string `json:"shiftId"`
			Assignees []struct {
				VolunteerID string `json:"volunteerId"`
				Name        string `json:"name"`
				Role        string `json:"role"`
			} `json:"assignees"`
		} `json:"shifts"`
	} `json:"draft"`
}

// The rota an admin watches take shape: who the solver put where, keyed by Shift
// so the page can lay the draft over the rota it is already showing.
func TestGetDraftRotaAllocation(t *testing.T) {
	rec := doRequest(t, newTestHandler(draftedRotaStore(), testVolunteers()), http.MethodGet, "/api/draft-rota-allocation", "", adminCookie())
	require.Equal(t, http.StatusOK, rec.Code)

	var resp draftReadResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	require.NotNil(t, resp.Rota)
	assert.Equal(t, "rota-1", resp.Rota.ID)
	assert.Equal(t, 10, resp.Rota.SeatsAsked, "two shifts asking for five Seats each")

	require.NotNil(t, resp.Draft)
	assert.Equal(t, "2026-08-05T06:00:00Z", resp.Draft.SolvedAt)
	assert.True(t, resp.Draft.Success)
	assert.Equal(t, "OPTIMAL", resp.Draft.SolverStatus)
	assert.Equal(t, 2, resp.Draft.SeatsFilled)

	// Only the Shift the draft placed anybody on, its people in the order the
	// rota shows them — by Role priority, so the team lead leads.
	require.Len(t, resp.Draft.Shifts, 1)
	assert.Equal(t, "shift-1", resp.Draft.Shifts[0].ShiftID)
	require.Len(t, resp.Draft.Shifts[0].Assignees, 2)
	assert.Equal(t, "Alice", resp.Draft.Shifts[0].Assignees[0].Name)
	assert.Equal(t, "Team lead", resp.Draft.Shifts[0].Assignees[0].Role)
	assert.Equal(t, "Bob", resp.Draft.Shifts[0].Assignees[1].Name)
}

// An anonymous visitor sees nothing of the draft. This is the gate ADR 0008 is
// built around: a draft names people against Shifts nobody has decided yet, and
// the rota page and its calendar feed are read by the very volunteers it names.
func TestGetDraftRotaAllocationRequiresAdmin(t *testing.T) {
	rec := doRequest(t, newTestHandler(draftedRotaStore(), testVolunteers()), http.MethodGet, "/api/draft-rota-allocation", "")

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.NotContains(t, rec.Body.String(), "Alice", "no drafted name reaches the body")
	assert.NotContains(t, rec.Body.String(), "shift-1")
}

// A rota nobody has solved for yet. The rota comes back — what it is asking for
// is worth saying on its own — and the draft is null rather than an empty one,
// because "not solved" and "solved and found nothing" are different answers.
func TestGetDraftRotaAllocation_NotSolvedYet(t *testing.T) {
	store := draftedRotaStore()
	store.draft = nil
	store.draftSeats = nil

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/draft-rota-allocation", "", adminCookie())
	require.Equal(t, http.StatusOK, rec.Code)

	var resp draftReadResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Rota)
	assert.Equal(t, 10, resp.Rota.SeatsAsked)
	assert.Nil(t, resp.Draft)
}

// Nothing in flight: the state between one rota going out and the next being
// defined. The endpoint answers rather than 404s — there is nothing to draft, and
// that is something the screen says.
func TestGetDraftRotaAllocation_NothingInFlight(t *testing.T) {
	store := draftedRotaStore()
	store.allocate("rota-1")

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/draft-rota-allocation", "", adminCookie())
	require.Equal(t, http.StatusOK, rec.Code)

	var resp draftReadResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Nil(t, resp.Rota)
	assert.Nil(t, resp.Draft)
}
