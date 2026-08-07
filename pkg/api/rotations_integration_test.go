package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
	"github.com/jakechorley/ilford-drop-in/pkg/db/dbtest"
)

// TestDefineRotaEndpointIntegration drives POST /rotations against a real
// Postgres, proving the rotation and shift rows actually land — the mock store
// can only show that the handler asked. It then reads the shifts back through
// GET /shifts, which is the state an agent seeding a dev stack depends on
// (issue #75).
func TestDefineRotaEndpointIntegration(t *testing.T) {
	database, _ := dbtest.New(t)
	dbtest.SeedRoles(t, database)
	dbtest.SeedDefaultShape(t, database)
	dbtest.SeedRotaDefaults(t, database)
	ctx := context.Background()
	handler := NewHandler(database, testVolunteers(), apiTestCfg, newTestAuthenticator(), nil, nil, zap.NewNop()).Routes()

	rec := doRequest(t, handler, http.MethodPost, "/api/rotations", `{"shiftCount":3}`, adminCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var resp defineRotaResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Shifts, 3)

	// The rotation row is there, and its derived span matches what was reported.
	rotations, err := database.GetRotations(ctx)
	require.NoError(t, err)
	require.Len(t, rotations, 1)
	assert.Equal(t, resp.Rotation.ID, rotations[0].ID)
	assert.Equal(t, resp.Rotation.Start, rotations[0].Start)
	assert.Equal(t, resp.Rotation.End, rotations[0].End)
	assert.Equal(t, 3, rotations[0].ShiftCount)

	// So are the shifts, under the ids the response named.
	shifts, err := database.GetShiftsByRotaID(ctx, resp.Rotation.ID)
	require.NoError(t, err)
	require.Len(t, shifts, 3)
	for i, s := range shifts {
		assert.Equal(t, resp.Shifts[i].ID, s.ID)
		assert.Equal(t, resp.Shifts[i].Date, s.Date)
	}

	// A second call is refused while the first rota is in flight (issue #139).
	rec = doRequest(t, handler, http.MethodPost, "/api/rotations", `{"shiftCount":1}`, adminCookie())
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

	// Once the first rota has been allocated it defines the following rota
	// rather than replacing this one.
	require.NoError(t, database.InsertAllocationsAndSetAllocated(ctx,
		[]db.Allocation{{ID: uuid.New().String(), ShiftID: resp.Shifts[0].ID, Role: "Service volunteer", VolunteerID: "alice"}},
		resp.Rotation.ID, time.Now()))

	rec = doRequest(t, handler, http.MethodPost, "/api/rotations", `{"shiftCount":1}`, adminCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var second defineRotaResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &second))
	assert.Greater(t, second.Rotation.Start, resp.Rotation.End)

	// GET /shifts serves the minted shifts of both rotas.
	rec = doRequest(t, handler, http.MethodGet, "/api/shifts?from="+resp.Rotation.Start, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var listed struct {
		Shifts []struct {
			Date      string `json:"date"`
			Allocated bool   `json:"allocated"`
			Assignees []struct {
				Name string `json:"name"`
			} `json:"assignees"`
		} `json:"shifts"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listed))
	require.Len(t, listed.Shifts, 4, "three shifts from the first rota, one from the second")
	assert.Equal(t, resp.Shifts[0].Date, listed.Shifts[0].Date)

	// The second rota's shift is minted, unallocated and with nobody on it: the
	// state #31 needs to request availability against.
	newest := listed.Shifts[3]
	assert.Equal(t, second.Shifts[0].Date, newest.Date)
	assert.False(t, newest.Allocated)
	assert.Empty(t, newest.Assignees)
}

// TestRotaLifecycleEndpointsIntegration drives the one-rota-in-flight rule and
// its release valve against a real Postgres: what the mock store can only show
// the handler asking for, this shows actually happening to the rows.
//
// The discard is the part that needs real tables. Its guarantee is that a
// rotation, its shifts, their Shapes, its pins and its whole round go together
// or not at all, and that is a claim about foreign keys and one transaction —
// nothing an in-memory store can be wrong about (issue #139).
func TestRotaLifecycleEndpointsIntegration(t *testing.T) {
	database, _ := dbtest.New(t)
	dbtest.SeedRoles(t, database)
	dbtest.SeedDefaultShape(t, database)
	dbtest.SeedRotaDefaults(t, database)
	ctx := context.Background()
	handler := NewHandler(database, testVolunteers(), apiTestCfg, newTestAuthenticator(), nil, nil, zap.NewNop()).Routes()

	// Nothing defined yet, so nothing is in flight — the state a rota may be
	// defined in.
	rec := doRequest(t, handler, http.MethodGet, "/api/rotations/in-flight", "", adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var inFlight rotaInFlightBodyResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &inFlight))
	assert.Nil(t, inFlight.Rotation)

	rec = doRequest(t, handler, http.MethodPost, "/api/rotations", `{"shiftCount":3}`, adminCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var defined defineRotaResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &defined))

	// Pin someone, and put a round out with one answer in it, so the discard has
	// something of every kind to take with it.
	pin := `{"date":"` + defined.Shifts[0].Date + `","volunteerId":"alice","role":"Team lead"}`
	rec = doRequest(t, handler, http.MethodPost, "/api/preallocations", pin, adminCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	rec = doRequest(t, handler, http.MethodPost, "/api/availability-rounds", "", adminCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	requests, err := database.GetAvailabilityRequestsByRotaID(ctx, defined.Rotation.ID)
	require.NoError(t, err)
	require.NotEmpty(t, requests)
	require.NoError(t, database.MarkAvailabilityRequestSent(ctx, requests[0].ID))
	rec = doRequest(t, handler, http.MethodPost, "/api/availability/"+requests[0].Token,
		`{"shiftIds":["`+defined.Shifts[0].ID+`"]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// The rota in flight is that rota, and it reports the round hanging off it.
	rec = doRequest(t, handler, http.MethodGet, "/api/rotations/in-flight", "", adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &inFlight))
	require.NotNil(t, inFlight.Rotation)
	assert.Equal(t, defined.Rotation.ID, inFlight.Rotation.ID)
	assert.Equal(t, 3, inFlight.Rotation.ShiftCount)
	assert.Equal(t, len(requests), inFlight.Rotation.Asked)
	assert.Equal(t, 1, inFlight.Rotation.Sent)
	assert.Equal(t, 1, inFlight.Rotation.Replied)

	// Discard takes all of it, in one transaction.
	rec = doRequest(t, handler, http.MethodDelete, "/api/rotations/"+defined.Rotation.ID, "", adminCookie())
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	rotations, err := database.GetRotations(ctx)
	require.NoError(t, err)
	assert.Empty(t, rotations)
	shifts, err := database.GetShiftsByRotaID(ctx, defined.Rotation.ID)
	require.NoError(t, err)
	assert.Empty(t, shifts)
	shapes, err := database.GetShiftShapes(ctx, []string{defined.Shifts[0].ID})
	require.NoError(t, err)
	assert.Empty(t, shapes, "a Shape does not outlive the Shift it belongs to")
	pins, err := database.GetPreallocationsByShiftIDs(ctx, []string{defined.Shifts[0].ID})
	require.NoError(t, err)
	assert.Empty(t, pins)
	remaining, err := database.GetAvailabilityRequestsByRotaID(ctx, defined.Rotation.ID)
	require.NoError(t, err)
	assert.Empty(t, remaining)
	// The link is dead, which is the answer a volunteer who kept it now gets.
	request, err := database.GetAvailabilityRequestByToken(ctx, requests[0].Token)
	require.NoError(t, err)
	assert.Nil(t, request)

	// And the next rota can be defined, because nothing is in flight any more.
	rec = doRequest(t, handler, http.MethodPost, "/api/rotations", `{"shiftCount":1}`, adminCookie())
	assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
}

// An allocated rota is never discarded, and the refusal is the database's own:
// the check runs inside the transaction holding the rota's row lock, so it
// cannot be overtaken by an allocation landing a moment later.
func TestDiscardRotaEndpointIntegration_RefusesAnAllocatedRota(t *testing.T) {
	database, _ := dbtest.New(t)
	dbtest.SeedRoles(t, database)
	dbtest.SeedDefaultShape(t, database)
	dbtest.SeedRotaDefaults(t, database)
	ctx := context.Background()
	handler := NewHandler(database, testVolunteers(), apiTestCfg, newTestAuthenticator(), nil, nil, zap.NewNop()).Routes()

	rec := doRequest(t, handler, http.MethodPost, "/api/rotations", `{"shiftCount":2}`, adminCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var defined defineRotaResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &defined))

	require.NoError(t, database.InsertAllocationsAndSetAllocated(ctx,
		[]db.Allocation{{ID: uuid.New().String(), ShiftID: defined.Shifts[0].ID, Role: "Service volunteer", VolunteerID: "alice"}},
		defined.Rotation.ID, time.Now()))

	rec = doRequest(t, handler, http.MethodDelete, "/api/rotations/"+defined.Rotation.ID, "", adminCookie())
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

	rotations, err := database.GetRotations(ctx)
	require.NoError(t, err)
	require.Len(t, rotations, 1, "the allocated rota is untouched")
	allocations, err := database.GetAllocationsByShiftIDs(ctx, []string{defined.Shifts[0].ID})
	require.NoError(t, err)
	assert.Len(t, allocations, 1)
}
