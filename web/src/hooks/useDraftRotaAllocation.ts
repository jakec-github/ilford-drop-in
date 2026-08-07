import { useCallback, useEffect, useState } from "react";
import { fetchDraftRotaAllocation, solveDraftRotaAllocation } from "../api";
import type { DraftRotaState } from "../types";

interface UseDraftRotaAllocation {
  // null while the first load is still in flight. Loaded, it always answers:
  // a null rota inside it is "nothing in flight", which is a state rather than
  // an absence.
  state: DraftRotaState | null;
  error: string | null;
  // True while a solve is running. It is a CP-SAT subprocess capped at thirty
  // seconds, so this is a spinner the admin waits out rather than a flicker.
  solving: boolean;
  // The message from a solve that was refused, kept apart from `error` above:
  // that one means the draft could not be read, this one means the rota cannot
  // be solved yet and names the step that is missing.
  solveError: string | null;
  // Re-solves the rota in flight and re-reads the draft it wrote. Never
  // rejects — the outcome is in solveError, because the control that starts it
  // is also where the answer belongs.
  solve: () => Promise<void>;
}

interface UseDraftRotaAllocationOptions {
  // A draft is admin-only, so a view that shows it conditionally must be able
  // to say "not yet": fetching it for a logged-out visitor would be a
  // guaranteed 401 rendered as a load failure. Defaults to true.
  enabled?: boolean;
}

// useDraftRotaAllocation owns the rota in flight's Draft Rota Allocation: the
// read behind the dashed chips on the rota page, and the re-solve that replaces
// it.
//
// They belong together because a solve's only observable result is the next
// read: the endpoint that solves reports what it concluded, but the rota it
// drafted is stored, and stored is where the page takes it from. One shape for
// "what the draft says", whether it arrived from a page load or a re-solve.
export function useDraftRotaAllocation({
  enabled = true,
}: UseDraftRotaAllocationOptions = {}): UseDraftRotaAllocation {
  const [loaded, setLoaded] = useState<DraftRotaState | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [solving, setSolving] = useState(false);
  const [solveFailure, setSolveFailure] = useState<string | null>(null);
  const [reloads, setReloads] = useState(0);

  // Written with .then rather than await so no setState is reached
  // synchronously from the effect.
  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    void fetchDraftRotaAllocation()
      .then((draft) => {
        if (cancelled) return;
        setLoaded(draft);
        setLoadError(null);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setLoadError(
          err instanceof Error ? err.message : "Failed to load the draft rota",
        );
      });
    return () => {
      cancelled = true;
    };
  }, [enabled, reloads]);

  // Re-reading whatever the solve said. A refusal leaves the previous draft in
  // place and is the case that most needs the re-read: the usual reason a solve
  // is turned down is that the rota was allocated or discarded while this page
  // was open, and the fresh read is what makes the message make sense.
  const solve = useCallback(async () => {
    setSolving(true);
    setSolveFailure(null);
    try {
      await solveDraftRotaAllocation();
    } catch (err: unknown) {
      setSolveFailure(
        err instanceof Error ? err.message : "Failed to solve the draft rota",
      );
    } finally {
      setSolving(false);
      setReloads((n) => n + 1);
    }
  }, []);

  // Everything read out of here is gated on `enabled` rather than merely
  // stopping being refreshed by it, so disabling takes the draft away in the
  // same render. A session can end while the page is open — a logout in this
  // tab or another — and the draft is the one thing on this page a non-admin
  // must never see (ADR 0008). Returning what was last loaded until some later
  // reload cleared it would leave a logged-out reader looking at the rota the
  // solver drafted.
  return {
    state: enabled ? loaded : null,
    error: enabled ? loadError : null,
    solving: enabled && solving,
    solveError: enabled ? solveFailure : null,
    solve,
  };
}
