import { useCallback, useEffect, useRef, useState } from "react";
import {
  allocateRotaInFlight,
  fetchDraftRotaAllocation,
  solveDraftRotaAllocation,
} from "../api";
import type { AllocateOutcome, DraftRotaState, DraftShift } from "../types";

// AllocationAttempt is what came of the last attempt to allocate, kept so the
// screen can say something about it once the request is over.
//
// The refused case carries the rota that was shown, not the one that came
// back — that one is now in `state`, on screen. What an admin needs is the
// difference between the two, and this is the half of it the page would
// otherwise have overwritten.
export type AllocationAttempt =
  | { outcome: "allocated"; allocatedAt: string }
  | { outcome: "moved"; shown: DraftShift[] };

interface UseDraftRotaAllocation {
  // null while the first load is still in flight, and null when no rota is in
  // flight — the state between one rota going out and the next being defined.
  // The two are one thing to a caller: there is nothing to show either way, and
  // neither is a failure.
  state: DraftRotaState | null;
  error: string | null;
  // True while a solve is running. It is a CP-SAT subprocess capped at thirty
  // seconds, so this is a spinner the admin waits out rather than a flicker.
  solving: boolean;
  // The message from a solve that was refused, kept apart from `error` above:
  // that one means the draft could not be read, this one means the rota cannot
  // be solved yet and names the step that is missing.
  //
  // It covers both ways a solve is refused: one this hook asked for, and the
  // one the read itself attempts when a draft's inputs have moved. They are the
  // same refusal for the same reason, so they are one message.
  solveError: string | null;
  // Re-solves the rota in flight and re-reads the draft it wrote. Never
  // rejects — the outcome is in solveError, because the control that starts it
  // is also where the answer belongs.
  solve: () => Promise<void>;
  // True while an allocation is running. It re-solves before it commits, so it
  // is the same thirty seconds a solve takes, on the one action that cannot be
  // shown optimistically: what it does depends on what the solver says.
  allocating: boolean;
  // The message from an allocation that was refused outright — no rota in
  // flight, nothing drafted yet. A rota that had moved is not one of these:
  // that is an outcome, and it is in `attempt`.
  allocateError: string | null;
  // What came of the last allocation, or null if none has been attempted since
  // this page loaded — or since the last one was superseded by starting
  // another.
  attempt: AllocationAttempt | null;
  // Allocates the rota in flight, confirming the draft as it was last read.
  // Never rejects: what happened is in `attempt` and `allocateError`, and the
  // resolved outcome is there for a caller with something else to do about it —
  // the rota page reloads, because allocating is what puts the rota on it.
  allocate: () => Promise<AllocateOutcome | null>;
  // True from the moment an edit is reported until a read that accounts for it
  // comes back. `state` is still the last answer and still worth showing; this
  // says the solver has not had its say about what just changed, which is why
  // the screen fades the drafted names and takes Allocate away rather than
  // blanking anything.
  stale: boolean;
  // Says that something the solver reads has moved — a pin, a Shape, a closure,
  // a shift's hours. Marks the draft stale at once and re-reads on a trailing
  // debounce, so a burst of edits costs one solve rather than one each.
  //
  // Called once the edit has landed, not when it is fired: a read that overtook
  // its own write would come back solved against the inputs as they were, clear
  // `stale`, and leave the screen claiming to be current about a change it had
  // not seen.
  inputsMoved: () => void;
}

