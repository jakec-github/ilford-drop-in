import { useState } from "react";
import type { ReactNode } from "react";
import Button from "../ui/Button";
import Dialog from "../ui/Dialog";
import { useRoles } from "../hooks/useRoles";
import { useRotaDefaults } from "../hooks/useRotaDefaults";
import type {
  ConfiguredRole,
  RoleColour,
  RoleEdit,
  RotaDefaults,
} from "../types";
import { DEFAULT_ROLE_COLOUR, ROLE_COLOURS } from "../types";
import "./AdminSettings.css";

// SettingsSection is one thing an admin decides about how the drop-in runs.
// Each is its own card because they are independent: Roles now, Rota Defaults
// and Standing Preallocations later, and nothing about one section should have
// to know how many others there are.
function SettingsSection({
  title,
  blurb,
  action,
  children,
}: {
  title: string;
  blurb: string;
  // The one thing this section can be asked to do, in the header rather than
  // below the list — a list that grows would otherwise walk its own button off
  // the bottom of the screen.
  action?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="admin-panel settings-section">
      <header className="settings-section-head">
        <div>
          <h2>{title}</h2>
          <p className="settings-blurb">{blurb}</p>
        </div>
        {action}
      </header>
      {children}
    </section>
  );
}

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
  defaults: RotaDefaults;
  onSave: (defaults: RotaDefaults) => Promise<void>;
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

// RotaDefaultsSettings is the settings an admin keeps for the drop-in as a
// whole. It holds the shift times today; the default Shape, the allocation
// toggles and the Standing Preallocations join it here.
//
// Nothing seeds these, so "not set yet" is the state a new deployment is in
// rather than a fault — and the caption says what that costs, because
// allocation is the only thing it stops and nothing else on the screen will
// mention it.
function RotaDefaultsSettings() {
  const { defaults, error, saveShiftTimes } = useRotaDefaults();
  const [editing, setEditing] = useState(false);

  const timesSet =
    defaults !== null &&
    defaults.shiftStartTime !== "" &&
    defaults.shiftEndTime !== "";

  return (
    <SettingsSection
      title="Rota Defaults"
      blurb="What every rota starts from. When the drop-in runs, and the zone its times are read in."
      action={
        defaults && (
          <Button size="small" onClick={() => setEditing(true)}>
            Edit times
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
          </dl>
          {!timesSet && (
            <p className="settings-caption">
              A rota cannot be allocated until the shift times are set.
              Everything else — the rota, availability, the calendar feed —
              works without them.
            </p>
          )}
        </>
      )}

      {editing && defaults && (
        <ShiftTimesForm
          defaults={defaults}
          onSave={saveShiftTimes}
          onClose={() => setEditing(false)}
        />
      )}
    </SettingsSection>
  );
}

// How a Role's ceiling reads in a list. An uncapped Role is not "unlimited" so
// much as the one a shift's size is spent on, which is a different thing to say
// and is said in the caption under the list rather than on every row.
function ceilingLabel(max: number | null): string {
  if (max === null) return "No limit";
  return max === 1 ? "1 per shift" : `${max} per shift`;
}

// The colour a new Role starts on: the dullest token, so an admin who does not
// care about colour does not accidentally claim a distinctive one.
const NEW_ROLE_COLOUR: RoleColour = DEFAULT_ROLE_COLOUR;

