import { useMemo, useState } from "react";
import type { AllocationAttempt } from "../hooks/useDraftRotaAllocation";
import { usePreallocations } from "../hooks/usePreallocations";
import { useRoles } from "../hooks/useRoles";
import { useVolunteers } from "../hooks/useVolunteers";
import type {
  Assignee,
  DraftRotaState,
  PersonRef,
  Preallocation,
  Role,
  RotaShift,
} from "../types";
import { TEAM_LEAD_ROLE } from "../types";
import Button from "../ui/Button";
import Dialog from "../ui/Dialog";
import { compareDrafts } from "./draftChanges";
import {
  ClosureDialog,
  PinDialog,
  ShiftTimesDialog,
  UnpinDialog,
} from "./RotaEditDialogs";
import ShapeForm from "./ShapeForm";
import type { RowEdit } from "./ShiftList";
import ShiftList from "./ShiftList";
import { formatShiftDateLong } from "./shifts";
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
// One paragraph: the seat count, and what allocating does to everybody else —
// the rota reaches the page volunteers read and the calendars they have
// subscribed to, which is what makes it hard to undo.
//
// What it no longer says in advance is that the rota may not be this one.
// Allocating re-solves and commits only what it can still reproduce, but that
// is a branch the admin is shown when it happens (see ChangeReport), and
// explaining it up front cost two paragraphs on the screen every allocation
// passes through to warn about the case most of them never hit.
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

// What this panel can open. Every one of them acts on a single shift, which is
// why they are one union rather than a flag each: only one is ever up.
type PrepDialog =
  | { kind: "pin"; shift: RotaShift }
  | { kind: "unpin"; pin: Preallocation }
  | { kind: "closure"; shift: RotaShift }
  | { kind: "times"; shift: RotaShift }
  | { kind: "shape"; shift: RotaShift };

