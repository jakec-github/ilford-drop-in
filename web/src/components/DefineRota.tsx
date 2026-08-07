import { useState } from "react";
import { Link } from "wouter";
import Button from "../ui/Button";
import { useDefineRota } from "../hooks/useDefineRota";
import { useRoles } from "../hooks/useRoles";
import ShapeForm from "./ShapeForm";
import { describeShape } from "./shape";
import type {
  ConfiguredRole,
  NewRota,
  RotaProposal,
  RotaShift,
  ShapeSeat,
} from "../types";
import "./DefineRota.css";

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
        rota in flight, and this screen becomes the place it is prepared, asked
        about and allocated.
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

// Where the rota that is already out lives. The Allocation tab has nothing to
// say about an allocated rota — it is the rota now, and the rota page is what
// shows one — so this is a pointer rather than a copy of it.
//
// The date is the last shift still to come, not the rota's end: what an admin
// wants from this line is "how long have I got", and a rota that has run out is
// exactly the case where the pointer matters most and has no date to give.
function LastRotaPointer({ lastShiftDate }: { lastShiftDate: string | null }) {
  return (
    <p className="define-rota-last">
      {lastShiftDate === null ? (
        <>
          There are no shifts left on the rota. Whatever was allocated last is on
          the <Link href="/">rota page</Link>.
        </>
      ) : (
        <>
          The rota already allocated runs to {formatShiftDate(lastShiftDate)} —
          see it on the <Link href="/">rota page</Link>.
        </>
      )}
    </p>
  );
}

// DefineRota is the Allocation tab with nothing in flight: the form that starts
// the next rota, and a pointer to the one already out.
//
// There is no third thing here. Everything else the tab does — preparing the
// shifts, the round, the draft, allocating — is about a rota that exists, and
// none of it can be done to one that does not.
export default function DefineRota({
  shifts,
  onDefined,
}: {
  // The rota as the rota page shows it, or null while it is still loading. Used
  // only to name the last shift still to come; a rota being defined here has
  // none of its own yet.
  shifts: RotaShift[] | null;
  // Called after a define that may have worked. The tab re-reads what is in
  // flight, which is what swaps this screen for the working one.
  onDefined: () => void;
}) {
  const { proposal, rota, error, defining, define } = useDefineRota();
  const { roles } = useRoles();

  const lastShiftDate =
    shifts !== null && shifts.length > 0
      ? shifts[shifts.length - 1].date
      : null;

  return (
    <>
      <section className="admin-panel define-rota">
        <h2>Define the next rota</h2>

        {proposal === null && !error && (
          <p className="define-rota-intro">Working out the next rota…</p>
        )}

        {proposal !== null && (
          <DefineRotaForm
            proposal={proposal}
            roles={roles}
            defining={defining}
            onDefine={(next) => {
              // Reloading whatever the outcome: a define that succeeded put a
              // rota in flight, and one that was refused most likely means one
              // already was.
              void define(next).then(onDefined);
            }}
          />
        )}

        {/* aria-live so the outcome reaches a screen reader: submitting moves
            nothing into focus, so an unannounced result would go unnoticed. */}
        <div aria-live="polite">
          {error && (
            <p className="define-rota-error">Could not define: {error}</p>
          )}

          {/* Only until the tab re-reads and swaps this whole screen for the
              working one, which is the real confirmation. Worth showing in the
              meantime: the dates are the one thing the form did not state
              outright, and a shift landing on a day nobody expected is easiest
              to see here. */}
          {rota && (
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

      <section className="admin-panel">
        <h2>The rota that is out</h2>
        <LastRotaPointer lastShiftDate={lastShiftDate} />
      </section>
    </>
  );
}
