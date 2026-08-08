import { useMemo, useState } from "react";
import { Link } from "wouter";
import Button from "../ui/Button";
import Dialog from "../ui/Dialog";
import { useDraftRotaAllocation } from "../hooks/useDraftRotaAllocation";
import { useRota } from "../hooks/useRota";
import { useRotaInFlight } from "../hooks/useRotaInFlight";
import AvailabilityPanel from "./AvailabilityPanel";
import DefineRota from "./DefineRota";
import DraftRotaPanel from "./DraftRotaPanel";
import PrepShifts from "./PrepShifts";
import type { Assignee, RotaInFlight, RotaShift } from "../types";
import "./AdminAllocation.css";

// "2 Aug – 6 Sep 2026", for naming a rota in a sentence.
function formatSpan(rota: RotaInFlight): string {
  const short = (date: string) =>
    new Date(date).toLocaleDateString("en-GB", {
      day: "numeric",
      month: "short",
    });
  const year = new Date(rota.end).getFullYear();
  return `${short(rota.start)} – ${short(rota.end)} ${year}`;
}

// "Sun 2 Aug" — how a shift is named in a sentence, which on this screen is the
// change report under a refused allocation.
function formatShiftDateLong(dateStr: string): string {
  return new Date(dateStr).toLocaleDateString("en-GB", {
    weekday: "short",
    day: "numeric",
    month: "short",
  });
}

// How far the round has got, in a sentence an admin can act on. Kept apart from
// the discard warning below: this one is about what still needs doing, that one
// is about what would be lost.
function describeRound(rota: RotaInFlight): string {
  if (rota.asked === 0) return "Nobody has been asked about it yet.";
  // Answers are reported even where nothing has been sent. Minting and sending
  // are separate, and a link handed over by hand is answered without this app
  // ever emailing it — so "none sent" is never a reason to leave out how many
  // people have replied.
  const links =
    rota.sent === 0
      ? `${rota.asked} volunteers have a link, none of them sent yet`
      : `Links sent to ${rota.sent} of ${rota.asked} volunteers`;
  return `${links} · ${rota.replied} answered.`;
}

// The confirmation a discard is behind.
//
// It leads with the answers because those are the thing that cannot be
// remade — shifts and pins are a minute's work to re-enter, and a volunteer's
// answer is somebody else's time. The most likely reason to be here at all is
// realising the rota is wrong *because* somebody replied to say so, which is
// exactly when that number is largest.
function DiscardDialog({
  rota,
  discarding,
  error,
  onDiscard,
  onClose,
}: {
  rota: RotaInFlight;
  discarding: boolean;
  error: string | null;
  onDiscard: () => void;
  onClose: () => void;
}) {
  return (
    <Dialog title="Discard this rota?" onClose={onClose}>
      <p className="discard-lead">
        The rota running {formatSpan(rota)} will be deleted, along with its{" "}
        {rota.shiftCount} {rota.shiftCount === 1 ? "shift" : "shifts"}, what
        each of them asks for, and every pin on them.
      </p>

      {rota.replied > 0 ? (
        <p className="discard-loss">
          {rota.replied}{" "}
          {rota.replied === 1 ? "volunteer has" : "volunteers have"} already
          answered. {rota.replied === 1 ? "That answer" : "Those answers"} will
          be destroyed, and{" "}
          {rota.asked === 1 ? "their link" : "everyone's links"} will stop
          working.
        </p>
      ) : (
        rota.asked > 0 && (
          <p className="discard-loss">
            Nobody has answered yet, but the {rota.asked} links already handed
            out will stop working.
          </p>
        )
      )}

      <p className="discard-note">
        None of it can be recovered. Defining the next rota starts from a blank
        form.
      </p>

      {error && <p className="allocation-error">{error}</p>}

      <div className="discard-actions">
        <Button onClick={onClose} disabled={discarding}>
          Cancel
        </Button>
        <Button
          className="discard-button"
          onClick={onDiscard}
          disabled={discarding}
        >
          {discarding ? "Discarding…" : "Discard rota"}
        </Button>
      </div>
    </Dialog>
  );
}

