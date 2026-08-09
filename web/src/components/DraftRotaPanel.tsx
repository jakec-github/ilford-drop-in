import { useState } from "react";
import type { AllocationAttempt } from "../hooks/useDraftRotaAllocation";
import type { DraftRotaState } from "../types";
import Button from "../ui/Button";
import Dialog from "../ui/Dialog";
import { compareDrafts } from "./draftChanges";
import "./DraftRotaPanel.css";

// How long ago the solve ran, which is the only thing anybody asks of a draft's
// timestamp: the inputs move under it all through the availability window, so
// "six hours ago" answers "is this worth trusting" where a clock time does not.
//
// Rounded down to the coarsest unit that still says something, and never
// negative — a clock a few seconds behind the server's must not produce "in 4
// seconds".
function timeAgo(iso: string): string {
  const solved = new Date(iso).getTime();
  if (Number.isNaN(solved)) return "at an unknown time";

  const minutes = Math.max(0, Math.floor((Date.now() - solved) / 60000));
  if (minutes < 1) return "just now";
  if (minutes < 60)
    return `${minutes} ${minutes === 1 ? "minute" : "minutes"} ago`;

  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} ${hours === 1 ? "hour" : "hours"} ago`;

  const days = Math.floor(hours / 24);
  return `${days} ${days === 1 ? "day" : "days"} ago`;
}

// What the solve concluded, in the terms an admin acts on.
//
// The two outcomes lead to different work, so they are worded as different
// sentences rather than as a status with a number beside it. Unfilled Seats are
// people to chase; an infeasible solve is a conflict in the inputs to go and
// resolve, and no amount of chasing fixes it.
function describeOutcome(state: DraftRotaState): string {
  if (!state.success) {
    return (
      "No rota is possible from the availability, pins and shapes as they stand " +
      `(the solver said ${state.solverStatus}).`
    );
  }
  const unfilled = state.seatsAsked - state.seatsFilled;
  if (unfilled <= 0) {
    return `Every seat is filled — all ${state.seatsAsked} of them.`;
  }
  return `${state.seatsFilled} of ${state.seatsAsked} seats filled, ${unfilled} still empty.`;
}

// The confirmation allocating is behind.
//
// It leads with what allocating does to everybody else, because that is what
// makes it hard to undo: the rota reaches the page volunteers read and the
// calendars they have subscribed to, and taking somebody off it afterwards is
// an alteration with a reason attached, not a re-solve.
//
// It says out loud that the rota may not be this one. Allocating re-solves and
// commits only what it can still reproduce, so an admin who has had this page
// open for an hour is as likely to be shown a changed rota as an allocated one,
// and being told that after the fact would read as a fault.
function AllocateDialog({
  state,
  allocating,
  onAllocate,
  onClose,
}: {
  state: DraftRotaState;
  allocating: boolean;
  onAllocate: () => void;
  onClose: () => void;
}) {
  const unfilled = state.seatsAsked - state.seatsFilled;

  return (
    <Dialog title="Allocate this rota?" onClose={onClose}>
      <p className="allocate-lead">
        The draft below becomes the rota: {state.seatsFilled} of{" "}
        {state.seatsAsked} seats filled
        {unfilled > 0 ? `, ${unfilled} left empty` : ""}. Volunteers will see it
        on the rota page and in the calendar feeds they subscribe to.
      </p>

      <p className="allocate-note">
        Changing it afterwards is an alteration — one person at a time, with a
        reason — rather than another solve. No more rotas can be defined until
        this one is allocated, and after that this one cannot be discarded.
      </p>

      <p className="allocate-note">
        Allocating solves the rota once more and commits it only if it comes out
        the same. If anything has moved since this page was loaded you will be
        shown what changed instead, and nothing will be allocated. That takes as
        long as a solve, so this may sit for half a minute.
      </p>

      <div className="allocate-actions">
        <Button onClick={onClose} disabled={allocating}>
          Cancel
        </Button>
        <Button
          className="allocate-button"
          onClick={onAllocate}
          disabled={allocating}
        >
          {allocating ? "Allocating…" : "Allocate rota"}
        </Button>
      </div>
    </Dialog>
  );
}

// How many differences are worth naming one by one. Past this the list stops
// being something anybody reads: one new pin can make CP-SAT re-balance the
// whole rota, and thirty bullet points saying so is a wall in front of the rota
// they describe.
const CHANGES_WORTH_LISTING = 10;

// What changed under an allocation that was refused: the difference between the
// rota the admin confirmed and the one the solver produced when they did.
//
// Named one by one while there are few, because that is when the names are the
// whole answer: "Ada is on the 9th now" is usually recognisable as the change
// the admin was waiting for, and enough on its own to allocate again. Counted
// when there are many, because a rota the solver has re-balanced end to end is
// one fact, not thirty — and the rota it re-balanced into is on the screen
// below.
function ChangeReport({
  attempt,
  state,
  dateOf,
}: {
  attempt: Extract<AllocationAttempt, { outcome: "moved" }>;
  state: DraftRotaState;
  dateOf: (shiftId: string) => string;
}) {
  const changes = compareDrafts(attempt.shown, state.shifts);
  const shiftsAffected = new Set(changes.map((change) => change.shiftId)).size;

  return (
    <div className="draft-panel-moved" role="alert">
      <p className="draft-panel-moved-lead">
        The rota changed while you were looking at it, so nothing was allocated.
        This is the rota as it now stands — read it and allocate again.
      </p>
      {changes.length > CHANGES_WORTH_LISTING && (
        <p className="draft-panel-moved-count">
          {changes.length} placements are different, across {shiftsAffected}{" "}
          {shiftsAffected === 1 ? "shift" : "shifts"}. One change to the inputs
          can make the solver re-balance the whole rota, so this is normal
          rather than a sign that something went wrong.
        </p>
      )}
      {changes.length > 0 && changes.length <= CHANGES_WORTH_LISTING && (
        <ul className="draft-panel-changes">
          {changes.map((change) => (
            <li key={`${change.shiftId}-${change.kind}-${change.name}`}>
              <span className="draft-panel-change-date">
                {dateOf(change.shiftId)}
              </span>{" "}
              {change.kind === "in" && (
                <>
                  {change.name} added as {change.role.toLowerCase()}
                </>
              )}
              {change.kind === "out" && <>{change.name} no longer on it</>}
              {change.kind === "role" && (
                <>
                  {change.name} now {change.role.toLowerCase()} rather than{" "}
                  {change.wasRole?.toLowerCase()}
                </>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

// DraftRotaPanel is what the draft has to say for itself, above the rota it is
// drawn onto: when it was last solved, how much of the rota it staffed, whether
// a rota was possible at all, and the two controls that act on it — solve it
// again, or allocate it.
//
// Allocating lives here, in the panel, because this is where the rota being
// allocated is on screen: the Allocation tab draws the draft into the shift
// table directly below (issue #145). The whole design rests on allocating the
// rota you were shown (ADR 0008), and a button somewhere else would be a button
// to allocate a rota you were not looking at.
//
// Admin-only, like everything else about a draft, and mounted on the Allocation
// tab alone. The public rota page shows the drafted names as dashed chips but
// never this: a panel about solving and committing is not something a volunteer
// reading the rota has any use for.
export default function DraftRotaPanel({
  state,
  solving,
  solveError,
  onSolve,
  allocating,
  allocateError,
  attempt,
  onAllocate,
  dateOf,
}: {
  state: DraftRotaState;
  solving: boolean;
  solveError: string | null;
  onSolve: () => void;
  allocating: boolean;
  allocateError: string | null;
  attempt: AllocationAttempt | null;
  // Runs the allocation. Resolves when it is over, whatever it found, which is
  // what closes the dialog: the outcome is reported in the panel, beside the
  // rota it happened to, and never rejects.
  onAllocate: () => Promise<void>;
  // A shift's date, for naming one in a sentence. The panel is given this
  // rather than shift dates of its own: the page it sits on already holds the
  // rota, and a draft is keyed by shift id precisely so the two cannot drift
  // apart (ADR 0001).
  dateOf: (shiftId: string) => string;
}) {
  const [confirming, setConfirming] = useState(false);

  // Nothing to allocate until there is a rota to allocate. An unsolved draft
  // has none, and an infeasible one is the solver saying there is none to be
  // had. Nothing here has to ask whether the draft is current: what was read is
  // what the inputs say, and if it has moved since, allocating refuses and
  // reports the difference (ADR 0008).
  const allocatable = state.solved && state.success;
  const busy = solving || allocating;

  return (
    <section className="draft-panel">
      <div className="draft-panel-head">
        <h2 className="draft-panel-title">Draft rota</h2>
        <div className="draft-panel-controls">
          <Button size="small" onClick={onSolve} disabled={busy}>
            {solving ? "Solving…" : state.solved ? "Solve again" : "Solve now"}
          </Button>
          <Button
            size="small"
            className="draft-panel-allocate"
            onClick={() => setConfirming(true)}
            disabled={!allocatable || busy}
          >
            {allocating ? "Allocating…" : "Allocate rota"}
          </Button>
        </div>
      </div>

      {state.solved && state.solvedAt !== null ? (
        <p className="draft-panel-state">
          {describeOutcome(state)}{" "}
          {/* The instant in the title, so the exact time is a hover away
              without putting a timestamp nobody reads in the sentence. */}
          <span className="draft-panel-when" title={state.solvedAt}>
            Solved {timeAgo(state.solvedAt)}.
          </span>
        </p>
      ) : (
        <p className="draft-panel-state">
          Nothing has been solved for this rota yet.
        </p>
      )}

      {attempt?.outcome === "moved" && (
        <ChangeReport attempt={attempt} state={state} dateOf={dateOf} />
      )}

      {/* Said on every state, because it is true on every state and it is the
          thing most easily forgotten: a draft is a guess, and it is a guess
          about inputs that keep moving. */}
      <p className="draft-panel-note">
        A draft is what the solver makes of the availability, pins and shapes as
        they stood when it ran &mdash; not the rota, until it is allocated. It
        is solved again whenever one of those moves, so opening this page is
        usually enough to bring it up to date. To hold somebody to a shift, pin
        them.
      </p>

      {/* A refused solve names the step that has not been taken — an
          availability round nobody has minted, settings nobody has filled in —
          so it is shown verbatim rather than summarised. */}
      {solveError && (
        <p className="draft-panel-error" role="alert">
          {solveError}
        </p>
      )}

      {/* And a refused allocation names what stopped it: an infeasible solve, a
          rota somebody else has already allocated, a solve running now. */}
      {allocateError && (
        <p className="draft-panel-error" role="alert">
          {allocateError}
        </p>
      )}

      {confirming && (
        <AllocateDialog
          state={state}
          allocating={allocating}
          onClose={() => setConfirming(false)}
          // Held open for the whole solve, rather than dismissed onto a
          // spinner behind it: it is the one place that says what is being
          // waited for, and closing it would invite a second click on a button
          // that is about to change what it means.
          onAllocate={() => {
            void onAllocate().finally(() => setConfirming(false));
          }}
        />
      )}
    </section>
  );
}
