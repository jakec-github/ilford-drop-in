package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/db/dbtest"
)

// TestDefineRotaEndpointIntegration drives POST /rotations against a real
// Postgres, proving the rotation and shift rows actually land — the mock store
// can only show that the handler asked. It then reads the shifts back through
// GET /shifts, which is the state an agent seeding a dev stack depends on
// (issue #75).
func TestDefineRotaEndpointIntegration(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	handler := NewHandler(database, testVolunteers(), apiTestCfg, newTestAuthenticator(), nil, zap.NewNop()).Routes()

	rec := doRequest(t, handler, http.MethodPost, "/rotations", `{"shiftCount":3}`, adminCookie())
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

	// A second call defines the following rota rather than replacing this one.
	rec = doRequest(t, handler, http.MethodPost, "/rotations", `{"shiftCount":1}`, adminCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var second defineRotaResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &second))
	assert.Greater(t, second.Rotation.Start, resp.Rotation.End)

	// GET /shifts now serves the minted shifts, unallocated and with nobody on
	// them: the state #31 needs to request availability against.
	rec = doRequest(t, handler, http.MethodGet, "/shifts?from="+resp.Rotation.Start, "")
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
	assert.False(t, listed.Shifts[0].Allocated)
	assert.Empty(t, listed.Shifts[0].Assignees)
}