// DraftRotaPanel is the rota in flight: what the solver has made of it so far,
// heading the shifts it made it from.
//
// One panel rather than two (issue #180). The draft's report and the shifts it
// describes were a panel each, and reading one meant looking away from the
// other — while everything the head says is arithmetic over the rows below it,
// and every control on those rows changes what the head says. The head is what
// the draft staffed, when it solved, and the two things that can be done to
// it; the body is the rota, drawn by the same component the rota page draws it
// with.
//
// Nothing here is gated on anything else (issue #145): the Shape, the pins and
// the hours all move while the answers are coming in, and the draft re-solves
// around them. Nor is anything gated on an editing toggle, as the rota page
// gates its own: preparing the rota in flight is the only thing this tab is
// for.
//
// Admin-only, like everything else about a draft, and mounted on the Allocation
// tab alone — which is also the only screen a draft appears on at all. The rota
// page draws the same rows for the shifts of the rota in flight, and an admin
// can pin, close and shape them there, but it shows no drafted names: the rota
// is what has been decided, and a draft is a guess the next solve may replace.
export default function DraftRotaPanel({
  state,
  loadError,
  stale,
  solving,
  solveError,
  onSolve,
  allocating,
  allocateError,
  attempt,
  onAllocate,
  shifts,
  onInputMoved,
  onSetClosed,
  onSetTimes,
  onSetShape,
}: {
  // The draft as it was last read, or null while that read is in flight or has
  // failed. The panel stays on the page either way (issue #193): a draft read
  // can take as long as a solve, and taking the panel away while it runs takes
  // "Regenerate draft" — the one control that recovers a read that failed —
  // away with it.
  state: DraftRotaState | null;
  // Why there is no draft to show, where that is the reason. Reported inside
  // the panel rather than in place of it, for the same reason.
  loadError: string | null;
  // True while a fresh solve is owed to an edit made here. The drafted names
  // fade and Allocate goes away: what is on screen is a rota that predates
  // something the admin has just changed, and committing it is exactly what
  // ADR 0008 exists to stop.
  stale: boolean;
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
  // The rota in flight's shifts in date order, or null while they are loading.
  shifts: RotaShift[] | null;
  // Says that something the solver reads has moved. The pins are this panel's
  // own, and the Shapes, hours and closures belong to the caller's rota, so a
  // single signal upward is what lets one draft state answer for all four.
  onInputMoved: () => void;
  onSetClosed: (shiftId: string, closed: boolean) => Promise<void>;
  onSetTimes: (shiftId: string, start: string, end: string) => Promise<void>;
  onSetShape: (
    shiftId: string,
    seats: { roleId: string; count: number }[],
  ) => Promise<void>;
}) {
  const [confirming, setConfirming] = useState(false);
  const [dialog, setDialog] = useState<PrepDialog | null>(null);
  const [saving, setSaving] = useState(false);
  const [changeError, setChangeError] = useState<{
    shiftId: string;
    message: string;
  } | null>(null);

  const { roles, colourOf, idOf } = useRoles();
  const { volunteers, error: volunteersError } = useVolunteers();
  const {
    preallocations,
    error: preallocationsError,
    addPin,
    removePin,
  } = usePreallocations();

  const pinsByDate = useMemo(() => {
    const byDate = new Map<string, Preallocation[]>();
    for (const pin of preallocations ?? []) {
      const forDate = byDate.get(pin.date);
      if (forDate) forDate.push(pin);
      else byDate.set(pin.date, [pin]);
    }
    return byDate;
  }, [preallocations]);

  const draftByShiftID = useMemo(() => {
    const byShift = new Map<string, Assignee[]>();
    for (const shift of state?.shifts ?? []) {
      byShift.set(shift.shiftId, shift.assignees);
    }
    return byShift;
  }, [state]);

  // A draft names shifts by id and nothing else (ADR 0001), so naming one in a
  // sentence — which is what a refused allocation's change report does — means
  // looking it up in the rota beside it.
  const dateByShiftID = useMemo(() => {
    const byID = new Map<string, string>();
    for (const shift of shifts ?? []) {
      byID.set(shift.id, formatShiftDateLong(shift.date));
    }
    return byID;
  }, [shifts]);

  // Nothing to allocate until there is a rota to allocate. A draft that has not
  // been read has none, an unsolved one has none, and an infeasible one is the
  // solver saying there is none to be had. A stale one has a rota, but not the
  // one the inputs now imply — and allocating it would be refused by the server
  // anyway, on the hash it was shown (ADR 0008), so the button is unavailable
  // rather than left to fail.
  const allocatable = state !== null && state.solved && state.success && !stale;
  const busy = solving || allocating;

  // Who can still be pinned to a shift: the active roster, less anyone already
  // pinned there. Both halves matter — the server refuses a pin for an inactive
  // volunteer, and a repeat of one that already exists.
  function pinnableTo(date: string) {
    if (volunteers === null) return null;
    const pinned = new Set(
      (pinsByDate.get(date) ?? []).map((p) => p.volunteerId).filter(Boolean),
    );
    return volunteers.filter((v) => v.active && !pinned.has(v.id));
  }

  // Fires one change against one shift and leaves the screen showing whatever
  // the server now says. A refusal is not a failure of the app: the message
  // explains what the change contradicts, so it is shown against the row it was
  // refused for and nothing is rolled back — nothing was applied.
  //
  // Every change routed through here is an allocator input, so the draft above
  // is reported stale on the way out. Reported once the change has landed
  // rather than when it was fired: a re-read that overtook its own write would
  // come back solved against the inputs as they were.
  async function run(
    shiftId: string,
    apply: () => Promise<void>,
    fallback: string,
  ) {
    setSaving(true);
    try {
      await apply();
      setChangeError(null);
    } catch (err) {
      setChangeError({
        shiftId,
        message: err instanceof Error ? err.message : fallback,
      });
    } finally {
      setSaving(false);
      setDialog(null);
      onInputMoved();
    }
  }

  function submitPin(shift: RotaShift, person: PersonRef, role: Role) {
    // The picker names a Role the way the roster spells it; a pin references
    // one by id. A name nothing answers to is a pin that cannot be made, and
    // saying so beats sending a reference the server would refuse.
    const roleId = idOf(role);
    return run(
      shift.id,
      () =>
        roleId === null
          ? Promise.reject(new Error(`There is no role called ${role}`))
          : addPin({ date: shift.date, person, roleId }),
      "The pin was not saved",
    );
  }

  function submitUnpin(pin: Preallocation) {
    // Only ever called for a pin the listing gave an id, which is all of them.
    if (pin.id === null) return;
    const id = pin.id;
    const shift = (shifts ?? []).find((s) => s.date === pin.date);
    return run(shift?.id ?? "", () => removePin(id), "The pin was not removed");
  }

  function open(next: PrepDialog) {
    setChangeError(null);
    setDialog(next);
  }

  function rowEdit(shift: RotaShift): RowEdit {
    return {
      error: changeError?.shiftId === shift.id ? changeError.message : null,
      onPin: () => open({ kind: "pin", shift }),
      onUnpin: (pin) => open({ kind: "unpin", pin }),
      canSetClosed: !shift.allocated,
      onSetClosed: () => open({ kind: "closure", shift }),
      onEditTimes: () => open({ kind: "times", shift }),
      canEditShape: roles !== null,
      onEditShape: () => open({ kind: "shape", shift }),
      // Nothing on this tab has been allocated, so there is nobody on a shift
      // to move to another one. Moving people about is the rota page's job,
      // after allocation, one alteration at a time.
      placement: null,
    };
  }

  return (
    <section className="admin-panel draft-panel">
      <div className="draft-panel-head">
        <h2 className="draft-panel-title">Draft rota</h2>
        <div className="draft-panel-controls">
          {stale && (
            <span className="draft-panel-updating" role="status">
              <span className="draft-panel-spinner" aria-hidden="true" />
              Updating…
            </span>
          )}
          {/* "Regenerate" rather than "Refresh": reading this tab already
              brings the draft up to date, so the only reason to press this is
              the change no stamp can see — a volunteer added to the roster
              Sheet, or a Role given to one — and the button has to say "do it
              anyway" rather than "catch up". */}
          <Button size="small" onClick={onSolve} disabled={busy}>
            {solving ? "Solving…" : "Regenerate draft"}
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

      {/* One sentence, whatever state the draft is in — including "we do not
          know yet". A read of a draft whose inputs have moved solves before it
          answers (ADR 0008), so this is a wait that can run to half a minute,
          and it says so rather than leaving the panel looking empty. */}
      {state === null ? (
        <p className="draft-panel-state">
          {loadError
            ? "The draft could not be read."
            : "Reading the draft rota…"}
        </p>
      ) : state.solved && state.solvedAt !== null ? (
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

      {/* Verbatim, under the sentence that says there is no draft: it is the
          answer to why, and it names the thing to fix. */}
      {loadError && (
        <p className="draft-panel-error" role="alert">
          Could not load the draft rota: {loadError}
        </p>
      )}

      {state !== null && attempt?.outcome === "moved" && (
        <ChangeReport
          attempt={attempt}
          state={state}
          dateOf={(shiftId) => dateByShiftID.get(shiftId) ?? "That shift"}
        />
      )}

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

      {/* A failed pin load leaves the rows looking empty when they may not be,
          so it is said out loud rather than swallowed. The rest still reads. */}
      {preallocationsError && (
        <p className="draft-panel-error" role="alert">
          Could not load who is pinned: {preallocationsError}
        </p>
      )}

      {shifts === null ? (
        <p className="draft-panel-loading">Loading the shifts…</p>
      ) : shifts.length === 0 ? (
        <p className="draft-panel-loading">This rota has no shifts.</p>
      ) : (
        <ShiftList
          shifts={shifts}
          pinsByDate={pinsByDate}
          draftByShiftID={draftByShiftID}
          // An infeasible solve is not an answer about any one shift — it is
          // the solver saying there is no rota to be had — and the sentence
          // above says so. Marking all six rows unfilled underneath it would be
          // the same news, six times, in a place it cannot be acted on.
          draftSolved={state !== null && state.solved && state.success}
          draftStale={stale}
          colourOf={colourOf}
          rowEdit={rowEdit}
        />
      )}

      {confirming && state !== null && (
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

      {dialog?.kind === "pin" && (
        <PinDialog
          dateLabel={formatShiftDateLong(dialog.shift.date)}
          volunteers={pinnableTo(dialog.shift.date)}
          volunteersError={volunteersError}
          // A shift has one team-lead Seat, so a lead already pinned there
          // rules out a second — and it can be given up from here, whichever
          // way it came to be made.
          leadPinned={(pinsByDate.get(dialog.shift.date) ?? []).some(
            (p) => p.role === TEAM_LEAD_ROLE,
          )}
          pinnedNames={(pinsByDate.get(dialog.shift.date) ?? []).map(
            (p) => p.name,
          )}
          busy={saving}
          onCancel={() => setDialog(null)}
          onConfirm={(person, role) =>
            void submitPin(dialog.shift, person, role)
          }
        />
      )}

      {dialog?.kind === "unpin" && (
        <UnpinDialog
          name={dialog.pin.name}
          dateLabel={formatShiftDateLong(dialog.pin.date)}
          busy={saving}
          onCancel={() => setDialog(null)}
          onConfirm={() => void submitUnpin(dialog.pin)}
        />
      )}

      {dialog?.kind === "closure" && (
        <ClosureDialog
          dateLabel={formatShiftDateLong(dialog.shift.date)}
          closing={!dialog.shift.closed}
          pinnedCount={(pinsByDate.get(dialog.shift.date) ?? []).length}
          busy={saving}
          onCancel={() => setDialog(null)}
          onConfirm={() =>
            void run(
              dialog.shift.id,
              () => onSetClosed(dialog.shift.id, !dialog.shift.closed),
              dialog.shift.closed
                ? "The shift was not reopened"
                : "The shift was not closed",
            )
          }
        />
      )}

      {dialog?.kind === "times" && (
        <ShiftTimesDialog
          dateLabel={formatShiftDateLong(dialog.shift.date)}
          start={dialog.shift.start}
          end={dialog.shift.end}
          busy={saving}
          onCancel={() => setDialog(null)}
          onConfirm={(start, end) =>
            void run(
              dialog.shift.id,
              () => onSetTimes(dialog.shift.id, start, end),
              "The shift times were not saved",
            )
          }
        />
      )}

      {/* Its own errors rather than the row's: a refusal here names the Role
          whose ceiling was hit or the person pinned to a Seat that would go, and
          the form stays open on what was typed so the number can be corrected
          rather than retyped. */}
      {dialog?.kind === "shape" && roles && (
        <ShapeForm
          title={`What does ${formatShiftDateLong(dialog.shift.date)} ask for?`}
          intro="How many places of each Role this shift has. It starts from the default shape and can differ from every other shift; leave a Role at 0 if this one does not need it."
          saveLabel="Save shape"
          roles={roles}
          shape={dialog.shift.shape}
          onSave={(seats) =>
            onSetShape(dialog.shift.id, seats).finally(onInputMoved)
          }
          onClose={() => setDialog(null)}
        />
      )}
    </section>
  );
}