// The rota in flight, named, with the one action that ends it short of
// allocating.
//
// Discard sits up here rather than beside Allocate at the bottom. They are the
// two ways this rota's life ends, but they are not a pair of buttons to choose
// between: one is where the screen is heading, the other is the way out of a
// rota that should never have been defined. Putting them together would offer
// them as alternatives.
function InFlightHead({
  rota,
  discard,
  onDiscarded,
}: {
  rota: RotaInFlight;
  discard: (id: string) => Promise<void>;
  onDiscarded: () => void;
}) {
  const [confirming, setConfirming] = useState(false);
  const [discarding, setDiscarding] = useState(false);
  const [error, setError] = useState<string | null>(null);

  return (
    <section className="admin-panel allocation-head">
      <div className="allocation-head-row">
        <div>
          <h2>The rota in flight</h2>
          <p className="in-flight-span">
            {formatSpan(rota)} · {rota.shiftCount}{" "}
            {rota.shiftCount === 1 ? "shift" : "shifts"}
          </p>
          <p className="in-flight-round">{describeRound(rota)}</p>
        </div>

        <Button
          className="discard-button"
          size="small"
          onClick={() => {
            setError(null);
            setConfirming(true);
          }}
        >
          Discard rota
        </Button>
      </div>

      <p className="in-flight-note">
        Everything below is about this rota, and none of it is a step to be
        finished before the next: the shifts keep being edited while the answers
        come in, and the draft is re-solved around whatever has moved. One rota
        is in flight at a time, so the next cannot be defined until this one is
        allocated — or discarded, which deletes it and everything on it.
      </p>

      {confirming && (
        <DiscardDialog
          rota={rota}
          discarding={discarding}
          error={error}
          onClose={() => setConfirming(false)}
          onDiscard={() => {
            setDiscarding(true);
            discard(rota.id)
              .then(() => {
                setConfirming(false);
                onDiscarded();
              })
              .catch((err: unknown) => {
                setError(
                  err instanceof Error
                    ? err.message
                    : "Failed to discard the rota",
                );
              })
              .finally(() => {
                setDiscarding(false);
              });
          }}
        />
      )}
    </section>
  );
}

