package api

import "context"

// draftSolves is this process's one solve slot for Draft Rota Allocations.
//
// A solve is a CP-SAT subprocess, and the whole rota is solved entire every
// time, so two at once do the same work twice and race to replace each other's
// answer. Everything that solves takes this slot: reading a draft whose inputs
// have moved, re-solving one on request, and allocating the rota.
//
// Callers queue rather than being turned away (issue #179). Being told "a solve
// is running" left every caller holding a retry policy of its own, and the
// policies did not agree: a read could come back stale, a re-solve could be
// refused outright, and each screen decided for itself what to do about it.
// Waiting makes reading a draft one thing — ask, wait, get an answer that speaks
// for the inputs as they stand. The cost is that a slow solve and a hung server
// look alike to a client, which is accepted: the simple model is the one that
// stays maintainable.
//
// In memory rather than in the database, like the availability sends beside it
// (sendjobs.go). A solve lost to a restart costs a re-solve, not correctness
// (ADR 0008) — and a lock outliving the process that held it would be worse than
// the duplicate work it prevents.
//
// The slot is per-process, and that is a constraint on the deployment rather
// than an implementation detail: it holds for one app container and no more, and
// deploy/compose.yaml runs exactly one. A second instance would mean a second
// slot, so two concurrent solves racing to store their answers — wasted CPU and
// a flapping draft, though not a wrong allocation, since the hash guard still
// means only a rota that was shown can be committed (ADR 0008). If the
// deployment ever grows past one container this becomes a Postgres advisory lock
// keyed on the rota, and the change stays inside this file.
type draftSolves struct {
	// A channel of capacity one rather than a mutex, because waiting has to be
	// selectable against the caller's context: a client that disconnects
	// mid-queue must leave it rather than hold a place for a solve nobody will
	// read.
	slot chan struct{}
}

func newDraftSolves() *draftSolves {
	return &draftSolves{slot: make(chan struct{}, 1)}
}

// acquire waits for the slot and takes it, or gives up when ctx is done and
// returns its error — so a caller can tell "the client left" from "the slot is
// mine". A caller that got it must release it.
//
// A waiter leaving disturbs nothing. The running solve holds the slot, not its
// waiters: it finishes and stores its answer whether or not anybody is still
// there to read it, and the next in the queue gets the slot when it does.
func (d *draftSolves) acquire(ctx context.Context) error {
	select {
	case d.slot <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// release hands the slot to whoever is next in the queue.
func (d *draftSolves) release() {
	<-d.slot
}
