import { useState } from "react";
import Button from "../ui/Button";
import { useShiftPatternPreview } from "../hooks/useShiftPatternPreview";
import PatternCalendar from "./PatternCalendar";
import {
  formatLength,
  isDate,
  monthsFrom,
  today as todayDate,
} from "./patternDates";
import type { ShiftPattern } from "../types";
import "./ShiftPatternEditor.css";

// The lengths offered, a quarter of an hour apart up to sixteen hours. The
// server takes anything from a minute to a day; these are the ones anybody
// runs, and a length saved outside them is offered too rather than lost.
const LENGTHS = Array.from({ length: 64 }, (_, i) => (i + 1) * 15);

// How many months the calendar shows at once.
const MONTHS_SHOWN = 3;

// ShiftPatternEditor is the Shift pattern section of the Rota Defaults, open
// for editing (ADR 0013): the RRULE lines, Start from, the zone, the length,
// and a calendar of what they generate.
//
// It is drawn in the card rather than in a dialog because the calendar is the
// point of it, and three months side by side do not fit a dialog's width.
//
// Every line is read back in plain English as it is typed, and every problem
// named against its line, by the server — the same evaluator that mints the
// rota, so the reading and the calendar are what defining will do.
export default function ShiftPatternEditor({
  saved,
  onSave,
  onClose,
}: {
  saved: ShiftPattern;
  onSave: (pattern: ShiftPattern) => Promise<void>;
  onClose: () => void;
}) {
  const [pattern, setPattern] = useState(saved.shiftPattern);
  const [startFrom, setStartFrom] = useState(saved.patternStartFrom);
  const [timezone, setTimezone] = useState(saved.shiftTimezone);
  const [length, setLength] = useState(saved.shiftLengthMinutes);
  const [offset, setOffset] = useState(0);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const today = todayDate();
  // The calendar opens on the month the pattern starts from — or this month,
  // until Start from is a date.
  const anchor = isDate(startFrom) ? startFrom : today;
  const months = monthsFrom(anchor, offset, MONTHS_SHOWN);
  const { preview, error: previewError } = useShiftPatternPreview(
    pattern,
    startFrom,
    timezone,
    months[0].first,
    months[months.length - 1].next,
  );

  const lengths = LENGTHS.includes(length) || length === 0
    ? LENGTHS
    : [...LENGTHS, length].sort((a, b) => a - b);
  const blank = pattern.trim() === "";
  const hasProblems = preview?.lines.some((l) => l.problems.length > 0);
  const changed =
    pattern !== saved.shiftPattern ||
    startFrom !== saved.patternStartFrom ||
    timezone.trim() !== saved.shiftTimezone ||
    length !== saved.shiftLengthMinutes;

  async function save() {
    setSaving(true);
    setError(null);
    try {
      await onSave({
        shiftPattern: pattern,
        patternStartFrom: startFrom,
        shiftTimezone: timezone.trim(),
        shiftLengthMinutes: length,
      });
      onClose();
    } catch (err: unknown) {
      // The server's own message names the line or the field that was wrong,
      // so it is shown as-is and the section stays open on what was typed.
      setError(
        err instanceof Error ? err.message : "Failed to save the shift pattern",
      );
      setSaving(false);
    }
  }

  return (
    <form
      className="shift-pattern-editor"
      aria-label="Shift pattern"
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
            {length === 0 && <option value={0}>Choose…</option>}
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
        month for a bare FREQ=MONTHLY. Nothing occurs before it; left blank, it
        is the day you save. Times are wall-clock in the timezone, so 19:30
        stays 19:30 when the clocks change.
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

      {error && <p className="settings-error">{error}</p>}

      <div className="settings-actions">
        <Button onClick={onClose} disabled={saving}>
          Cancel
        </Button>
        <Button
          type="submit"
          disabled={blank || length === 0 || !changed || saving}
        >
          {saving ? "Saving…" : "Save pattern"}
        </Button>
      </div>
    </form>
  );
}
