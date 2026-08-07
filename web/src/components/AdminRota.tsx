import { useState } from "react";
import Button from "../ui/Button";
import Dialog from "../ui/Dialog";
import { useDefineRota } from "../hooks/useDefineRota";
import { useRoles } from "../hooks/useRoles";
import { useRotaInFlight } from "../hooks/useRotaInFlight";
import ShapeForm from "./ShapeForm";
import { describeShape } from "./shape";
import type {
  ConfiguredRole,
  NewRota,
  RotaInFlight,
  RotaProposal,
  ShapeSeat,
} from "../types";
import "./AdminRota.css";

// How many shifts a rota has when nobody has said. The server has no opinion —
// no rota implies how long the next one should be — so the number lives here,
// where it is a starting point rather than a rule.
const DEFAULT_SHIFT_COUNT = "6";

// "Sun 2 Aug 2026" — the weekday is worth showing here, unlike on the rota
// itself: what an admin is checking is that the weeks they expected were taken.
function formatShiftDate(dateStr: string): string {
  return new Date(dateStr).toLocaleDateString("en-GB", {
    weekday: "short",
    day: "numeric",
    month: "short",
    year: "numeric",
  });
}

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

      {error && <p className="define-rota-error">{error}</p>}

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

// The rota being worked on, and the one action that ends it short of allocating.
function InFlightPanel({
  rota,
  onDiscarded,
  discard,
}: {
  rota: RotaInFlight;
  onDiscarded: () => void;
  discard: (id: string) => Promise<void>;
}) {
  const [confirming, setConfirming] = useState(false);
  const [discarding, setDiscarding] = useState(false);
  const [error, setError] = useState<string | null>(null);

  return (
    <>
      <p className="in-flight-span">
        {formatSpan(rota)} · {rota.shiftCount}{" "}
        {rota.shiftCount === 1 ? "shift" : "shifts"}
      </p>
      <p className="in-flight-round">{describeRound(rota)}</p>
      {/* Says why there is no form here, rather than leaving a disabled one to
          be puzzled over. */}
      <p className="in-flight-note">
        One rota is in flight at a time, so the next one cannot be defined until
        this is allocated — or discarded, which deletes it and everything on it.
      </p>

      <div className="in-flight-actions">
        <Button
          className="discard-button"
          onClick={() => {
            setError(null);
            setConfirming(true);
          }}
        >
          Discard rota
        </Button>
      </div>

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
    </>
  );
}

