import { useEffect, useState } from "react";
import {
  useDraft,
  useSaveWhenSettled,
  type SaveState,
} from "../hooks/useAutosave";
import { useRoles } from "../hooks/useRoles";
import {
  patternSet,
  shapeSet,
  useRotaDefaults,
} from "../hooks/useRotaDefaults";
import { useShiftPatternPreview } from "../hooks/useShiftPatternPreview";
import PatternCalendar from "./PatternCalendar";
import SaveStatus from "./SaveStatus";
import SettingsSection from "./SettingsSection";
import ShapeSeats from "./ShapeSeats";
import { seatCountsOf, seatsKey, seatsOf, type SeatCounts } from "./shape";
import {
  formatLength,
  isDate,
  lengthChoices,
  monthsFrom,
  today as todayDate,
} from "./patternDates";
import type { ConfiguredRole, RotaDefaults, ShiftPattern } from "../types";
import "./RotaDefaultsCard.css";

// What the length starts at before one has been saved: the commonest session,
// and a value rather than a blank so a first pattern can be saved without a
// detour through the list.
const DEFAULT_LENGTH = 60;

// How many months the calendar shows at once.
const MONTHS_SHOWN = 3;

// The pattern half as the card holds it. Start from and the length are filled
// in for a pattern never saved — today, and an hour — so that only the lines
// are left to write; the server would take a blank Start from as today too.
function patternOf(defaults: RotaDefaults): ShiftPattern {
  return {
    shiftPattern: defaults.shiftPattern,
    patternStartFrom: defaults.patternStartFrom || todayDate(),
    shiftTimezone: defaults.shiftTimezone,
    shiftLengthMinutes: defaults.shiftLengthMinutes || DEFAULT_LENGTH,
  };
}

const samePattern = (a: ShiftPattern, b: ShiftPattern) =>
  a.shiftPattern === b.shiftPattern &&
  a.patternStartFrom === b.patternStartFrom &&
  a.shiftTimezone.trim() === b.shiftTimezone.trim() &&
  a.shiftLengthMinutes === b.shiftLengthMinutes;

// Both halves save on their own; the card says the busier of the two.
function either(a: SaveState, b: SaveState): SaveState {
  if (a === "saving" || b === "saving") return "saving";
  if (a === "saved" || b === "saved") return "saved";
  return "idle";
}

// ShapeHalf is the default Shape, saved whenever its counts stop moving. Its
// own component so its draft can be read from the Roles, which it cannot be
// until they have loaded.
function ShapeHalf({
  defaults,
  roles,
  onSave,
  onState,
}: {
  defaults: RotaDefaults;
  roles: ConfiguredRole[];
  onSave: (seats: { roleId: string; count: number }[]) => Promise<void>;
  onState: (state: SaveState) => void;
}) {
  const sameCounts = (a: SeatCounts, b: SeatCounts) =>
    seatsKey(seatsOf(roles, a)) === seatsKey(seatsOf(roles, b));
  const { draft, setDraft, saved } = useDraft({
    source: defaults.defaultShape,
    toDraft: (shape) => seatCountsOf(roles, shape),
    same: sameCounts,
  });
  const { saveState, error } = useSaveWhenSettled({
    draft,
    saved,
    same: sameCounts,
    save: (counts) => onSave(seatsOf(roles, counts)),
  });
  // Said up to the card, which shows one status for both halves.
  useEffect(() => onState(saveState), [saveState, onState]);

  return (
    <>
      <p className="settings-hint">
        How many places of each Role a shift has. Every shift of a rota is
        minted with this; leave a Role at 0 if a shift does not need one, and a
        single shift can be changed on its own once the rota exists.
      </p>
      <div className="rota-defaults-shape">
        <ShapeSeats roles={roles} counts={draft} onChange={setDraft} />
      </div>
      {error && <p className="settings-error">{error}</p>}
    </>
  );
}

// RotaDefaultsCard is the Rota: what every rota is made from — when the
// Organisation runs (the Shift pattern, ADR 0013) and what a shift asks for
// (the default Shape) — edited where it stands. Each half saves itself once it
// stops moving: the pattern only once the server has read every line of it
// without a problem, so a half-typed line is never stored.
//
// It is the one settings card read on two screens. On Config it sits with the
// rest; on the Allocation tab it sits under the define form, because those two
// facts are what defining a rota spends (issue #176). One component rather
// than a read-only copy: an Organiser who finds the pattern wrong at the moment
// of defining should fix it there.
//
// onSaved is for the screen around it: the define screen previews the next
// rota from these settings, and re-reads it when one of them changes.
export default function RotaDefaultsCard({
  onSaved,
}: {
  onSaved?: () => void;
} = {}) {
  const rotaDefaults = useRotaDefaults();
  const { defaults, error } = rotaDefaults;
  const { roles } = useRoles();

  return (
    <SettingsSection
      title="Rota"
      blurb="What every rota is made from: when the organisation runs, and how many people each shift asks for."
      incomplete={
        defaults !== null && (!patternSet(defaults) || !shapeSet(defaults))
      }
    >
      {error && (
        <p className="settings-error">Could not load the settings: {error}</p>
      )}
      {defaults === null && !error && (
        <p className="settings-empty">Loading…</p>
      )}
      {defaults !== null && (
        <RotaDefaultsForm
          defaults={defaults}
          roles={roles}
          onSavePattern={async (pattern) => {
            await rotaDefaults.saveShiftPattern(pattern);
            onSaved?.();
          }}
          onSaveShape={async (seats) => {
            await rotaDefaults.saveShape(seats);
            onSaved?.();
          }}
        />
      )}
    </SettingsSection>
  );
}

