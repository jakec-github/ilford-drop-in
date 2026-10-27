import { useState } from "react";
import Button from "../ui/Button";
import { useDefineRota } from "../hooks/useDefineRota";
import OverridesCard from "./OverridesCard";
import RotaDefaultsCard from "./RotaDefaultsCard";
import { formatDay, formatLength, formatStart, endOf } from "./patternDates";
import type { OverrideReach, OverrideTouch, PlannedShift } from "../types";
import "./DefineRota.css";

// The count as typed, or null while it is not yet a whole number of shifts.
// The server still has the last word on the range; this only decides whether
// there is anything to ask it about.
function parseCount(value: string): number | null {
  if (!/^\d+$/.test(value.trim())) return null;
  const n = Number(value);
  return n >= 1 ? n : null;
}

// "November 2026", for grouping the plan by month.
function monthOf(timestamp: string): string {
  const [y, m] = timestamp.slice(0, 7).split("-").map(Number);
  return new Date(Date.UTC(y, m - 1, 1)).toLocaleDateString("en-GB", {
    month: "long",
    year: "numeric",
    timeZone: "UTC",
  });
}

function shiftLength(shift: PlannedShift): number {
  return (Date.parse(`${shift.end}Z`) - Date.parse(`${shift.start}Z`)) / 60000;
}

// What one Override did to one Shift, in a few words:
// "Override 2: starts 19:00, length 3 hours, shape, 1 pin".
function touchLabel(touch: OverrideTouch): string {
  const did: string[] = [];
  if (touch.startTime) did.push(`starts ${touch.startTime}`);
  if (touch.lengthMinutes > 0)
    did.push(`length ${formatLength(touch.lengthMinutes)}`);
  if (touch.shape) did.push("shape");
  if (touch.pins > 0) did.push(`${touch.pins} ${touch.pins === 1 ? "pin" : "pins"}`);
  return `Override ${touch.number}: ${did.join(", ") || "nothing to change"}`;
}

// One Shift as a row: the day, then its hours, with "(next day)" for one that
// runs past midnight. The end is the server's; it is only said here. A planned
// Shift is tagged with the Overrides that touched it, in the order they
// applied, so an Organiser can see which shifts their Overrides land on before
// defining. One an Override moved says where the pattern put it, and one that
// would run into another Shift is marked, beside the refusal naming the first.
function ShiftRow({ shift, note }: { shift: PlannedShift; note?: string }) {
  const end = endOf(shift.start, shiftLength(shift));
  const touches = shift.overrides ?? [];
  const className = [
    "define-rota-shift",
    note && "defined",
    shift.clash && "define-rota-shift--clash",
  ]
    .filter(Boolean)
    .join(" ");
  return (
    <li className={className}>
      <span className="define-rota-shift-day">
        {formatDay(shift.start.slice(0, 10))}
      </span>
      <span className="define-rota-shift-time">
        {shift.start.slice(11, 16)}–{end.time}
        {end.nextDay && <small> (next day)</small>}
      </span>
      {note && <span className="define-rota-shift-note">{note}</span>}
      {(shift.movedFrom || shift.clash || touches.length > 0) && (
        <span className="define-rota-shift-tags">
          {shift.clash && (
            <span className="define-rota-shift-tag define-rota-shift-tag--clash">
              clashes
            </span>
          )}
          {shift.movedFrom && (
            <span className="define-rota-shift-moved">
              moved from {shift.movedFrom.slice(11, 16)}
            </span>
          )}
          {touches.map((touch) => (
            <span key={touch.number} className="define-rota-shift-tag">
              {touchLabel(touch)}
            </span>
          ))}
        </span>
      )}
    </li>
  );
}

// An Override that matched none of the planned Shifts is worth a word: it may
// be aimed past this rota, or — for one naming a time — at a time the pattern
// no longer starts a shift at.
function OverrideWarnings({ reach }: { reach: OverrideReach[] }) {
  const missed = reach.filter((r) => r.matched === 0);
  if (missed.length === 0) return null;
  return (
    <ul className="define-rota-override-warnings">
      {missed.map((r) => (
        <li key={r.number}>
          Override {r.number} ({r.description}) matches no shift in this rota
          {r.missedByTime
            ? ". It falls on days this rota has shifts, but not at a time the pattern starts one — has the pattern's time changed?"
            : "."}
        </li>
      ))}
    </ul>
  );
}

