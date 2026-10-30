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
import { monthsFrom, today as todayDate } from "./patternDates";
import type { ConfiguredRole, PatternEntry, RotaDefaults } from "../types";
import "./RotaDefaultsCard.css";

// How many months the calendar shows at once.
const MONTHS_SHOWN = 3;

// What the textarea offers before anything is written: one entry, the shape
// a calendar event is pasted in.
const PLACEHOLDER = `BEGIN:VEVENT
DTSTART:20261101T193000
DURATION:PT3H
RRULE:FREQ=WEEKLY;BYDAY=SU
END:VEVENT`;

const samePattern = (a: string, b: string) => a === b;

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
// Organisation runs (the Shift pattern, ADR 0014) and what a shift asks for
// (the default Shape) — edited where it stands. Each half saves itself once it
// stops moving: the pattern only once the server has read every entry of it
// without a problem, so a half-typed entry is never stored.
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
  collapsible = false,
}: {
  onSaved?: () => void;
  // Closed until opened, as on Config; the define form shows it open. It
  // stays open while the pattern or shape is unset.
  collapsible?: boolean;
} = {}) {
  const rotaDefaults = useRotaDefaults();
  const { defaults, error } = rotaDefaults;
  const { roles } = useRoles();
  const [saveState, setSaveState] = useState<SaveState>("idle");

  return (
    <SettingsSection
      title="Rota"
      collapsible={collapsible}
      blurb="What every rota is made from: when the organisation runs, and how many people each shift asks for."
      incomplete={
        defaults !== null && (!patternSet(defaults) || !shapeSet(defaults))
      }
      status={<SaveStatus state={saveState} />}
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
          onState={setSaveState}
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

// EntryReading is what the server made of one entry: what it means, or what
// is wrong with it, and anything in it that was read past.
function EntryReading({ entry }: { entry: PatternEntry }) {
  return (
    <>
      {entry.problems.length > 0 ? (
        <li className="problem">
          <span>
            Entry {entry.number} (line {entry.line}):{" "}
            {entry.problems.join("; ")}
          </span>
        </li>
      ) : (
        <li>{entry.description}</li>
      )}
      {entry.warnings.map((warning) => (
        <li key={warning} className="warning">
          Entry {entry.number}: {warning}
        </li>
      ))}
    </>
  );
}

function RotaDefaultsForm({
  defaults,
  roles,
  onSavePattern,
  onSaveShape,
  onState,
}: {
  defaults: RotaDefaults;
  roles: ConfiguredRole[] | null;
  // The busier of the two halves' saves, said up to the card's title.
  onState: (state: SaveState) => void;
  onSavePattern: (pattern: string) => Promise<void>;
  onSaveShape: (seats: { roleId: string; count: number }[]) => Promise<void>;
}) {
  const today = todayDate();
  const { draft, setDraft, saved } = useDraft({
    source: defaults,
    toDraft: (d: RotaDefaults) => d.shiftPattern,
    same: samePattern,
  });
  const [offset, setOffset] = useState(0);
  const [shapeState, setShapeState] = useState<SaveState>("idle");
  // Where the pattern starts, as the server last read it: the calendar opens
  // on that month when it is still to come, and on this one otherwise.
  const [startsFrom, setStartsFrom] = useState("");

  const anchor = startsFrom > today ? startsFrom : today;
  const months = monthsFrom(anchor, offset, MONTHS_SHOWN);
  const {
    preview,
    current,
    error: previewError,
  } = useShiftPatternPreview(
    draft,
    months[0].first,
    months[months.length - 1].next,
  );
  // Kept through a pattern with problems, which answers with no date, so the
  // calendar does not jump back to this month while an entry is being fixed.
  const answered = preview?.startsFrom ?? "";
  if (answered !== "" && answered !== startsFrom) setStartsFrom(answered);

  const blank = draft.trim() === "";
  const hasProblems =
    (preview?.problems.length ?? 0) > 0 ||
    (preview?.entries.some((e) => e.problems.length > 0) ?? false);

  const { saveState, error } = useSaveWhenSettled({
    draft,
    saved,
    same: samePattern,
    save: onSavePattern,
    ready: !blank && current && !hasProblems,
  });
  const state = either(saveState, shapeState);
  useEffect(() => onState(state), [state, onState]);

  return (
    <div className="rota-defaults-card">
      <p className="settings-hint">
        Times are wall-clock in {defaults.shiftTimezone || "the time zone"}, so
        19:30 stays 19:30 when the clocks change.
      </p>
      <label className="settings-field">
        Shift pattern
        <textarea
          className="shift-pattern-text"
          value={draft}
          rows={Math.max(6, draft.split("\n").length + 1)}
          spellCheck={false}
          placeholder={PLACEHOLDER}
          onChange={(e) => setDraft(e.target.value)}
        />
      </label>
      <p className="settings-hint">
        One VEVENT per kind of shift, each with a DTSTART (the first shift), a
        DURATION (how long it runs) and an RRULE (how it repeats). An event
        copied out of a calendar works too. Every other week counts from each
        entry&rsquo;s own DTSTART, and nothing is minted before it.
      </p>

      <ul className="shift-pattern-lines" aria-live="polite">
        {blank && <li className="problem">No entries yet.</li>}
        {!blank &&
          preview?.problems.map((problem) => (
            <li key={problem} className="problem">
              {problem}
            </li>
          ))}
        {!blank &&
          preview?.entries.map((entry) => (
            <EntryReading key={entry.number} entry={entry} />
          ))}
        {!blank &&
          preview?.warnings.map((warning) => (
            <li key={warning} className="warning">
              {warning}
            </li>
          ))}
      </ul>
      {previewError && <p className="settings-error">{previewError}</p>}
      {error && <p className="settings-error">{error}</p>}

      <h3 className="shift-pattern-heading">Calendar</h3>
      {blank || hasProblems ? (
        <p className="settings-empty">Write the pattern to see its calendar.</p>
      ) : (
        <PatternCalendar
          months={months}
          starts={preview?.starts ?? []}
          startsFrom={preview?.startsFrom || startsFrom}
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