function RotaDefaultsForm({
  defaults,
  roles,
  onSavePattern,
  onSaveShape,
}: {
  defaults: RotaDefaults;
  roles: ConfiguredRole[] | null;
  onSavePattern: (pattern: ShiftPattern) => Promise<void>;
  onSaveShape: (seats: { roleId: string; count: number }[]) => Promise<void>;
}) {
  const today = todayDate();
  const { draft, setDraft, saved } = useDraft({
    source: defaults,
    toDraft: patternOf,
    same: samePattern,
  });
  const [offset, setOffset] = useState(0);
  const [shapeState, setShapeState] = useState<SaveState>("idle");

  // The calendar opens on the month the pattern starts from — or this month,
  // while Start from is not a date.
  const anchor = isDate(draft.patternStartFrom)
    ? draft.patternStartFrom
    : today;
  const months = monthsFrom(anchor, offset, MONTHS_SHOWN);
  const {
    preview,
    current,
    error: previewError,
  } = useShiftPatternPreview(
    draft.shiftPattern,
    draft.patternStartFrom,
    draft.shiftTimezone,
    months[0].first,
    months[months.length - 1].next,
  );
  const blank = draft.shiftPattern.trim() === "";
  const hasProblems = preview?.lines.some((l) => l.problems.length > 0);

  const { saveState, error } = useSaveWhenSettled({
    draft,
    saved,
    same: samePattern,
    save: (pattern) =>
      onSavePattern({
        ...pattern,
        shiftTimezone: pattern.shiftTimezone.trim(),
      }),
    ready: !blank && current && !hasProblems,
  });

  const change = (part: Partial<ShiftPattern>) =>
    setDraft((d) => ({ ...d, ...part }));

  return (
    <div className="rota-defaults-card">
      <div className="rota-defaults-status">
        <SaveStatus state={either(saveState, shapeState)} />
      </div>
      <label className="settings-field">
        Shift pattern
        <textarea
          className="shift-pattern-text"
          value={draft.shiftPattern}
          rows={Math.max(3, draft.shiftPattern.split("\n").length + 1)}
          spellCheck={false}
          placeholder="RRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30"
          onChange={(e) => change({ shiftPattern: e.target.value })}
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
            value={draft.patternStartFrom}
            onChange={(e) => change({ patternStartFrom: e.target.value })}
          />
        </label>
        <label className="settings-field">
          Timezone
          <input
            type="text"
            value={draft.shiftTimezone}
            onChange={(e) => change({ shiftTimezone: e.target.value })}
            placeholder="Europe/London"
          />
        </label>
        <label className="settings-field">
          Shift length
          <select
            value={draft.shiftLengthMinutes}
            onChange={(e) =>
              change({ shiftLengthMinutes: Number(e.target.value) })
            }
          >
            {lengthChoices(draft.shiftLengthMinutes).map((m) => (
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
        wall-clock in the timezone, so 19:30 stays 19:30 when the clocks change.
      </p>
      {error && <p className="settings-error">{error}</p>}

      <h3 className="shift-pattern-heading">Calendar</h3>
      {blank || hasProblems ? (
        <p className="settings-empty">Write the pattern to see its calendar.</p>
      ) : (
        <PatternCalendar
          months={months}
          starts={preview?.starts ?? []}
          startFrom={preview?.patternStartFrom ?? anchor}
          lengthMinutes={draft.shiftLengthMinutes}
          today={today}
          moved={offset !== 0}
          onMove={(by) => setOffset((o) => o + by)}
          onHome={() => setOffset(0)}
        />
      )}

      <h3 className="shift-pattern-heading">Shape</h3>
      {roles === null ? (
        <p className="settings-empty">Loading…</p>
      ) : roles.length === 0 ? (
        // A Shape is counts of Roles, so there is nothing to ask for until
        // one exists, and no control is offered.
        <p className="settings-error">
          Define roles first: a shift's shape says how many of each Role it asks
          for. Add them under Roles on Config.
        </p>
      ) : (
        <ShapeHalf
          defaults={defaults}
          roles={roles}
          onSave={onSaveShape}
          onState={setShapeState}
        />
      )}
    </div>
  );
}
