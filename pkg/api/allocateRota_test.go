package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const allocatePath = "/api/rotations/in-flight/allocation"

// Allocating is the act that publishes the rota: afterwards it reaches the
// public shift listing and the calendar feeds volunteers subscribe to. A
// stranger cannot start one, and the refusal comes before the solve.
func TestAllocateRotaInFlightRequiresAdmin(t *testing.T) {
	store := draftedRotaStore()

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, allocatePath, `{"draftHash":"abc"}`)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, store.insertedAllocations, "nothing was allocated")
	assert.Empty(t, store.allocatedRotaIDs)
}

// The request states the draft being confirmed. One that names none is refused
// rather than read as "allocate whatever the solver says now" — which is the
// one thing the design exists to prevent (ADR 0008) — and the refusal is a fast
// one, before the thirty-second solve.
func TestAllocateRotaInFlightRefusesAnUnstatedDraft(t *testing.T) {
	store := draftedRotaStore()

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, allocatePath, `{}`, adminCookie())

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "states the draft")
	assert.Empty(t, store.insertedAllocations)
}

// A body this endpoint cannot read is a client fault, not a rota that has
// moved, and it says so as a 400 rather than reaching the solver.
func TestAllocateRotaInFlightRejectsAnUnreadableBody(t *testing.T) {
	store := draftedRotaStore()

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, allocatePath, `{"draftHash":`, adminCookie())

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid request body")
	assert.Empty(t, store.insertedAllocations)
}

// Allocating confirms a draft, so a rota nobody has drafted cannot be
// allocated. It is a conflict rather than a bad request: the request is
// well-formed, the rota is simply not at a stage where allocating means
// anything yet.
func TestAllocateRotaInFlightRefusesWithNoDraft(t *testing.T) {
	store := draftedRotaStore()
	store.storedDrafts = nil
	store.draftSeats = nil

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, allocatePath, `{"draftHash":"abc"}`, adminCookie())

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "drafted")
	assert.Empty(t, store.insertedAllocations)
}

// The rota went out while this admin had the page open. There is no rota in
// flight to allocate, and saying so beats a solve that would be thrown away.
func TestAllocateRotaInFlightRefusesAnAllocatedRota(t *testing.T) {
	store := draftedRotaStore()
	store.allocate("rota-1")

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, allocatePath, `{"draftHash":"abc"}`, adminCookie())

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "no rota in flight")
	assert.Empty(t, store.insertedAllocations)
}

// Allocating and drafting take the same solve slot, and allocating waits its
// turn for it rather than being refused (issue #179). A solve landing while it
// queues may have moved the rota, which is not a reason to refuse: it is the
// hash guard's ordinary business, and what comes back is the fresh rota to
// confirm instead.
//
// Nothing is committed here — the solve it eventually runs is refused at the
// first gate the mock store presents — which is what proves it waited rather
// than answered.
func TestAllocateRotaInFlightWaitsForTheRunningSolve(t *testing.T) {
	store := draftedRotaStore()
	handler := NewHandler(store, testVolunteers(), apiTestCfg, newTestAuthenticator(), nil, nil, zap.NewNop())
	require.NoError(t, handler.drafts.acquire(t.Context()), "the slot starts free")

	answered := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		answered <- doRequest(t, handler.Routes(), http.MethodPost, allocatePath, `{"draftHash":"abc"}`, adminCookie())
	}()

	select {
	case rec := <-answered:
		t.Fatalf("the allocation answered while a solve held the slot: %s", rec.Body.String())
	case <-time.After(50 * time.Millisecond):
	}

	handler.drafts.release()
	select {
	case rec := <-answered:
		assert.NotContains(t, rec.Body.String(), "already running", "nothing is refused for being queued")
		assert.Empty(t, store.insertedAllocations, "and the solve it went on to run committed nothing")
	case <-time.After(5 * time.Second):
		t.Fatal("the allocation never took the slot it was waiting for")
	}
}

// The draft an admin reads carries the fingerprint they allocate by. Without it
// on the wire there is nothing for them to confirm, so it is part of the draft
// rather than a second endpoint to ask.
func TestGetDraftRotaAllocationCarriesTheHashToConfirm(t *testing.T) {
	rec := doRequest(t, newTestHandler(draftedRotaStore(), testVolunteers()), http.MethodGet, "/api/draft-rota-allocation", "", adminCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body draftRotaAllocationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.NotEmpty(t, body.Hash)
}