// The Shifts the plan would mint, a heading per month: a rota is usually a
// season, and months are how an Organiser checks one against the calendar.
function PlannedShifts({ shifts }: { shifts: PlannedShift[] }) {
  const months: { label: string; shifts: PlannedShift[] }[] = [];
  for (const shift of shifts) {
    const label = monthOf(shift.start);
    const last = months[months.length - 1];
    if (last && last.label === label) last.shifts.push(shift);
    else months.push({ label, shifts: [shift] });
  }
  return (
    <>
      {months.map((month) => (
        <div key={month.label} className="define-rota-month">
          <h4>{month.label}</h4>
          <ol className="define-rota-shifts">
            {month.shifts.map((shift) => (
              <ShiftRow key={shift.start} shift={shift} />
            ))}
          </ol>
        </div>
      ))}
    </>
  );
}

// DefineRota is the Allocation tab with nothing in flight: the form that starts
// the next rota, and the settings that rota will be made from.
//
// A rota is the next however many Shifts the Shift pattern generates after the
// one the previous rota stopped after (ADR 0013). So the form asks for a count
// and nothing else — where it starts is not a choice but a consequence, and
// the screen says it rather than asking it. A break is the one exception, and
// it is behind "Leave a gap…" because it is the exception: a date no Shift may
// start before.
//
// Everything below the form is the server's plan, read again as the count
// changes, by the arithmetic defining uses — so the Shifts listed are the ones
// pressing the button mints, hours and all. The end of the previous rota is
// shown above them so the join can be checked by eye: an Organiser who expected
// the 19:30 after a 10:00 sees it there.
//
// The Rota Defaults and Overrides cards are the same ones the settings screen
// shows (issue #176). They are here because this is the moment those settings
// are spent, and a change to either re-plans the form above.
export default function DefineRota({
  onDefined,
}: {
  // Called after a define that may have worked. The tab re-reads what is in
  // flight, which is what swaps this screen for the working one.
  onDefined: () => void;
}) {
  // Empty: how long the next rota should run is a decision nobody has made
  // yet, and a number already in the box is one an Organiser can define a rota
  // without ever reading (issue #174).
  const [shiftCount, setShiftCount] = useState("");
  const [gap, setGap] = useState(false);
  const [notBefore, setNotBefore] = useState("");
  const [reload, setReload] = useState(0);

  const count = parseCount(shiftCount);
  const asked = gap ? notBefore : "";
  const { plan, planError, rota, error, defining, define } = useDefineRota(
    count,
    asked,
    reload,
  );

  const previous = plan?.previous ?? null;
  const noPattern = plan !== null && !plan.patternSet;
  const shifts = count !== null && plan ? plan.shifts : [];
  const refusal = count !== null && plan && !noPattern ? plan.refusal : "";
  const canDefine =
    count !== null && plan !== null && !refusal && !noPattern && !defining &&
    shifts.length === count;

  return (
    <>
      <section className="organiser-panel define-rota">
        <h2>Define the next rota</h2>

        {plan === null && !planError && (
          <p className="define-rota-intro">Working out the next rota…</p>
        )}

        {plan !== null && (
          <p className="define-rota-intro">
            {previous
              ? `The previous rota had ${previous.shiftCount} ${previous.shiftCount === 1 ? "shift" : "shifts"} and stopped after ${formatStart(previous.stoppedAfter)}. This one carries on from there.`
              : "This is the first rota. It starts from the Shift pattern's Start from."}
          </p>
        )}

        {/* noValidate deliberately: min below floors the spinner, but native
            validation would answer a bad count with a transient browser bubble
            in some cases and the server's message in others. */}
        <form
          className="define-rota-form"
          noValidate
          onSubmit={(e) => {
            e.preventDefault();
            if (count === null) return;
            const next = asked ? { shiftCount: count, notBefore: asked } : {
              shiftCount: count,
            };
            // Reloading whatever the outcome: a define that succeeded put a
            // rota in flight, and one that was refused most likely means one
            // already was.
            void define(next).then(onDefined);
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

            {gap ? (
              <>
                <label className="define-rota-field">
                  Not before
                  <input
                    type="date"
                    value={notBefore}
                    onChange={(e) => setNotBefore(e.target.value)}
                  />
                </label>
                <button
                  type="button"
                  className="define-rota-link"
                  onClick={() => {
                    setGap(false);
                    setNotBefore("");
                  }}
                >
                  No gap after all
                </button>
              </>
            ) : (
              <button
                type="button"
                className="define-rota-link"
                onClick={() => setGap(true)}
              >
                Leave a gap…
              </button>
            )}
          </div>

          {gap && (
            <p className="define-rota-note">
              No shift in this rota will start before this date. The pattern
              carries on as it would have, so shifts the gap skips are skipped
              rather than moved.
            </p>
          )}

          {/* aria-live so a screen reader hears the plan change as the count
              does: nothing moves focus to it. */}
          <div className="define-rota-plan" aria-live="polite">
            {planError && <p className="define-rota-error">{planError}</p>}

            {noPattern && (
              <p className="define-rota-warning">
                There is no shift pattern yet, so there are no shifts to define.
                Set one in the Rota Defaults below.
              </p>
            )}

            {!noPattern && count === null && plan !== null && (
              <p className="define-rota-note">
                Say how many shifts, and the ones it would take are listed here.
              </p>
            )}

            {refusal && <p className="define-rota-warning">{refusal}</p>}

            {shifts.length > 0 && (
              <>
                <p className="define-rota-headline">
                  {shifts.length} {shifts.length === 1 ? "shift" : "shifts"},
                  from {formatStart(shifts[0].start)} to{" "}
                  {formatStart(shifts[shifts.length - 1].start)}
                </p>
                {previous && previous.tail.length > 0 && (
                  <div className="define-rota-month">
                    <h4>End of the previous rota</h4>
                    <ol className="define-rota-shifts">
                      {previous.tail.map((shift) => (
                        <ShiftRow
                          key={shift.start}
                          shift={shift}
                          note="already defined"
                        />
                      ))}
                    </ol>
                  </div>
                )}
                <OverrideWarnings reach={plan?.overrides ?? []} />
                <PlannedShifts shifts={shifts} />
              </>
            )}
          </div>

          {/* Says the part of defining that is not on the form. Every active
              volunteer gets their link the moment the rota exists — and the
              reassurance that goes with it: nothing is emailed until you send
              the round, with a deadline in it. */}
          <p className="define-rota-note">
            Defining also gives every active volunteer their availability link.
            No email goes out until you send the round.
          </p>

          <div className="define-rota-actions">
            <Button type="submit" disabled={!canDefine}>
              {defining ? "Defining…" : "Define rota"}
            </Button>
          </div>
        </form>

        {/* aria-live so the outcome reaches a screen reader: submitting moves
            nothing into focus, so an unannounced result would go unnoticed. */}
        <div aria-live="polite">
          {error && (
            <p className="define-rota-error">Could not define: {error}</p>
          )}

          {/* Only until the tab re-reads and swaps this whole screen for the
              working one, which is the real confirmation. */}
          {rota && (
            <>
              <p className="define-rota-result">
                Defined {rota.shifts.length}{" "}
                {rota.shifts.length === 1 ? "shift" : "shifts"}:
              </p>
              <ol className="define-rota-dates">
                {rota.shifts.map((shift) => (
                  <li key={shift.start}>{formatStart(shift.start)}</li>
                ))}
              </ol>
            </>
          )}
        </div>
      </section>

      <RotaDefaultsCard onSaved={() => setReload((n) => n + 1)} />
      <OverridesCard onSaved={() => setReload((n) => n + 1)} />
    </>
  );
}
