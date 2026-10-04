package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/allocator"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// recordingPublisher counts the times a handler told it the rota sheet may be
// out of date. The publish itself is not the handlers' business, so telling is
// all these tests look for.
type recordingPublisher struct{ triggers atomic.Int32 }

func (p *recordingPublisher) Trigger() { p.triggers.Add(1) }

func handlerPublishingTo(store *mockStore, publisher RotaPublisher) *Handler {
	return NewHandler(store, testVolunteers(), apiTestCfg, newTestAuthenticator(), nil, nil, publisher, zap.NewNop())
}

// stubSolver writes an executable standing in for pyallocator that answers
// output whatever it is asked. Allocating re-solves, and these tests are about
// what the handler does once it has, not about CP-SAT.
func stubSolver(t *testing.T, output allocator.CpsatOutput) string {
	t.Helper()
	payload, err := json.Marshal(output)
	require.NoError(t, err)
	// Drain stdin before answering: a script that exits without reading would
	// break the pipe the runner writes the problem into.
	script := "#!/bin/sh\ncat > /dev/null\ncat <<'CPSAT_OUTPUT'\n" + string(payload) + "\nCPSAT_OUTPUT\n"
	path := filepath.Join(t.TempDir(), "stub-pyallocator")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
	return path
}

// allocatableStore is draftedRotaStore with its availability round started,
// which allocating asks for before it solves anything.
func allocatableStore() *mockStore {
	store := draftedRotaStore()
	store.availabilityRequests = []db.AvailabilityRequest{
		{ID: "req-alice", RotaID: "rota-1", VolunteerID: "alice", Token: "tok-alice"},
		{ID: "req-bob", RotaID: "rota-1", VolunteerID: "bob", Token: "tok-bob"},
	}
	return store
}

// draftedSolve is what solving draftedRotaStore's rota answers: the draft it
// already holds, so allocating confirms it.
func draftedSolve() allocator.CpsatOutput {
	return allocator.CpsatOutput{SolverStatus: "OPTIMAL", Success: true, Shifts: []allocator.CpsatOutputShift{
		{Index: 0, Date: "2026-08-02", Assignments: []allocator.CpsatAssignment{
			{VolunteerID: "bob", Role: "Service volunteer"},
			{VolunteerID: "alice", Role: "Team lead"},
		}},
		{Index: 1, Date: "2026-08-09"},
	}}
}

func draftHash(t *testing.T, handler *Handler) string {
	t.Helper()
	rec := doRequest(t, handler.Routes(), http.MethodGet, "/api/draft-rota-allocation", "", organiserCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body draftRotaAllocationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.NotEmpty(t, body.Hash)
	return body.Hash
}

// Allocating a rota makes it the rota on the sheet (issue #191).
func TestAllocatingARotaPublishesIt(t *testing.T) {
	publisher := &recordingPublisher{}
	store := allocatableStore()
	handler := handlerPublishingTo(store, publisher)
	handler.solverPython = stubSolver(t, draftedSolve())

	rec := doRequest(t, handler.Routes(), http.MethodPost, allocatePath,
		`{"draftHash":"`+draftHash(t, handler)+`"}`, organiserCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotEmpty(t, store.insertedAllocations, "the rota was allocated")
	assert.Equal(t, int32(1), publisher.triggers.Load())
}

// An allocation refused because the rota moved allocated nothing, so there is
// nothing new for the sheet.
func TestARefusedAllocationPublishesNothing(t *testing.T) {
	publisher := &recordingPublisher{}
	handler := handlerPublishingTo(allocatableStore(), publisher)
	handler.solverPython = stubSolver(t, draftedSolve())

	rec := doRequest(t, handler.Routes(), http.MethodPost, allocatePath, `{"draftHash":"not-what-was-shown"}`, organiserCookie())

	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Zero(t, publisher.triggers.Load())
}

// Every change to the rota could change the sheet, and one that is refused
// changes nothing.
func TestChangesThatCouldShowOnTheSheetPublishIt(t *testing.T) {
	roleID := "4c1e2f8a-0b3d-4a5e-8c9f-1a2b3c4d5e6f"
	tests := []struct {
		name   string
		store  func() *mockStore
		method string
		path   string
		body   string
		status int
		// publishes is whether the sheet is told.
		publishes bool
	}{
		{
			name: "a change to who is on a shift", store: alterationTestStore,
			method: http.MethodPost, path: "/api/alterations",
			body:   `{"date":"2026-01-11","out":"bob","in":"charlie","roleId":"role-service-volunteer","reason":"Holiday cover"}`,
			status: http.StatusCreated, publishes: true,
		},
		{
			name: "a refused change to who is on a shift", store: alterationTestStore,
			method: http.MethodPost, path: "/api/alterations",
			body:   `{"date":"2026-01-11","out":"nobody-here","reason":"Ill"}`,
			status: http.StatusNotFound, publishes: false,
		},
		{
			name: "an edit to a shift", store: shiftEditTestStore,
			method: http.MethodPatch, path: "/api/shifts/s1", body: `{"closed":true}`,
			status: http.StatusOK, publishes: true,
		},
		{
			name: "an edit to a shift that does not exist", store: shiftEditTestStore,
			method: http.MethodPatch, path: "/api/shifts/nope", body: `{"closed":true}`,
			status: http.StatusNotFound, publishes: false,
		},
		{
			name: "a new Role", store: func() *mockStore { return &mockStore{roles: []db.Role{}} },
			method: http.MethodPost, path: "/api/roles", body: `{"name":"Food collector","priority":3,"colour":"amber"}`,
			status: http.StatusCreated, publishes: true,
		},
		{
			name: "an edit to a Role",
			store: func() *mockStore {
				return &mockStore{roles: []db.Role{{ID: roleID, Name: "Team lead", Priority: 1, Colour: "violet"}}}
			},
			method: http.MethodPut, path: "/api/roles/" + roleID, body: `{"name":"Shift lead","priority":1,"colour":"violet"}`,
			status: http.StatusOK, publishes: true,
		},
		{
			name: "an edit to a Role that does not exist", store: func() *mockStore { return &mockStore{roles: []db.Role{}, roleMissing: true} },
			method: http.MethodPut, path: "/api/roles/" + roleID, body: `{"name":"Shift lead","priority":1,"colour":"violet"}`,
			status: http.StatusNotFound, publishes: false,
		},
		{
			name: "reading the rota", store: shiftEditTestStore,
			method: http.MethodGet, path: "/api/shifts",
			status: http.StatusOK, publishes: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			publisher := &recordingPublisher{}
			handler := handlerPublishingTo(tt.store(), publisher)

			rec := doRequest(t, handler.Routes(), tt.method, tt.path, tt.body, organiserCookie())

			require.Equal(t, tt.status, rec.Code, rec.Body.String())
			if tt.publishes {
				assert.Equal(t, int32(1), publisher.triggers.Load())
			} else {
				assert.Zero(t, publisher.triggers.Load())
			}
		})
	}
}