// DefineRotaForm is the define screen: the whole of the rota about to be made,
// on one form (issue #140).
//
// Every field starts from the proposal and every field can be changed. The
// start date is the one an admin is most likely to touch — a rota can begin
// after a break rather than the week after the last one — but the hours and the
// Shape are here too, so a rota that runs differently for a few weeks does not
// need the settings edited and put back.
//
// What is stated here applies to this rota alone. The Rota Defaults are what the
// form began from, not what it writes to, and the note under the form is the one
// place that is said.
//
// It is mounted on a loaded proposal and never sees a null one, which is what
// lets each field initialise from it directly: a re-read unmounts the form
// rather than trying to reconcile new answers with what somebody has typed.
function DefineRotaForm({
  proposal,
  roles,
  defining,
  onDefine,
}: {
  proposal: RotaProposal;
  // null while the Roles are still loading; empty on a deployment that has
  // stated none, where there is nothing a shift could ask for.
  roles: ConfiguredRole[] | null;
  defining: boolean;
  onDefine: (rota: NewRota) => void;
}) {
  const [shiftCount, setShiftCount] = useState(DEFAULT_SHIFT_COUNT);
  const [startDate, setStartDate] = useState(proposal.startDate);
  const [startTime, setStartTime] = useState(proposal.shiftStartTime);
  const [endTime, setEndTime] = useState(proposal.shiftEndTime);
  const [shape, setShape] = useState<ShapeSeat[]>(proposal.shape);
  const [editingShape, setEditingShape] = useState(false);

  return (
    <>
      <p className="define-rota-intro">
        One shift a week from the date below. Once the rota is defined it is the
        rota in flight, and the next cannot be defined until it is allocated.
      </p>

      {/* noValidate deliberately: min below floors the spinner, but native
          validation would answer a bad count with a transient browser bubble in
          some cases and the server's message in others. Submitting regardless
          means one field is only ever rejected in one place, with one
          wording. */}
      <form
        className="define-rota-form"
        noValidate
        onSubmit={(e) => {
          e.preventDefault();
          onDefine({
            shiftCount: Number(shiftCount),
            startDate,
            shiftStartTime: startTime,
            shiftEndTime: endTime,
            shape: shape.map((seat) => ({
              roleId: seat.roleId,
              count: seat.count,
            })),
          });
        }}
      >
        <div className="define-rota-fields">
          <label className="define-rota-field">
            Shifts
            <input
              className="define-rota-count"
              type="number"
              inputMode="numeric"
              min="1"
              value={shiftCount}
              onChange={(e) => setShiftCount(e.target.value)}
            />
          </label>

          <label className="define-rota-field">
            First shift
            <input
              type="date"
              value={startDate}
              onChange={(e) => setStartDate(e.target.value)}
            />
          </label>

          {/* Native time inputs read and write the same 24-hour "HH:MM" the
              server stores, so nothing here parses or formats a time. */}
          <label className="define-rota-field">
            Starts
            <input
              type="time"
              value={startTime}
              onChange={(e) => setStartTime(e.target.value)}
            />
          </label>

          <label className="define-rota-field">
            Ends
            <input
              type="time"
              value={endTime}
              onChange={(e) => setEndTime(e.target.value)}
            />
          </label>
        </div>

        {/* The Shape is a summary and a button rather than a row of counters,
            for the same reason it is on the settings screen: it is a handful of
            numbers an admin reads and rarely changes, and inlining them would
            bury the two fields that matter under a list of Roles. */}
        <div className="define-rota-shape">
          <span className="define-rota-shape-label">Every shift asks for</span>
          <span className="define-rota-shape-value">
            {shape.length > 0 ? (
              describeShape(shape)
            ) : (
              <span className="define-rota-unset">Nobody yet</span>
            )}
          </span>
          {/* Nothing to shape until Roles exist, and the note below says so —
              offering the button here would open a dialog with no rows in it. */}
          {roles !== null && roles.length > 0 && (
            <Button size="small" onClick={() => setEditingShape(true)}>
              Edit shape
            </Button>
          )}
        </div>

        <div className="define-rota-actions">
          <Button type="submit" disabled={defining}>
            {defining ? "Defining…" : "Define rota"}
          </Button>
        </div>
      </form>

      <p className="define-rota-note">
        {roles !== null && roles.length === 0
          ? "No roles have been added yet, so there is nothing a shift can ask for. Add them on Settings first."
          : "The times and the shape apply to this rota only — changing them here does not change the Rota Defaults on Settings. Each shift can be changed on its own once the rota exists."}
      </p>

      {editingShape && roles && (
        <ShapeForm
          title="What each shift asks for"
          intro="How many places of each Role every shift of this rota starts with. Leave a Role at 0 if these shifts do not need one; a single shift can be changed on its own once the rota exists."
          saveLabel="Use this shape"
          roles={roles}
          shape={shape}
          // Held rather than saved: this Shape is part of the rota being
          // defined, and nothing exists to write it to until the rota does.
          // ShapeForm cannot tell the difference, since all it ever does is hand
          // back the Seats and close.
          onSave={(seats) => {
            setShape(
              seats.map((seat) => ({
                roleId: seat.roleId,
                role: roles.find((r) => r.id === seat.roleId)?.name ?? "",
                count: seat.count,
              })),
            );
            return Promise.resolve();
          }}
          onClose={() => setEditingShape(false)}
        />
      )}
    </>
  );
}

// AdminRota is the rota tab, and it has two states because the rota does: one
// rota is in flight at a time, so either there is one — and this shows it, with
// the discard that ends it — or there is not, and this offers the form that
// defines the next.
//
// The form is not shown alongside the rota in flight and disabled. Defining is
// refused server-side while one exists, and a form that is only ever going to
// be rejected is worse than a sentence saying why there is no form.
export default function AdminRota() {
  const { proposal, reloadProposal, rota, error, defining, define } =
    useDefineRota();
  const { roles } = useRoles();
  const {
    inFlight,
    loading,
    error: inFlightError,
    reload,
    discard,
  } = useRotaInFlight();

  return (
    <section className="admin-panel define-rota">
      <h2>Rota</h2>

      {inFlightError && (
        <p className="define-rota-error">
          Could not load the rota: {inFlightError}
        </p>
      )}

      {loading && <p className="define-rota-intro">Loading the rota…</p>}

      {!loading && inFlight !== null && (
        <InFlightPanel
          rota={inFlight}
          discard={discard}
          // The discarded rota is the one the proposal counted forward from, so
          // the date it named is now too late by the length of that rota.
          onDiscarded={reloadProposal}
        />
      )}

      {!loading && inFlight === null && proposal === null && !error && (
        <p className="define-rota-intro">Working out the next rota…</p>
      )}

      {!loading && inFlight === null && proposal !== null && (
        <DefineRotaForm
          proposal={proposal}
          roles={roles}
          defining={defining}
          onDefine={(next) => {
            // Reloading whatever the outcome: a define that succeeded put a
            // rota in flight, and one that was refused most likely means one
            // already was.
            void define(next).then(reload);
          }}
        />
      )}

      {/* aria-live so the outcome reaches a screen reader: submitting moves
          nothing into focus, so an unannounced result would go unnoticed. */}
      <div aria-live="polite">
        {error && (
          <p className="define-rota-error">Could not define: {error}</p>
        )}

        {rota && inFlight?.id === rota.id && (
          <>
            <p className="define-rota-result">
              Defined {rota.shiftDates.length}{" "}
              {rota.shiftDates.length === 1 ? "shift" : "shifts"}:
            </p>
            <ol className="define-rota-dates">
              {rota.shiftDates.map((date) => (
                <li key={date}>{formatShiftDate(date)}</li>
              ))}
            </ol>
          </>
        )}
      </div>
    </section>
  );
}