// The working screen: everything about the rota in flight, all at once.
//
// Not a wizard, and the order down the page is not an order to do things in
// (issue #145). It reads as what the rota is, what the solver makes of it, and
// who has answered — with the two terminal actions where each belongs.
function InFlightRota({
  rota,
  shifts,
  setClosed,
  setTimes,
  setShape,
  onDiscarded,
  onAllocated,
  discard,
}: {
  rota: RotaInFlight;
  // The whole rota as the rota page reads it, or null while it is loading.
  shifts: RotaShift[] | null;
  setClosed: (shiftId: string, closed: boolean) => Promise<void>;
  setTimes: (shiftId: string, start: string, end: string) => Promise<void>;
  setShape: (
    shiftId: string,
    seats: { roleId: string; count: number }[],
  ) => Promise<void>;
  onDiscarded: () => void;
  onAllocated: () => void;
  discard: (id: string) => Promise<void>;
}) {
  const {
    state: draftState,
    error: draftError,
    solving,
    solveError,
    solve,
    allocating,
    allocateError,
    attempt,
    allocate,
  } = useDraftRotaAllocation();

  // The shifts of the rota in flight are exactly the unallocated ones: only one
  // rota is unallocated at a time, and a shift's flag is its rota's. Closed ones
  // are among them — a shut date is still this rota's to reopen.
  const inFlightShifts = useMemo(
    () => (shifts ?? []).filter((s) => !s.allocated),
    [shifts],
  );

  const dateByShiftID = useMemo(() => {
    const byID = new Map<string, string>();
    for (const shift of inFlightShifts) {
      byID.set(shift.id, formatShiftDateLong(shift.date));
    }
    return byID;
  }, [inFlightShifts]);

  const draftByShiftID = useMemo(() => {
    const byShift = new Map<string, Assignee[]>();
    for (const shift of draftState?.shifts ?? []) {
      byShift.set(shift.shiftId, shift.assignees);
    }
    return byShift;
  }, [draftState]);

  return (
    <>
      <InFlightHead rota={rota} discard={discard} onDiscarded={onDiscarded} />

      {/* Above the shifts it is drawn onto, as on the rota page: what a refused
          allocation reports is the difference between the rota that was
          confirmed and the one in the table below. */}
      {draftError && (
        <p className="allocation-error" role="alert">
          Could not load the draft rota: {draftError}
        </p>
      )}

      {draftState && (
        <DraftRotaPanel
          state={draftState}
          solving={solving}
          solveError={solveError}
          onSolve={() => void solve()}
          allocating={allocating}
          allocateError={allocateError}
          attempt={attempt}
          onAllocate={() =>
            allocate().then((outcome) => {
              if (!outcome?.allocated) return;
              // The rota has gone out. Nothing on this screen is about it any
              // more — there is no rota in flight, no draft and no round — so
              // the tab re-reads and swaps itself for the define screen.
              onAllocated();
            })
          }
          dateOf={(shiftId) => dateByShiftID.get(shiftId) ?? "That shift"}
        />
      )}

      {shifts === null ? (
        <section className="admin-panel">
          <h2>Shifts</h2>
          <p className="allocation-loading">Loading the shifts…</p>
        </section>
      ) : (
        <PrepShifts
          shifts={inFlightShifts}
          draftByShiftID={draftByShiftID}
          // Each of these re-reads the rota on its way through, so the table
          // redraws from what the server now says rather than from what was
          // asked for.
          onSetClosed={setClosed}
          onSetTimes={setTimes}
          onSetShape={setShape}
        />
      )}

      {/* Minting a round and sending the links is the same rota's business as
          everything above, which is why it is a section here rather than a tab
          of its own. It reads the latest rota's round, and the latest rota is
          the one in flight — defining a second is refused while this exists. */}
      <AvailabilityPanel />
    </>
  );
}

// AdminAllocation is the Allocation tab, and it has two states because the rota
// does. One rota is in flight at a time, so either there is one — and this is
// everything about it, on one screen — or there is not, and this is the form
// that defines the next.
//
// There is no third state. An allocated rota is the rota, and the rota page is
// what shows one; all this has to say about it then is where to find it.
export default function AdminAllocation() {
  const { inFlight, loading, error, reload, discard } = useRotaInFlight();
  const {
    shifts,
    error: rotaError,
    reload: reloadShifts,
    setClosed,
    setTimes,
    setShape,
  } = useRota();
  // Set when an allocation this screen ran succeeded, and never cleared. What
  // replaces this screen is the define form for the next rota, which says
  // nothing about the one that just went out — so the sentence that does has to
  // outlive the state change that prompted it.
  const [allocated, setAllocated] = useState(false);

  // Both reads change together at every point this screen has an action for:
  // defining mints the shifts, discarding destroys them, allocating turns them
  // into the rota.
  const reloadBoth = () => {
    void reload();
    void reloadShifts();
  };

  return (
    <>
      {error && (
        <p className="allocation-error" role="alert">
          Could not load the rota: {error}
        </p>
      )}

      {rotaError && (
        <p className="allocation-error" role="alert">
          Could not load the shifts: {rotaError}
        </p>
      )}

      {allocated && (
        <p className="allocation-allocated" role="status">
          The rota is allocated. Volunteers can see it on the{" "}
          <Link href="/">rota page</Link> and in the calendar feeds they
          subscribe to. Changing it from here on is one alteration at a time, on
          that page.
        </p>
      )}

      {loading && <p className="allocation-loading">Loading the rota…</p>}

      {!loading && inFlight !== null && (
        <InFlightRota
          rota={inFlight}
          shifts={shifts}
          setClosed={setClosed}
          setTimes={setTimes}
          setShape={setShape}
          discard={discard}
          onDiscarded={reloadBoth}
          onAllocated={() => {
            setAllocated(true);
            reloadBoth();
          }}
        />
      )}

      {!loading && inFlight === null && <DefineRota onDefined={reloadBoth} />}
    </>
  );
}
