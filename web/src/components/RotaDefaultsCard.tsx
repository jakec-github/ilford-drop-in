import { useState } from "react";
import Button from "../ui/Button";
import { useRoles } from "../hooks/useRoles";
import { useRotaDefaults } from "../hooks/useRotaDefaults";
import SettingsSection from "./SettingsSection";
import { describeShape } from "./shape";
import { useShiftPatternPreview } from "../hooks/useShiftPatternPreview";
import RotaDefaultsEditor from "./RotaDefaultsEditor";
import { addDays, formatDay, formatLength } from "./patternDates";
import type { ShiftPattern } from "../types";

// PatternReading is the saved Shift pattern in plain English, one line per
// line of it, as the server reads it back. Empty until the reading arrives:
// the raw RRULE is not worth showing an Organiser for the moment it takes.
function PatternReading({
  pattern,
  startFrom,
  timezone,
}: {
  pattern: string;
  startFrom: string;
  timezone: string;
}) {
  // Any span will do: only the lines' readings are wanted here.
  const { preview } = useShiftPatternPreview(
    pattern,
    startFrom,
    timezone,
    startFrom,
    addDays(startFrom, 1),
  );
  if (!preview) return null;
  return (
    <ul className="settings-pattern-lines">
      {preview.lines.map((line) => (
        <li key={line.number}>
          {line.problems.length > 0 ? (
            <code>{line.text}</code>
          ) : (
            line.description
          )}
        </li>
      ))}
    </ul>
  );
}

// RotaDefaultsCard is the settings an Organiser keeps for the Organisation as a whole:
// when the Organisation runs, and what a shift asks for.
//
// It is the one settings card read on two screens. On Settings it sits with the
// rest; on the Allocation tab it sits under the define form, because those two
// facts are what defining a rota spends and this is where they are stated
// (issue #176). One component rather than a read-only copy: an Organiser who finds
// the pattern wrong at the moment of defining should fix it there, and a copy
// would be a second thing to keep true.
//
// Nothing seeds these, so "not set yet" is the state a new deployment is in
// rather than a fault — and the caption says what that costs, because a rota
// cannot be defined until they are stated.
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
  const saveShiftPattern = async (pattern: ShiftPattern) => {
    await rotaDefaults.saveShiftPattern(pattern);
    onSaved?.();
  };
  const saveShape = async (seats: { roleId: string; count: number }[]) => {
    await rotaDefaults.saveShape(seats);
    onSaved?.();
  };
  const { roles } = useRoles();
  const [editing, setEditing] = useState(false);

  const patternSet =
    defaults !== null &&
    defaults.shiftPattern !== "" &&
    defaults.patternStartFrom !== "" &&
    defaults.shiftLengthMinutes > 0;
  const shapeSet = defaults !== null && defaults.defaultShape.length > 0;

  return (
    <SettingsSection
      title="Rota Defaults"
      blurb="What every rota is made from. When the organisation runs, and how many people each shift asks for."
      action={
        defaults &&
        roles !== null &&
        !editing && (
          // Named in full for a screen reader: every Role on Settings has an
          // Edit button of its own.
          <Button
            size="small"
            aria-label="Edit Rota Defaults"
            onClick={() => setEditing(true)}
          >
            Edit
          </Button>
        )
      }
    >
      {error && (
        <p className="settings-error">Could not load the settings: {error}</p>
      )}

      {defaults === null && !error && (
        <p className="settings-empty">Loading…</p>
      )}

      {editing && defaults && roles && (
        <RotaDefaultsEditor
          saved={defaults}
          roles={roles}
          onSavePattern={saveShiftPattern}
          onSaveShape={saveShape}
          onClose={() => setEditing(false)}
        />
      )}

      {defaults !== null && !editing && (
        <>
          <dl className="settings-facts">
            <div className="settings-fact">
              <dt>Shift pattern</dt>
              <dd>
                {patternSet ? (
                  <PatternReading
                    pattern={defaults.shiftPattern}
                    startFrom={defaults.patternStartFrom}
                    timezone={defaults.shiftTimezone}
                  />
                ) : (
                  <span className="settings-unset">Not set yet</span>
                )}
              </dd>
            </div>
            {patternSet && (
              <>
                <div className="settings-fact">
                  <dt>Start from</dt>
                  <dd>{formatDay(defaults.patternStartFrom)}</dd>
                </div>
                <div className="settings-fact">
                  <dt>Shift length</dt>
                  <dd>{formatLength(defaults.shiftLengthMinutes)}</dd>
                </div>
              </>
            )}
            <div className="settings-fact">
              <dt>Timezone</dt>
              <dd>{defaults.shiftTimezone}</dd>
            </div>
            <div className="settings-fact">
              <dt>Shape</dt>
              <dd>
                {shapeSet ? (
                  describeShape(defaults.defaultShape)
                ) : (
                  <span className="settings-unset">Not set yet</span>
                )}
              </dd>
            </div>
          </dl>
          {(!patternSet || !shapeSet) && (
            <p className="settings-caption">
              {roles !== null && roles.length === 0
                ? "No roles have been added yet, so there is nothing a shift can ask for. Add them on Settings, then say what every shift needs here."
                : "A rota cannot be defined until the shift pattern and the shape are set. Everything else — the rota, availability, the calendar feed — works without them."}
            </p>
          )}
        </>
      )}

    </SettingsSection>
  );
}
