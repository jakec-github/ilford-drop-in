package api

import "sync"

// draftSolves is this process's one solve slot for Draft Rota Allocations.
//
// A solve is a CP-SAT subprocess capped at thirty seconds, and the whole rota is
// solved entire every time, so two at once do the same work twice and race to
// replace each other's answer. One at a time is not a nicety here: the second
// caller has nothing to gain by waiting for its own solve, because the one
// already running is solving the same rota from the same inputs.
//
// In memory rather than in the database, like the availability sends beside it
// (sendjobs.go). A solve lost to a restart costs a re-solve, not correctness
// (ADR 0008) — and a lock outliving the process that held it would be worse than
// the duplicate work it prevents.
//
// A caller that cannot get in is not made to wait. It is told a solve is running
// and given the draft as it stands, which is what a screen watching the rota
// take shape wants: something to show now, and a reason to ask again.
type draftSolves struct {
	mu      sync.Mutex
	running bool
}

func newDraftSolves() *draftSolves {
	return &draftSolves{}
}

// begin claims the slot, reporting whether it got it. A caller that did must
// call end.
func (d *draftSolves) begin() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.running {
		return false
	}
	d.running = true
	return true
}

// end gives the slot back.
func (d *draftSolves) end() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.running = false
}