// How long after the last edit lands before the draft is re-read. Long enough
// that pinning three people in a row is one solve, short enough that an admin
// who made one change is not left watching a spinner they could have believed
// was stuck.
const RE_READ_DEBOUNCE_MS = 2000;

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
  const [allocating, setAllocating] = useState(false);
  const [allocateFailure, setAllocateFailure] = useState<string | null>(null);
  const [attempt, setAttempt] = useState<AllocationAttempt | null>(null);
  const [stale, setStale] = useState(false);

  // One request at a time. Every request here can end in a solve, and a solve
  // is a CP-SAT subprocess the server queues one at a time anyway — so a second
  // would only wait in line behind the first while looking to this side like
  // two things happening at once.
  const inFlight = useRef(false);
  // An edit that landed while a request was running. Remembered rather than
  // fired: an abort would kill the solve the running read is waiting on, and an
  // admin editing steadily would abort every read they started and never see a
  // draft at all. At most one — what is wanted afterwards is a read of the
  // inputs as they finally stand, however many times they moved.
  const readAgain = useRef(false);
  const debounce = useRef<ReturnType<typeof setTimeout>>(undefined);

  const read = useCallback(() => {
    if (inFlight.current) {
      readAgain.current = true;
      return;
    }
    setReloads((n) => n + 1);
  }, []);

  // Written with .then rather than await so no setState is reached
  // synchronously from the effect.
  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    inFlight.current = true;
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
      })
      .finally(() => {
        inFlight.current = false;
        if (cancelled) return;
        // A read that came back with an edit still unaccounted for has not
        // answered the question, so it goes round again and the draft stays
        // stale. Reading is what makes a draft current (ADR 0008), so there is
        // nothing else to ask.
        if (readAgain.current) {
          readAgain.current = false;
          setReloads((n) => n + 1);
        } else {
          setStale(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [enabled, reloads]);

  // Unmounting mid-debounce would otherwise re-read a draft nobody is looking
  // at, and set state on a component that has gone.
  useEffect(() => () => clearTimeout(debounce.current), []);

  const inputsMoved = useCallback(() => {
    setStale(true);
    clearTimeout(debounce.current);
    debounce.current = setTimeout(read, RE_READ_DEBOUNCE_MS);
  }, [read]);

  // Re-reading whatever the solve said. A refusal leaves the previous draft in
  // place and is the case that most needs the re-read: the usual reason a solve
  // is turned down is that the rota was allocated or discarded while this page
  // was open, and the fresh read is what makes the message make sense.
  const solve = useCallback(async () => {
    setSolving(true);
    setSolveFailure(null);
    inFlight.current = true;
    // Whatever the last allocation attempt found, a fresh solve has just
    // answered the same question again — so what it said about the rota moving
    // is about a rota two solves ago.
    setAttempt(null);
    try {
      await solveDraftRotaAllocation();
    } catch (err: unknown) {
      setSolveFailure(
        err instanceof Error ? err.message : "Failed to solve the draft rota",
      );
    } finally {
      setSolving(false);
      inFlight.current = false;
      // The re-read below is the one an edit during the solve was waiting for:
      // a read re-checks dirtiness for itself and solves again if the answer it
      // is about to return predates the edit (ADR 0008).
      readAgain.current = false;
      setReloads((n) => n + 1);
    }
  }, []);

  // Allocating confirms the draft this page last read, by the fingerprint it
  // came with. Nothing here decides whether it still holds — the server
  // re-solves and compares, which is the only way to know (ADR 0008).
  //
  // A refused allocation carries the rota as it now stands, so it is taken from
  // the response rather than re-read: the read would repeat the solve's roster
  // fetch to be told the same thing. A successful one does need the re-read,
  // and gets nothing back: the rota is allocated, so there is no draft any
  // more, and the read that says so is what takes the draft panel off screen.
  const allocate = useCallback(async (): Promise<AllocateOutcome | null> => {
    if (!loaded?.hash) {
      setAllocateFailure(
        "There is no draft to allocate. Solve one and read it first.",
      );
      return null;
    }

    setAllocating(true);
    setAllocateFailure(null);
    setAttempt(null);
    inFlight.current = true;
    try {
      const outcome = await allocateRotaInFlight(loaded.hash);
      if (outcome.allocated) {
        setAttempt({
          outcome: "allocated",
          allocatedAt: outcome.allocatedAt,
        });
        setReloads((n) => n + 1);
      } else {
        setAttempt({ outcome: "moved", shown: loaded.shifts });
        setLoaded(outcome.rota);
      }
      return outcome;
    } catch (err: unknown) {
      setAllocateFailure(
        err instanceof Error ? err.message : "Failed to allocate the rota",
      );
      // The usual reason an allocation is refused outright is that the rota is
      // no longer what this page thinks it is — allocated by somebody else, or
      // discarded — so the re-read is what makes the message make sense.
      setReloads((n) => n + 1);
      return null;
    } finally {
      setAllocating(false);
      inFlight.current = false;
      // An edit that landed while the allocation ran is not answered by what it
      // came back with: the refused case carries the solve the server had just
      // done, which predates the edit. Bumping twice in one batch is one read,
      // not two — React has not re-rendered in between.
      if (readAgain.current) {
        readAgain.current = false;
        setReloads((n) => n + 1);
      }
    }
  }, [loaded]);

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
    // The read's own refusal is the fallback rather than the override: a solve
    // this admin asked for is the more recent of the two, and it is the one
    // they are waiting on an answer to.
    solveError: enabled ? (solveFailure ?? loaded?.solveError ?? null) : null,
    solve,
    allocating: enabled && allocating,
    allocateError: enabled ? allocateFailure : null,
    attempt: enabled ? attempt : null,
    allocate,
    stale: enabled && stale,
    inputsMoved,
  };
}
