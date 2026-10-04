import { useState } from "react";
import Button from "../ui/Button";
import Dialog from "../ui/Dialog";
import { useRoles } from "../hooks/useRoles";
import { useRotaDefaults } from "../hooks/useRotaDefaults";
import { useStandingPreallocations } from "../hooks/useStandingPreallocations";
import { useVolunteers } from "../hooks/useVolunteers";
import RotaDefaultsCard from "./RotaDefaultsCard";
import SettingsSection from "./SettingsSection";
import StandingPreallocationForm from "./StandingPreallocationForm";
import { describeRule } from "./standingRules";
import type {
  AllocationSettings,
  ConfiguredRole,
  RoleColour,
  RoleEdit,
  SwitchableConstraint,
} from "../types";
import { DEFAULT_ROLE_COLOUR, ROLE_COLOURS } from "../types";
import "./OrganiserSettings.css";

// The colour a new Role starts on: the dullest token, so an Organiser who does not
// care about colour does not accidentally claim a distinctive one.
const NEW_ROLE_COLOUR: RoleColour = DEFAULT_ROLE_COLOUR;

// RoleForm is the whole of creating and editing a Role: one form, because an
// edit says everything a creation says. `role` is the Role being edited, or
// null when one is being made.
//
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
          {/* How many of each Role a shift asks for is the Shape's business,
              not a Role's — said here because the list otherwise looks like it
              is missing the number. */}
          <p className="settings-caption">
            How many of each Role a shift needs is set by its shape, in Rota
            Defaults.
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

// StandingPreallocationsSettings is the pins an Organiser expects to make every
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

      {standing === null && !error && (
        <p className="settings-empty">Loading…</p>
      )}

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
              and the thing an Organiser is most likely to get wrong: removing one
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

// AllocationRulesForm switches the optional allocator rules on and off, and
// asks for the one value a rule carries.
//
// Every rule the server offered is drawn from the list it sent, so a rule
// arriving or leaving needs no change here: the registry lives in Go and this
// renders it (ADR 0006).
function AllocationRulesForm({
  settings,
  constraints,
  onSave,
  onClose,
}: {
  settings: AllocationSettings;
  constraints: SwitchableConstraint[];
  onSave: (settings: AllocationSettings) => Promise<void>;
  onClose: () => void;
}) {
  const [enabled, setEnabled] = useState<Record<string, boolean>>(
    settings.enabled,
  );
  // The share is held as a percentage because that is how anybody says it —
  // "a third of the rota", not "0.34" — and as a string so the box can be
  // emptied while it is being retyped. It converts on the way out.
  const [percent, setPercent] = useState(
    settings.maxFrequency > 0
      ? String(Math.round(settings.maxFrequency * 100))
      : "",
  );
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function save() {
    setSaving(true);
    setError(null);
    try {
      await onSave({
        enabled,
        maxFrequency: percent === "" ? 0 : Number(percent) / 100,
      });
      onClose();
    } catch (err: unknown) {
      // The server's own message names what was wrong with it, so it is shown
      // as-is and the form stays open on what was typed.
      setError(
        err instanceof Error
          ? err.message
          : "Failed to save the allocation rules",
      );
      setSaving(false);
    }
  }

  return (
    <Dialog title="Allocation rules" onClose={onClose}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        {constraints.map((constraint) => (
          <div key={constraint.name} className="rule-choice">
            <label className="rule-switch">
              <input
                type="checkbox"
                checked={enabled[constraint.name] ?? false}
                onChange={(e) =>
                  setEnabled({
                    ...enabled,
                    [constraint.name]: e.target.checked,
                  })
                }
              />
              {constraint.label}
            </label>
            <p className="settings-hint rule-description">
              {constraint.description}
            </p>

            {/* The value a rule carries belongs to that rule, so it sits under
                it and appears only when the rule is on — an answer to a
                question nobody asked is one more box to wonder about. */}
            {constraint.valueLabel && enabled[constraint.name] && (
              <label className="settings-field rule-value">
                {constraint.valueLabel}
                <input
                  type="number"
                  min={1}
                  max={100}
                  step={1}
                  value={percent}
                  onChange={(e) => setPercent(e.target.value)}
                />
              </label>
            )}
          </div>
        ))}

        {error && <p className="settings-error">{error}</p>}

        <div className="settings-actions">
          <Button onClick={onClose} disabled={saving}>
            Cancel
          </Button>
          <Button type="submit" disabled={saving}>
            {saving ? "Saving\u2026" : "Save rules"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// AllocationRulesSettings is which optional rules the solver applies. The
// fundamental ones are deliberately absent: a rota without them is not a rota,
// so they are not an Organiser's decision to make.
function AllocationRulesSettings() {
  const { defaults, saveAllocationRules } = useRotaDefaults();
  const [editing, setEditing] = useState(false);

  const onRules =
    defaults?.switchableConstraints.filter(
      (c) => defaults.allocationSettings.enabled[c.name],
    ) ?? [];

  return (
    <SettingsSection
      title="Allocation rules"
      blurb="The optional rules the allocator keeps. The ones that make a rota a rota are always on and are not listed."
      action={
        defaults && (
          <Button size="small" onClick={() => setEditing(true)}>
            Edit rules
          </Button>
        )
      }
    >
      {defaults === null && <p className="settings-empty">Loading\u2026</p>}

      {defaults !== null && (
        <>
          <dl className="settings-facts rule-facts">
            {defaults.switchableConstraints.map((constraint) => (
              <div key={constraint.name} className="settings-fact">
                <dt>{constraint.label}</dt>
                <dd>
                  {defaults.allocationSettings.enabled[constraint.name] ? (
                    "On"
                  ) : (
                    <span className="settings-unset">Off</span>
                  )}
                  {/* The value reads on the same line as the rule it belongs
                      to, and only while that rule is on. */}
                  {constraint.valueLabel &&
                    defaults.allocationSettings.enabled[constraint.name] &&
                    ` \u2014 at most ${Math.round(
                      defaults.allocationSettings.maxFrequency * 100,
                    )}% of a rota`}
                </dd>
              </div>
            ))}
          </dl>
          {onRules.length === 0 && (
            <p className="settings-caption">
              Nothing optional is switched on, so the allocator will keep only
              the rules a rota cannot be made without.
            </p>
          )}
        </>
      )}

      {editing && defaults && (
        <AllocationRulesForm
          settings={defaults.allocationSettings}
          constraints={defaults.switchableConstraints}
          onSave={saveAllocationRules}
          onClose={() => setEditing(false)}
        />
      )}
    </SettingsSection>
  );
}

// OrganiserSettings is everything an Organiser decides about how the drop-in runs, as
// opposed to what an operator sets when deploying it (ADR 0006). It is a stack
// of independent sections: the Rota Defaults the whole drop-in runs on, the
// Roles volunteers hold, and the pins made every rota.
//
// The Rota Defaults card is the one section that is not only here — the define
// screen shows the same component, because defining a rota is spending it
// (issue #176). This screen remains where it belongs: an Organiser looking for a
// setting finds every one of them in one place.
export default function OrganiserSettings() {
  return (
    <>
      <RotaDefaultsCard />
      <AllocationRulesSettings />
      <RolesSettings />
      <StandingPreallocationsSettings />
    </>
  );
}
