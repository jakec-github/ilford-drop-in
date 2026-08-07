package services

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// The hash is what a draft is confirmed by, so what it must ignore matters as
// much as what it reads. Seat ids are minted fresh by every solve — they say
// nothing about the rota — and the solver's assignment order is canonical but
// the store's read order is not, so neither may reach the fingerprint.
func TestHashAllocationsIgnoresIdsAndOrder(t *testing.T) {
	shown := []db.Allocation{
		{ID: "seat-1", ShiftID: "shift-1", Role: "Team lead", VolunteerID: "vol-1"},
		{ID: "seat-2", ShiftID: "shift-1", Role: "Service volunteer", VolunteerID: "vol-2"},
		{ID: "seat-3", ShiftID: "shift-2", Role: "Service volunteer", CustomEntry: "St John's team"},
	}
	resolved := []db.Allocation{
		{ID: "seat-9", ShiftID: "shift-2", Role: "Service volunteer", CustomEntry: "St John's team"},
		{ID: "seat-7", ShiftID: "shift-1", Role: "Team lead", VolunteerID: "vol-1"},
		{ID: "seat-8", ShiftID: "shift-1", Role: "Service volunteer", VolunteerID: "vol-2"},
	}

	assert.Equal(t, hashAllocations(shown), hashAllocations(resolved),
		"the same rota re-solved hashes the same however its Seats are ordered or identified")
}

// And what it must read: every part of a Seat that says who is working where,
// in what. Each of these is a different rota, and confirming one must never
// commit another.
func TestHashAllocationsSeparatesDifferentRotas(t *testing.T) {
	base := []db.Allocation{
		{ID: "seat-1", ShiftID: "shift-1", Role: "Team lead", VolunteerID: "vol-1"},
		{ID: "seat-2", ShiftID: "shift-1", Role: "Service volunteer", VolunteerID: "vol-2"},
	}

	for name, other := range map[string][]db.Allocation{
		"somebody else in a Seat": {
			{ID: "seat-1", ShiftID: "shift-1", Role: "Team lead", VolunteerID: "vol-1"},
			{ID: "seat-2", ShiftID: "shift-1", Role: "Service volunteer", VolunteerID: "vol-3"},
		},
		"the same people on another shift": {
			{ID: "seat-1", ShiftID: "shift-2", Role: "Team lead", VolunteerID: "vol-1"},
			{ID: "seat-2", ShiftID: "shift-2", Role: "Service volunteer", VolunteerID: "vol-2"},
		},
		"the same people in swapped Roles": {
			{ID: "seat-1", ShiftID: "shift-1", Role: "Service volunteer", VolunteerID: "vol-1"},
			{ID: "seat-2", ShiftID: "shift-1", Role: "Team lead", VolunteerID: "vol-2"},
		},
		"a Seat nobody is in": {
			{ID: "seat-1", ShiftID: "shift-1", Role: "Team lead", VolunteerID: "vol-1"},
		},
		"a custom entry rather than a volunteer": {
			{ID: "seat-1", ShiftID: "shift-1", Role: "Team lead", VolunteerID: "vol-1"},
			{ID: "seat-2", ShiftID: "shift-1", Role: "Service volunteer", CustomEntry: "vol-2"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.NotEqual(t, hashAllocations(base), hashAllocations(other))
		})
	}
}

// A rota that staffs nobody still has a fingerprint, and it is not the empty
// string: an infeasible solve is refused before anything is compared, but a
// hash that read as "no answer" would be one confirmation away from allocating
// nobody to everything.
func TestHashAllocationsOfAnEmptyRota(t *testing.T) {
	assert.NotEmpty(t, hashAllocations(nil))
	assert.Equal(t, hashAllocations(nil), hashAllocations([]db.Allocation{}))
	assert.NotEqual(t, hashAllocations(nil), hashAllocations([]db.Allocation{
		{ID: "seat-1", ShiftID: "shift-1", Role: "Team lead", VolunteerID: "vol-1"},
	}))
}