// RoleForm is the whole of creating and editing a Role: one form, because an
// edit says everything a creation says. `role` is the Role being edited, or
// null when one is being made.
//
// It holds the ceiling as a string rather than a number so that "no ceiling"
// has an obvious representation — an empty box — rather than being smuggled in
// as 0, which is a ceiling no shift could ever satisfy.
function RoleForm({
  role,
  nextPriority,
  onSave,
  onClose,
}: {
  role: ConfiguredRole | null;
  nextPriority: number;
  onSave: (edit: RoleEdit) => Promise<void>;
  onClose: () => void;
}) {
  const [name, setName] = useState(role?.name ?? "");
  const [max, setMax] = useState(role?.max == null ? "" : String(role.max));
  const [priority, setPriority] = useState(
    (role?.priority ?? nextPriority).toString(),
  );
  const [colour, setColour] = useState<RoleColour>(
    role?.colour ?? NEW_ROLE_COLOUR,
  );
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const trimmed = name.trim();
  // A rename is the one edit this app cannot finish on its own, so it is
  // flagged the moment the field diverges rather than on the way out.
  const renaming = role !== null && trimmed !== "" && trimmed !== role.name;

  async function save() {
    setSaving(true);
    setError(null);
    try {
      await onSave({
        name: trimmed,
        max: max.trim() === "" ? null : Number(max),
        priority: Number(priority),
        colour,
      });
      onClose();
    } catch (err: unknown) {
      // The server's own message says which name was taken or which field was
      // wrong, so it is shown as-is; the form stays open on what was typed.
      setError(err instanceof Error ? err.message : "Failed to save the role");
      setSaving(false);
    }
  }

  return (
    <Dialog title={role ? `Edit ${role.name}` : "New role"} onClose={onClose}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <label className="settings-field">
          Name
          <input
            type="text"
            value={name}
            autoFocus
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. Food collector"
          />
        </label>

        {renaming && (
          <p className="settings-warning" role="status">
            Renaming only changes this app. The roster sheet holds Roles by
            name, so change “{role.name}” to “{trimmed}” there too — until you
            do, nobody holds this Role.
          </p>
        )}

        <label className="settings-field">
          Most per shift
          <input
            type="number"
            min={1}
            value={max}
            onChange={(e) => setMax(e.target.value)}
            placeholder="No limit"
          />
        </label>
        <p className="settings-hint">
          Leave blank for no limit. The rest of a shift’s places are filled with
          the Role that has no limit.
        </p>

        <label className="settings-field">
          Priority
          <input
            type="number"
            value={priority}
            onChange={(e) => setPriority(e.target.value)}
          />
        </label>
        <p className="settings-hint">
          Lowest first: when volunteers are scarce, this is the order places are
          filled in.
        </p>

        <fieldset className="settings-field settings-swatches">
          <legend>Colour</legend>
          {ROLE_COLOURS.map((token) => (
            <label
              key={token}
              className="settings-swatch"
              data-role-colour={token}
              title={token}
            >
              <input
                type="radio"
                name="colour"
                value={token}
                checked={colour === token}
                onChange={() => setColour(token)}
              />
              <span className="settings-swatch-dot" aria-hidden="true" />
              <span className="settings-swatch-name">{token}</span>
            </label>
          ))}
        </fieldset>

        {error && <p className="settings-error">{error}</p>}

        <div className="settings-actions">
          <Button onClick={onClose} disabled={saving}>
            Cancel
          </Button>
          <Button type="submit" disabled={trimmed === "" || saving}>
            {saving ? "Saving…" : role ? "Save changes" : "Add role"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// One Role in the list. Editing is the only thing that can be done to it:
// Roles are permanent, so there is no delete and no retire here or anywhere
// else (ADR 0006).
function RoleRow({
  role,
  onEdit,
}: {
  role: ConfiguredRole;
  onEdit: () => void;
}) {
  return (
    <li className="role-row">
      <span className="role-name" data-role-colour={role.colour}>
        {role.name}
      </span>
      <span className="role-facts">
        <span className="role-fact">{ceilingLabel(role.max)}</span>
        <span className="role-fact">Priority {role.priority}</span>
      </span>
      <Button size="small" onClick={onEdit}>
        Edit
      </Button>
    </li>
  );
}

// What is being edited: nothing, a new Role, or one that exists. Held as one
// value rather than a boolean and a selection, so the dialog cannot be open
// with no Role and no intent to create one.
type Editing = { role: ConfiguredRole | null } | null;

// RolesSettings is the Roles section: the jobs volunteers hold, listed in the
// order their places are filled, with the one action that changes them.
function RolesSettings() {
  const { roles, error, addRole, saveRole } = useRoles();
  const [editing, setEditing] = useState<Editing>(null);

  // A new Role goes after the ones that exist by default, so adding one never
  // silently reorders the filling of everything else.
  const nextPriority =
    roles && roles.length > 0
      ? Math.max(...roles.map((r) => r.priority)) + 1
      : 1;

  const uncapped = roles?.find((r) => r.max === null) ?? null;

  return (
    <SettingsSection
      title="Roles"
      blurb="The jobs volunteers do on a shift. A volunteer can only be given a Role they hold on the roster."
      action={
        <Button size="small" onClick={() => setEditing({ role: null })}>
          New role
        </Button>
      }
    >
      {error && <p className="settings-error">Could not load roles: {error}</p>}

      {roles === null && !error && <p className="settings-empty">Loading…</p>}

      {roles !== null && roles.length === 0 && (
        <p className="settings-empty">
          No roles yet. Add the jobs volunteers do at the drop-in — a rota
          cannot be allocated without them.
        </p>
      )}

      {roles !== null && roles.length > 0 && (
        <>
          <ul className="roles">
            {roles.map((role) => (
              <RoleRow
                key={role.id}
                role={role}
                onEdit={() => setEditing({ role })}
              />
            ))}
          </ul>
          {/* Which Role has no ceiling decides where a shift's remaining places
              go, and capping every Role leaves nowhere for them — a mistake
              that would otherwise only show up at allocation. */}
          <p className="settings-caption">
            {uncapped ? (
              <>
                A shift’s remaining places are filled with{" "}
                <strong>{uncapped.name}</strong>.
              </>
            ) : (
              <>
                Every Role has a limit, so a shift has nowhere to put its
                remaining places. Leave one Role without a limit.
              </>
            )}
          </p>
        </>
      )}

      {editing && (
        <RoleForm
          role={editing.role}
          nextPriority={nextPriority}
          onSave={(edit) =>
            editing.role ? saveRole(editing.role.id, edit) : addRole(edit)
          }
          onClose={() => setEditing(null)}
        />
      )}
    </SettingsSection>
  );
}

// AdminSettings is everything an admin decides about how the drop-in runs, as
// opposed to what an operator sets when deploying it (ADR 0006). It is a stack
// of independent sections: the Rota Defaults the whole drop-in runs on, then
// the Roles volunteers hold. Standing Preallocations join them here.
export default function AdminSettings() {
  return (
    <>
      <RotaDefaultsSettings />
      <RolesSettings />
    </>
  );
}
