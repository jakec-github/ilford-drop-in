package services

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// hashAllocations fingerprints a solved rota: every Seat, who is in it and on
// which Shift, and nothing else.
//
// This is what "allocate the rota you were shown" is enforced by (ADR 0008).
// Allocating re-solves, hashes the answer, and commits only if it matches the
// hash of the draft the admin was looking at. The solver is deterministic —
// fixed seed, one worker — so an identical hash means the inputs that could
// change the rota have not moved, and a different one means the admin is about
// to commit a rota they have not seen.
//
// The comparison is on the output rather than on the assembled input, which was
// considered first and is worse: an input hash trips on map ordering and on
// numeric formatting, and it fires on input changes that cannot change the
// rota — a volunteer's name, a Shift's hours — throwing confirmations that mean
// nothing.
//
// Two things are deliberately not in it. Seat ids, because every solve mints
// fresh ones and they say nothing about who is working: hashing them would make
// every confirmation fail. And order, because the solver's canonical order
// survives the solve but not the round trip through the store — the draft comes
// back in whatever order the rows are read in, and that must not read as a
// different rota.
func hashAllocations(seats []db.Allocation) string {
	lines := make([]string, 0, len(seats))
	for _, seat := range seats {
		// Tab-separated because none of these fields can contain one: ids and
		// Role names come from this app, and a custom entry is a name typed
		// into a text field. Without a separator that cannot appear in a field,
		// two different rotas could write the same line.
		lines = append(lines, strings.Join([]string{seat.ShiftID, seat.Role, seat.VolunteerID, seat.CustomEntry}, "\t"))
	}
	sort.Strings(lines)

	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}
