import { useState } from "react";
import Button from "../ui/Button";
import Dialog from "../ui/Dialog";
import { useRoles } from "../hooks/useRoles";
import { useRotaDefaults } from "../hooks/useRotaDefaults";
import SettingsSection from "./SettingsSection";
import ShapeForm from "./ShapeForm";
import { describeShape } from "./shape";
import type { ShiftTimes } from "../types";

// ShiftTimesForm is the shift-time half of the Rota Defaults: when the drop-in
// starts, when it ends, and the zone those are read in. All three at once,
// because a time of day means nothing without the zone it is read in.
//
// The time fields are native time inputs, which read and write the same 24-hour
// "HH:MM" the server stores — so nothing here parses or formats a time, and a
// phone offers its own picker.
function ShiftTimesForm({
  defaults,
  onSave,
  onClose,
}: {
  defaults: ShiftTimes;
  onSave: (times: ShiftTimes) => Promise<void>;
  onClose: () => void;
}) {
  const [start, setStart] = useState(defaults.shiftStartTime);
  const [end, setEnd] = useState(defaults.shiftEndTime);
  const [timezone, setTimezone] = useState(defaults.shiftTimezone);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function save() {
    setSaving(true);
    setError(null);
    try {
      await onSave({
        shiftStartTime: start,
        shiftEndTime: end,
        shiftTimezone: timezone.trim(),
      });
      onClose();
    } catch (err: unknown) {
      // The server's own message names the field that was wrong — "a shift has
      // to end after it starts" is the whole explanation — so it is shown as-is
      // and the form stays open on what was typed.
      setError(
        err instanceof Error ? err.message : "Failed to save the shift times",
      );
      setSaving(false);
    }
  }

  return (
    <Dialog title="Shift times" onClose={onClose}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <label className="settings-field">
          Starts
          <input
            type="time"
            value={start}
            autoFocus
            onChange={(e) => setStart(e.target.value)}
          />
        </label>

        <label className="settings-field">
          Ends
          <input
            type="time"
            value={end}
            onChange={(e) => setEnd(e.target.value)}
          />
        </label>
        <p className="settings-hint">
          A shift ends the evening it starts, so the end has to be later than
          the start.
        </p>

        <label className="settings-field">
          Timezone
          <input
            type="text"
            value={timezone}
            onChange={(e) => setTimezone(e.target.value)}
            placeholder="Europe/London"
          />
        </label>
        <p className="settings-hint">
          The zone the times above are read in. Leave it as Europe/London unless
          the drop-in has moved.
        </p>

        {error && <p className="settings-error">{error}</p>}

        <div className="settings-actions">
          <Button onClick={onClose} disabled={saving}>
            Cancel
          </Button>
          <Button type="submit" disabled={start === "" || end === "" || saving}>
            {saving ? "Saving…" : "Save times"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// What is being edited in the Rota Defaults: nothing, the times, or the Shape.
// One value rather than two booleans, so two dialogs cannot be open at once.
type EditingDefaults = "times" | "shape" | null;

// RotaDefaultsCard is the settings an admin keeps for the drop-in as a whole:
// when the drop-in runs, and what a shift asks for.
//
// It is the one settings card read on two screens. On Settings it sits with the
// rest; on the Allocation tab it sits under the define form, because those two
// facts are what defining a rota spends and this is where they are stated
// (issue #176). One component rather than a read-only copy: an admin who finds
// the hours wrong at the moment of defining should fix them there, and a copy
// would be a second thing to keep true.
//
// Nothing seeds these, so "not set yet" is the state a new deployment is in
// rather than a fault — and the caption says what that costs, because a rota
// can neither be defined nor allocated until they are stated and nothing else
// on either screen will mention it.
export default function RotaDefaultsCard() {
  const { defaults, error, saveShiftTimes, saveShape } = useRotaDefaults();
  const { roles } = useRoles();
  const [editing, setEditing] = useState<EditingDefaults>(null);

  const timesSet =
    defaults !== null &&
    defaults.shiftStartTime !== "" &&
    defaults.shiftEndTime !== "";
  const shapeSet = defaults !== null && defaults.defaultShape.length > 0;

  return (
    <SettingsSection
      title="Rota Defaults"
      blurb="What every rota is made from. When the drop-in runs, and how many people each shift asks for."
      action={
        defaults && (
          <span className="settings-section-actions">
            <Button size="small" onClick={() => setEditing("times")}>
              Edit times
            </Button>
            {/* Nothing to shape until Roles exist, and the caption says so —
                offering the button here would open a dialog with no rows
                in it. */}
            {roles !== null && roles.length > 0 && (
              <Button size="small" onClick={() => setEditing("shape")}>
                Edit shape
              </Button>
            )}
          </span>
        )
      }
    >
      {error && (
        <p className="settings-error">Could not load the settings: {error}</p>
      )}

      {defaults === null && !error && (
        <p className="settings-empty">Loading…</p>
      )}

      {defaults !== null && (
        <>
          <dl className="settings-facts">
            <div className="settings-fact">
              <dt>Shift times</dt>
              <dd>
                {timesSet ? (
                  `${defaults.shiftStartTime} – ${defaults.shiftEndTime}`
                ) : (
                  <span className="settings-unset">Not set yet</span>
                )}
              </dd>
            </div>
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
          {(!timesSet || !shapeSet) && (
            <p className="settings-caption">
              {roles !== null && roles.length === 0
                ? "No roles have been added yet, so there is nothing a shift can ask for. Add them on Settings, then say what every shift needs here."
                : "A rota cannot be defined until the shift times and the shape are set. Everything else — the rota, availability, the calendar feed — works without them."}
            </p>
          )}
        </>
      )}

      {editing === "times" && defaults && (
        <ShiftTimesForm
          defaults={defaults}
          onSave={saveShiftTimes}
          onClose={() => setEditing(null)}
        />
      )}

      {editing === "shape" && defaults && roles && (
        <ShapeForm
          title="Default shape"
          intro="How many places of each Role a shift has. Every shift of a rota is minted with this; leave a Role at 0 if a shift does not need one, and a single shift can be changed on its own once the rota exists."
          saveLabel="Save shape"
          roles={roles}
          shape={defaults.defaultShape}
          onSave={saveShape}
          onClose={() => setEditing(null)}
        />
      )}
    </SettingsSection>
  );
}
