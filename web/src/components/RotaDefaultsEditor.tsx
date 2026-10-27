import { useState } from "react";
import Button from "../ui/Button";
import { useShiftPatternPreview } from "../hooks/useShiftPatternPreview";
import PatternCalendar from "./PatternCalendar";
import ShapeSeats from "./ShapeSeats";
import { seatCountsOf, seatsKey, seatsOf } from "./shape";
import {
  formatLength,
  lengthChoices,
  isDate,
  monthsFrom,
  today as todayDate,
} from "./patternDates";
import type { ConfiguredRole, RotaDefaults, ShiftPattern } from "../types";
import "./RotaDefaultsEditor.css";

// What the length starts at before one has been saved: the commonest session,
// and a value rather than a blank so a first pattern can be saved without a
// detour through the list.
const DEFAULT_LENGTH = 60;

// How many months the calendar shows at once.
const MONTHS_SHOWN = 3;

// RotaDefaultsEditor is the Rota Defaults open for editing: the Shift pattern
// (ADR 0013) — its RRULE lines, Start from, the zone, the length, and a
// calendar of what they generate — and the default Shape, under one Save.
//
// One editor rather than one each, because the two are one answer: what every
// Shift of the next rota is, when and with how many people. An Organiser
// setting up a deployment states both, and one changing the evening usually
// changes who it needs.
//
// It is drawn in the card rather than in a dialog because the calendar is the
// point of the pattern half, and three months side by side do not fit a
// dialog's width.
//
// The two halves are still two endpoints, so Save sends only the half that
// changed, the pattern first. A pattern the server refuses saves nothing; a
// Shape refused after the pattern was taken leaves the editor open on it, and
// the next Save sends the Shape alone.
export default function RotaDefaultsEditor({
  saved,
  roles,
  onSavePattern,
  onSaveShape,
  onClose,
}: {
  saved: RotaDefaults;
  roles: ConfiguredRole[];
  onSavePattern: (pattern: ShiftPattern) => Promise<void>;
  onSaveShape: (seats: { roleId: string; count: number }[]) => Promise<void>;
  onClose: () => void;
}) {
  const today = todayDate();
  // Start from and the length are filled in for a pattern never saved — today,
  // and an hour — so that only the lines are left to write. The server would
  // take a blank Start from as today too; saying so in the box is plainer.
  const [initial] = useState<ShiftPattern>(() => ({
    shiftPattern: saved.shiftPattern,
    patternStartFrom: saved.patternStartFrom || today,
    shiftTimezone: saved.shiftTimezone,
    shiftLengthMinutes: saved.shiftLengthMinutes || DEFAULT_LENGTH,
  }));
  const [pattern, setPattern] = useState(initial.shiftPattern);
  const [startFrom, setStartFrom] = useState(initial.patternStartFrom);
  const [timezone, setTimezone] = useState(initial.shiftTimezone);
  const [length, setLength] = useState(initial.shiftLengthMinutes);
  const [counts, setCounts] = useState(() =>
    seatCountsOf(roles, saved.defaultShape),
  );
  const [offset, setOffset] = useState(0);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // The calendar opens on the month the pattern starts from — or this month,
  // while Start from is not a date.
  const anchor = isDate(startFrom) ? startFrom : today;
  const months = monthsFrom(anchor, offset, MONTHS_SHOWN);
  const { preview, error: previewError } = useShiftPatternPreview(
    pattern,
    startFrom,
    timezone,
    months[0].first,
    months[months.length - 1].next,
  );

  const lengths = lengthChoices(length);
  const blank = pattern.trim() === "";
  const hasProblems = preview?.lines.some((l) => l.problems.length > 0);

  const next: ShiftPattern = {
    shiftPattern: pattern,
    patternStartFrom: startFrom,
    shiftTimezone: timezone.trim(),
    shiftLengthMinutes: length,
  };
  // Measured against the saved pattern, not against what the editor opened
  // on: Start from filled in with today is not a change worth saving while
  // there are no lines to go with it.
  const patternChanged =
    next.shiftPattern !== saved.shiftPattern ||
    (!blank &&
      (next.patternStartFrom !== saved.patternStartFrom ||
        next.shiftTimezone !== saved.shiftTimezone ||
        next.shiftLengthMinutes !== saved.shiftLengthMinutes));
  const seats = seatsOf(roles, counts);
  // Saving the Shape that is there would change nothing, and on the
  // Allocation tab would still mark the draft stale (issue #149).
  const shapeChanged = seatsKey(seats) !== seatsKey(saved.defaultShape);

  async function save() {
    setSaving(true);
    setError(null);
    try {
      if (patternChanged) await onSavePattern(next);
      if (shapeChanged) await onSaveShape(seats);
      onClose();
    } catch (err: unknown) {
      // The server's own message names the line or the field that was wrong,
      // so it is shown as-is and the editor stays open on what was typed.
      setError(
        err instanceof Error ? err.message : "Failed to save the Rota Defaults",
      );
      setSaving(false);
    }
  }

  return (
    <form
      className="rota-defaults-editor"
      aria-label="Rota Defaults"
      onSubmit={(e) => {
        e.preventDefault();
        void save();
      }}
    >
      <label className="settings-field">
        Shift pattern
        <textarea
          className="shift-pattern-text"
          value={pattern}
          rows={Math.max(3, pattern.split("\n").length + 1)}
          spellCheck={false}
          autoFocus
          placeholder="RRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30"
          onChange={(e) => setPattern(e.target.value)}
        />
      </label>
      <p className="settings-hint">
        One RRULE per line. Each line says when shifts start, with BYHOUR and
        BYMINUTE giving the time; two lines give two start times.
      </p>

      <ul className="shift-pattern-lines" aria-live="polite">
        {blank && <li className="problem">No lines yet.</li>}
        {!blank &&
          preview?.lines.map((line) =>
            line.problems.length > 0 ? (
              <li key={line.number} className="problem">
                <span>
                  Line {line.number}: <code>{line.text}</code>
                  <br />
                  {line.problems.join("; ")}
                </span>
              </li>
            ) : (
              <li key={line.number}>{line.description}</li>
            ),
          )}
      </ul>
      {previewError && <p className="settings-error">{previewError}</p>}

      <div className="shift-pattern-fields">
        <label className="settings-field">
          Start from
          <input
            type="date"
            value={startFrom}
            onChange={(e) => setStartFrom(e.target.value)}
          />
        </label>
        <label className="settings-field">
          Timezone
          <input
            type="text"
            value={timezone}
            onChange={(e) => setTimezone(e.target.value)}
            placeholder="Europe/London"
          />
        </label>
        <label className="settings-field">
          Shift length
          <select
            value={length}
            onChange={(e) => setLength(Number(e.target.value))}
          >
            {lengths.map((m) => (
              <option key={m} value={m}>
                {formatLength(m)}
              </option>
            ))}
          </select>
        </label>
      </div>
      <p className="settings-hint">
        Start from is where “every other week” counts from, and it fills in what
        a line leaves out: the weekday for a bare FREQ=WEEKLY, the day of the
        month for a bare FREQ=MONTHLY. Nothing occurs before it. Times are
        wall-clock in the timezone, so 19:30 stays 19:30 when the clocks
        change.
      </p>

      <h3 className="shift-pattern-heading">Calendar</h3>
      {blank || hasProblems ? (
        <p className="settings-empty">Fix the pattern to see its calendar.</p>
      ) : (
        <PatternCalendar
          months={months}
          starts={preview?.starts ?? []}
          startFrom={preview?.patternStartFrom ?? anchor}
          lengthMinutes={length}
          today={today}
          moved={offset !== 0}
          onMove={(by) => setOffset((o) => o + by)}
          onHome={() => setOffset(0)}
        />
      )}

      <h3 className="shift-pattern-heading">Shape</h3>
      {roles.length === 0 ? (
        <p className="settings-empty">
          No roles have been added yet, so there is nothing a shift can ask
          for. Add them on Settings first.
        </p>
      ) : (
        <>
          <p className="settings-hint">
            How many places of each Role a shift has. Every shift of a rota is
            minted with this; leave a Role at 0 if a shift does not need one,
            and a single shift can be changed on its own once the rota exists.
          </p>
          <div className="rota-defaults-shape">
            <ShapeSeats roles={roles} counts={counts} onChange={setCounts} />
          </div>
        </>
      )}

      {error && <p className="settings-error">{error}</p>}

      <div className="settings-actions">
        <Button onClick={onClose} disabled={saving}>
          Cancel
        </Button>
        <Button
          type="submit"
          disabled={
            (!patternChanged && !shapeChanged) ||
            (patternChanged && blank) ||
            saving
          }
        >
          {saving ? "Saving…" : "Save"}
        </Button>
      </div>
    </form>
  );
}
