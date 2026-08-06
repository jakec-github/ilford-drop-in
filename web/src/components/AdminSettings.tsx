import { useState } from "react";
import type { ReactNode } from "react";
import Button from "../ui/Button";
import Dialog from "../ui/Dialog";
import { useRoles } from "../hooks/useRoles";
import { useRotaDefaults } from "../hooks/useRotaDefaults";
import { useStandingPreallocations } from "../hooks/useStandingPreallocations";
import { useVolunteers } from "../hooks/useVolunteers";
import type {
  ConfiguredRole,
  NewStandingPreallocation,
  PersonRef,
  RoleColour,
  RoleEdit,
  RotaDefaults,
  ShapeSeat,
  ShiftTimes,
  Volunteer,
} from "../types";
import { CUSTOM_CHOICE, DEFAULT_ROLE_COLOUR, ROLE_COLOURS } from "../types";
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

// ShapeForm is the whole of editing the default Shape: every Role, with how
// many Seats of it a shift asks for.
//
// Every Role gets a row rather than there being an add-a-Role dance, because a
// drop-in has a handful of Roles and "how many of this one?" is the only
// question there is. Nought is how a Role is left out — the server refuses a
// Seat of nought, so those rows are dropped on the way rather than sent.
//
// Counts are held as strings so that a cleared box stays cleared while it is
// being retyped, rather than snapping to 0 under the cursor.
function ShapeForm({
  roles,
  shape,
  onSave,
  onClose,
}: {
  roles: ConfiguredRole[];
  shape: ShapeSeat[];
  onSave: (seats: { roleId: string; count: number }[]) => Promise<void>;
  onClose: () => void;
}) {
  const [counts, setCounts] = useState<Record<string, string>>(() =>
    Object.fromEntries(
      roles.map((role) => [
        role.id,
        String(shape.find((seat) => seat.roleId === role.id)?.count ?? 0),
      ]),
    ),
  );
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const seats = roles
    .map((role) => ({ roleId: role.id, count: Number(counts[role.id] || 0) }))
    .filter((seat) => seat.count > 0);
  const total = seats.reduce((sum, seat) => sum + seat.count, 0);

  async function save() {
    setSaving(true);
    setError(null);
    try {
      await onSave(seats);
      onClose();
    } catch (err: unknown) {
      // The server's own message names the Role whose ceiling was exceeded, so
      // it is shown as-is and the form stays open on what was typed.
      setError(err instanceof Error ? err.message : "Failed to save the shape");
      setSaving(false);
    }
  }

  return (
    <Dialog title="Default shape" onClose={onClose}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <p className="settings-hint">
          How many places of each Role a shift has. Every shift of a rota starts
          from this; leave a Role at 0 if a shift does not need one.
        </p>

        {roles.map((role) => (
          <label key={role.id} className="settings-field settings-seat">
            <span className="role-name" data-role-colour={role.colour}>
              {role.name}
            </span>
            {/* A capped Role says its ceiling here rather than only refusing at
                it: the box stops at that number, and a number box that will not
                go higher is a puzzle without the reason beside it. */}
            {role.max !== null && (
              <span className="settings-seat-max">at most {role.max}</span>
            )}
            <input
              type="number"
              min={0}
              max={role.max ?? undefined}
              value={counts[role.id] ?? "0"}
              onChange={(e) =>
                setCounts({ ...counts, [role.id]: e.target.value })
              }
            />
          </label>
        ))}

        <p className="settings-hint">
          {total > 0
            ? `A shift asks for ${total} ${total === 1 ? "person" : "people"} in total.`
            : "A shift asking for nobody cannot be allocated."}
        </p>

        {error && <p className="settings-error">{error}</p>}

        <div className="settings-actions">
          <Button onClick={onClose} disabled={saving}>
            Cancel
          </Button>
          <Button type="submit" disabled={saving}>
            {saving ? "Saving…" : "Save shape"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// How a Shape reads in the list of facts: "1 Team lead, 4 Service volunteer",
// in the order the Seats are filled.
function describeShape(shape: ShapeSeat[]): string {
  return shape.map((seat) => `${seat.count} ${seat.role}`).join(", ");
}

// What is being edited in the Rota Defaults: nothing, the times, or the Shape.
// One value rather than two booleans, so two dialogs cannot be open at once.
type EditingDefaults = "times" | "shape" | null;

// RotaDefaultsSettings is the settings an admin keeps for the drop-in as a
// whole: when the drop-in runs, and what a shift asks for. The allocation
// toggles join them here.
//
// Nothing seeds these, so "not set yet" is the state a new deployment is in
// rather than a fault — and the caption says what that costs, because
// allocation is the only thing it stops and nothing else on the screen will
// mention it.
function RotaDefaultsSettings() {
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
      blurb="What every rota starts from. When the drop-in runs, and how many people each shift asks for."
      action={
        defaults && (
          <span className="settings-section-actions">
            <Button size="small" onClick={() => setEditing("times")}>
              Edit times
            </Button>
            {/* Nothing to shape until Roles exist, and the section below says
                so — offering the button here would open a dialog with no rows
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
              A rota cannot be allocated until the shift times and the shape are
              set. Everything else — the rota, availability, the calendar feed —
              works without them.
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
          roles={roles}
          shape={defaults.defaultShape}
          onSave={saveShape}
          onClose={() => setEditing(null)}
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

// Which Shifts of a rota a Standing Preallocation lands on, offered as the
// handful of answers anybody actually gives. Each is a recurrence rule, which is
// what the server stores and what the seeding matches shift dates against; an
// admin picks the sentence and never sees the rule.
//
// Sundays because that is the day a rota is minted on — definition walks weekly
// from a Sunday. A cadence that is not weekly is out of scope until rota
// definition offers one.
const STANDING_RULES: { rrule: string; label: string }[] = [
  { rrule: "FREQ=WEEKLY;BYDAY=SU", label: "Every shift" },
  { rrule: "FREQ=MONTHLY;BYDAY=1SU", label: "The first Sunday of the month" },
  { rrule: "FREQ=MONTHLY;BYDAY=2SU", label: "The second Sunday of the month" },
  { rrule: "FREQ=MONTHLY;BYDAY=3SU", label: "The third Sunday of the month" },
  { rrule: "FREQ=MONTHLY;BYDAY=4SU", label: "The fourth Sunday of the month" },
  { rrule: "FREQ=MONTHLY;BYDAY=-1SU", label: "The last Sunday of the month" },
];

// How a stored rule reads in the list. A rule this app did not offer — written
// against the API, or offered by an older build — falls back to itself rather
// than to nothing: it is still doing something, and an admin deciding whether to
// remove it needs to see what.
function describeRule(rrule: string): string {
  return STANDING_RULES.find((r) => r.rrule === rrule)?.label ?? rrule;
}

// StandingPreallocationForm is the whole of making one: who, in which Role, on
// which shifts. There is no edit — a promise is made or it is not, and changing
// one is removing it and making the one that was meant — so this only ever
// creates.
function StandingPreallocationForm({
  roles,
  volunteers,
  volunteersError,
  onSave,
  onClose,
}: {
  roles: ConfiguredRole[];
  // null while the roster is still loading.
  volunteers: Volunteer[] | null;
  volunteersError: string | null;
  onSave: (standing: NewStandingPreallocation) => Promise<void>;
  onClose: () => void;
}) {
  const [choice, setChoice] = useState("");
  const [customName, setCustomName] = useState("");
  const [roleId, setRoleId] = useState(roles[0]?.id ?? "");
  const [rrule, setRRule] = useState(STANDING_RULES[0].rrule);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const isCustom = choice === CUSTOM_CHOICE;
  const trimmedName = customName.trim();
  const person: PersonRef | null = isCustom
    ? trimmedName
      ? { custom: trimmedName }
      : null
    : choice
      ? { volunteerId: choice }
      : null;

  // Only the Roles the chosen volunteer holds: the server refuses a pin for a
  // Role they do not, and offering it here would be offering a refusal. A custom
  // entry holds nothing on the roster and may be pinned to anything.
  const chosen = volunteers?.find((v) => v.id === choice) ?? null;
  const offeredRoles = isCustom
    ? roles
    : chosen
      ? roles.filter((r) => chosen.roles.includes(r.name))
      : roles;

  function handleChoice(value: string) {
    setChoice(value);
    // Keep the Role only if the new subject can hold it, so the form never
    // sits on a combination the server would turn down.
    const picked = volunteers?.find((v) => v.id === value);
    if (value !== CUSTOM_CHOICE && picked) {
      const held = roles.filter((r) => picked.roles.includes(r.name));
      if (!held.some((r) => r.id === roleId)) setRoleId(held[0]?.id ?? "");
    }
  }

  async function save() {
    if (!person) return;
    setSaving(true);
    setError(null);
    try {
      await onSave({ rrule, roleId, person });
      onClose();
    } catch (err: unknown) {
      // The server's own message names who was already promised those shifts,
      // so it is shown as-is and the form stays open on what was chosen.
      setError(err instanceof Error ? err.message : "Failed to add the pin");
      setSaving(false);
    }
  }

  return (
    <Dialog title="New standing preallocation" onClose={onClose}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <p className="settings-hint">
          Whoever you pin here is pinned to the matching shifts of every rota
          from now on, as it is defined. Rotas that already exist are not
          touched.
        </p>

        <label className="settings-field">
          Who
          <select
            value={choice}
            onChange={(e) => handleChoice(e.target.value)}
            disabled={volunteers === null && volunteersError === null}
          >
            <option value="">
              {volunteers === null && volunteersError === null
                ? "Loading the roster…"
                : "Choose someone…"}
            </option>
            {volunteers?.map((v) => (
              <option key={v.id} value={v.id}>
                {v.fullName}
              </option>
            ))}
            <option value={CUSTOM_CHOICE}>Someone not on the roster…</option>
          </select>
        </label>

        {/* The roster failing is not a dead end: a custom entry needs nothing
            from it, so the picker degrades to that rather than to nothing. */}
        {volunteersError && (
          <p className="settings-hint">
            Could not load the roster ({volunteersError}). You can still pin
            someone by name.
          </p>
        )}

        {isCustom && (
          <label className="settings-field">
            Name
            <input
              type="text"
              value={customName}
              onChange={(e) => setCustomName(e.target.value)}
              placeholder="e.g. Redbridge youth group"
            />
          </label>
        )}

        <label className="settings-field">
          Role
          <select value={roleId} onChange={(e) => setRoleId(e.target.value)}>
            {offeredRoles.map((role) => (
              <option key={role.id} value={role.id}>
                {role.name}
              </option>
            ))}
          </select>
        </label>

        <label className="settings-field">
          Which shifts
          <select value={rrule} onChange={(e) => setRRule(e.target.value)}>
            {STANDING_RULES.map((rule) => (
              <option key={rule.rrule} value={rule.rrule}>
                {rule.label}
              </option>
            ))}
          </select>
        </label>

        {error && <p className="settings-error">{error}</p>}

        <div className="settings-actions">
          <Button onClick={onClose} disabled={saving}>
            Cancel
          </Button>
          <Button
            type="submit"
            disabled={person === null || roleId === "" || saving}
          >
            {saving ? "Saving…" : "Add pin"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// StandingPreallocationsSettings is the pins an admin expects to make every
// rota. Add and remove are the only actions: a Standing Preallocation is one
// promise, and half-changing one is not a thing to want.
function StandingPreallocationsSettings() {
  const { standing, error, addStanding, removeStanding } =
    useStandingPreallocations();
  const { roles } = useRoles();
  const { volunteers, error: volunteersError } = useVolunteers();
  const [adding, setAdding] = useState(false);
  const [removeError, setRemoveError] = useState<string | null>(null);

  // Nothing to pin anybody into until Roles exist, and the Roles section above
  // says so — offering the button here would open a form with an empty picker.
  const canAdd = roles !== null && roles.length > 0;

  return (
    <SettingsSection
      title="Standing preallocations"
      blurb="The pins you expect to make every rota. Each one pins somebody to the matching shifts of a rota as it is defined."
      action={
        canAdd && (
          <Button size="small" onClick={() => setAdding(true)}>
            New pin
          </Button>
        )
      }
    >
      {error && (
        <p className="settings-error">Could not load the pins: {error}</p>
      )}
      {removeError && <p className="settings-error">{removeError}</p>}

      {standing === null && !error && <p className="settings-empty">Loading…</p>}

      {standing !== null && standing.length === 0 && (
        <p className="settings-empty">
          Nothing is pinned every rota. Add one for the people who always take
          the same shift — everyone else is pinned on the rota itself.
        </p>
      )}

      {standing !== null && standing.length > 0 && (
        <>
          <ul className="roles">
            {standing.map((pin) => (
              <li key={pin.id} className="role-row">
                <span className="role-name">{pin.name}</span>
                <span className="role-facts">
                  <span className="role-fact standing-role">{pin.role}</span>
                  <span className="role-fact standing-rule">
                    {describeRule(pin.rrule)}
                  </span>
                </span>
                <Button
                  size="small"
                  onClick={() => {
                    setRemoveError(null);
                    void removeStanding(pin.id).catch((err: unknown) => {
                      setRemoveError(
                        err instanceof Error
                          ? err.message
                          : "Failed to remove the pin",
                      );
                    });
                  }}
                >
                  Remove
                </Button>
              </li>
            ))}
          </ul>
          {/* Says the one thing about these that is not obvious from the list,
              and the thing an admin is most likely to get wrong: removing one
              does not unpin anybody from a rota that already exists. */}
          <p className="settings-caption">
            Removing a pin changes what the next rota starts from. The people it
            has already pinned stay pinned — unpin them on the rota itself.
          </p>
        </>
      )}

      {adding && roles && (
        <StandingPreallocationForm
          roles={roles}
          volunteers={volunteers}
          volunteersError={volunteersError}
          onSave={addStanding}
          onClose={() => setAdding(false)}
        />
      )}
    </SettingsSection>
  );
}

// AdminSettings is everything an admin decides about how the drop-in runs, as
// opposed to what an operator sets when deploying it (ADR 0006). It is a stack
// of independent sections: the Rota Defaults the whole drop-in runs on, the
// Roles volunteers hold, and the pins made every rota.
export default function AdminSettings() {
  return (
    <>
      <RotaDefaultsSettings />
      <RolesSettings />
      <StandingPreallocationsSettings />
    </>
  );
}
