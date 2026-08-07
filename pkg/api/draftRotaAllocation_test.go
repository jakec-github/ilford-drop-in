package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
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
