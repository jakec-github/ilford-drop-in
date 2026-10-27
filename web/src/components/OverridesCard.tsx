import { useState } from "react";
import Button from "../ui/Button";
import Chip from "../ui/Chip";
import { useOverrideRule } from "../hooks/useOverrideRule";
import { useOverrides } from "../hooks/useOverrides";
import { useRoles } from "../hooks/useRoles";
import { useVolunteers } from "../hooks/useVolunteers";
import SettingsSection from "./SettingsSection";
import ShapeSeats from "./ShapeSeats";
import { formatDay, isDate, today } from "./patternDates";
import { describeShape, seatCountsOf, seatsOf, type SeatCounts } from "./shape";
import type {
  ConfiguredRole,
  Override,
  OverrideDraft,
  OverrideGranularity,
  PersonRef,
  Volunteer,
} from "../types";
import { CUSTOM_CHOICE } from "../types";
import "./OverridesCard.css";

// What a new Override's rule box starts with: the commonest thing an
// Organiser overrides, and a working example of the syntax to edit from.
const NEW_RULE = "FREQ=MONTHLY;BYDAY=1SU";

// How an Override matches, in the words the card uses for it.
function GranularityBadge({
  granularity,
}: {
  granularity: OverrideGranularity;
}) {
  return granularity === "time" ? (
    <span className="override-badge override-badge--time">
      the shift at that time
    </span>
  ) : (
    <span className="override-badge">every shift that day</span>
  );
}

// One Pin as a chip in its Role's colour: a pill for a volunteer, a tag for a
// custom entry, as on the rota.
function PinChip({
  name,
  role,
  custom,
  colour,
  onRemove,
}: {
  name: string;
  role: string;
  custom: boolean;
  colour: string | undefined;
  onRemove?: () => void;
}) {
  return (
    <span className="override-pin" data-role-colour={colour}>
      <Chip
        label={`${name} · ${role}`}
        shape={custom ? "tag" : "pill"}
        removeLabel={`Unpin ${name}`}
        onRemove={onRemove}
      />
    </span>
  );
}

// PinDraft is one Pin in an Override being edited. key tells two custom
// entries of the same name apart, since those may repeat.
interface PinDraft {
  key: string;
  roleId: string;
  person: PersonRef;
  name: string;
}

let nextPinKey = 0;
function pinKey(): string {
  nextPinKey += 1;
  return `pin-${nextPinKey}`;
}

// PinAdder is the one row that adds a Pin: who, then which Role. Every Role is
// offered whoever is chosen, since not holding one is a rule and rules bind
// the allocator, not the Organiser (ADR 0010).
function PinAdder({
  roles,
  volunteers,
  volunteersError,
  onAdd,
}: {
  roles: ConfiguredRole[];
  volunteers: Volunteer[] | null;
  volunteersError: string | null;
  onAdd: (pin: Omit<PinDraft, "key">) => void;
}) {
  const [choice, setChoice] = useState("");
  const [customName, setCustomName] = useState("");
  const [roleId, setRoleId] = useState("");

  const isCustom = choice === CUSTOM_CHOICE;
  const chosen = volunteers?.find((v) => v.id === choice) ?? null;
  // The Role they mostly do, until somebody picks one.
  const role =
    roles.find((r) => r.id === roleId) ??
    roles.find((r) => chosen?.roles.includes(r.name)) ??
    roles[0] ??
    null;
  const name = isCustom ? customName.trim() : (chosen?.name ?? "");
  const ready = role !== null && name !== "";
  const loading = volunteers === null && volunteersError === null;

  function add() {
    if (!ready) return;
    onAdd({
      roleId: role.id,
      person: isCustom ? { custom: name } : { volunteerId: choice },
      name,
    });
    setChoice("");
    setCustomName("");
    setRoleId("");
  }

  return (
    <div className="override-adder">
      <select
        aria-label="Who to pin"
        value={choice}
        disabled={loading}
        onChange={(e) => setChoice(e.target.value)}
      >
        <option value="">
          {loading ? "Loading the roster…" : "Pin somebody…"}
        </option>
        {volunteers?.map((v) => (
          <option key={v.id} value={v.id}>
            {v.fullName}
            {v.active ? "" : " (not active)"}
          </option>
        ))}
        <option value={CUSTOM_CHOICE}>Someone not on the roster…</option>
      </select>
      {isCustom && (
        <input
          type="text"
          aria-label="Name"
          value={customName}
          placeholder="e.g. Redbridge youth group"
          onChange={(e) => setCustomName(e.target.value)}
        />
      )}
      <select
        aria-label="Role"
        value={role?.id ?? ""}
        onChange={(e) => setRoleId(e.target.value)}
      >
        {roles.map((r) => (
          <option key={r.id} value={r.id}>
            {r.name}
          </option>
        ))}
      </select>
      <Button size="small" disabled={!ready} onClick={add}>
        Pin
      </Button>
      {volunteersError && (
        <p className="settings-hint">
          Could not load the roster ({volunteersError}). You can still pin
          someone by name.
        </p>
      )}
    </div>
  );
}

// OverrideEditor is the whole of stating one Override: its rule, read back as
// it is typed, the date it counts from, and what it changes. It edits in place
// in the list, as the prototype does, rather than in a dialog, so the
// Overrides around it stay in view while one is written.
export function OverrideEditor({
  number,
  override,
  roles,
  volunteers,
  volunteersError,
  onSave,
  onCancel,
}: {
  number: number;
  override: Override | null;
  roles: ConfiguredRole[];
  volunteers: Volunteer[] | null;
  volunteersError: string | null;
  onSave: (draft: OverrideDraft) => Promise<void>;
  onCancel: () => void;
}) {
  const [rrule, setRRule] = useState(override?.rrule ?? NEW_RULE);
  // Today for a new one, as for the pattern; the server settles what today is.
  const [startFrom, setStartFrom] = useState(override?.startFrom ?? today());
  const [shapeOn, setShapeOn] = useState(override?.shape != null);
  const [counts, setCounts] = useState<SeatCounts>(() =>
    seatCountsOf(roles, override?.shape ?? []),
  );
  const [pins, setPins] = useState<PinDraft[]>(
    () =>
      override?.pins.map((pin) => ({
        key: pinKey(),
        roleId: pin.roleId,
        person: pin.volunteerId
          ? { volunteerId: pin.volunteerId }
          : { custom: pin.custom ?? pin.name },
        name: pin.name,
      })) ?? [],
  );
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const anchor = isDate(startFrom) ? startFrom : today();
  const { reading, error: readingError } = useOverrideRule(rrule, anchor);
  const problems = reading?.problems ?? [];
  const readable = rrule.trim() !== "" && problems.length === 0;

  // One person once on an Override: pinning them again changes their Role,
  // since a person fills one Seat on a Shift. Custom entries may repeat.
  function addPin(pin: Omit<PinDraft, "key">) {
    setPins((current) => {
      const id = "volunteerId" in pin.person ? pin.person.volunteerId : null;
      const kept = id
        ? current.filter(
            (p) => !("volunteerId" in p.person && p.person.volunteerId === id),
          )
        : current;
      return [...kept, { ...pin, key: pinKey() }];
    });
  }

  async function save() {
    setSaving(true);
    setError(null);
    try {
      await onSave({
        rrule: rrule.trim(),
        startFrom,
        shape: shapeOn ? seatsOf(roles, counts) : null,
        pins: pins.map((p) => ({ roleId: p.roleId, person: p.person })),
      });
    } catch (err: unknown) {
      // The server's own message says what it could not accept, and the
      // editor stays open on what was typed.
      setError(
        err instanceof Error ? err.message : "Failed to save the override",
      );
      setSaving(false);
    }
  }

  const roleOf = (id: string) => roles.find((r) => r.id === id);

  return (
    <form
      className="override override--editing"
      aria-label={`Override ${number}`}
      onSubmit={(e) => {
        e.preventDefault();
        void save();
      }}
    >
      <header className="override-head">
        <span className="override-number" aria-hidden="true">
          {number}
        </span>
        <input
          className="override-rule"
          type="text"
          aria-label={`Override ${number} rule`}
          spellCheck={false}
          autoFocus
          value={rrule}
          onChange={(e) => setRRule(e.target.value)}
        />
      </header>

      <div className="override-reading" aria-live="polite">
        {readingError && <p className="settings-error">{readingError}</p>}
        {problems.length > 0 ? (
          <ul className="override-problems">
            {problems.map((problem) => (
              <li key={problem}>{problem}</li>
            ))}
          </ul>
        ) : (
          reading && (
            <p className="override-description">
              {reading.description}{" "}
              {reading.granularity && (
                <GranularityBadge granularity={reading.granularity} />
              )}
            </p>
          )
        )}
      </div>

      <p className="settings-hint">
        With BYHOUR and BYMINUTE it changes only the shift the pattern starts at
        that time. Without them, it changes every shift on those days.
      </p>

      <div className="override-props">
        <label htmlFor={`override-${number}-start`}>Start from</label>
        <div>
          <input
            id={`override-${number}-start`}
            type="date"
            value={startFrom}
            onChange={(e) => setStartFrom(e.target.value)}
          />
        </div>

        <label>
          <input
            type="checkbox"
            checked={shapeOn}
            onChange={(e) => setShapeOn(e.target.checked)}
          />{" "}
          Replace the shape
        </label>
        <div>
          {shapeOn ? (
            <div className="override-shape">
              <ShapeSeats roles={roles} counts={counts} onChange={setCounts} />
            </div>
          ) : (
            <span className="override-off">the default shape</span>
          )}
        </div>

        <span className="override-label">Pins</span>
        <div>
          <div className="override-pins">
            {pins.length === 0 && <span className="override-off">none</span>}
            {pins.map((pin) => {
              const role = roleOf(pin.roleId);
              return (
                <PinChip
                  key={pin.key}
                  name={pin.name}
                  role={role?.name ?? "unknown role"}
                  custom={"custom" in pin.person}
                  colour={role?.colour}
                  onRemove={() =>
                    setPins((current) =>
                      current.filter((p) => p.key !== pin.key),
                    )
                  }
                />
              );
            })}
          </div>
          <PinAdder
            roles={roles}
            volunteers={volunteers}
            volunteersError={volunteersError}
            onAdd={addPin}
          />
        </div>
      </div>

      {error && <p className="settings-error">{error}</p>}

      <div className="settings-actions">
        <Button onClick={onCancel} disabled={saving}>
          Cancel
        </Button>
        <Button type="submit" disabled={!readable || saving}>
          {saving ? "Saving…" : override ? "Save override" : "Add override"}
        </Button>
      </div>
    </form>
  );
}

// OverrideView is one saved Override, read: its rule in plain English, how it
// matches, and what it changes, with the actions that change it.
function OverrideView({
  override,
  count,
  roles,
  busy,
  onEdit,
  onMove,
  onRemove,
}: {
  override: Override;
  count: number;
  roles: ConfiguredRole[];
  busy: boolean;
  onEdit: () => void;
  onMove: (by: -1 | 1) => void;
  onRemove: () => void;
}) {
  const n = override.number;
  return (
    <article className="override" aria-label={`Override ${n}`}>
      <header className="override-head">
        <span className="override-number" aria-hidden="true">
          {n}
        </span>
        <p className="override-description">
          {override.problems.length > 0 ? (
            <>
              <code>{override.rrule}</code>{" "}
              <span className="settings-error">
                cannot be read: {override.problems.join("; ")}
              </span>
            </>
          ) : (
            <>
              {override.description}{" "}
              <GranularityBadge granularity={override.granularity} />
            </>
          )}
        </p>
        <div className="override-actions">
          <Button
            size="small"
            aria-label={`Move override ${n} earlier`}
            title="Earlier: loses to the ones after it"
            disabled={busy || n === 1}
            onClick={() => onMove(-1)}
          >
            ↑
          </Button>
          <Button
            size="small"
            aria-label={`Move override ${n} later`}
            title="Later: wins over the ones before it"
            disabled={busy || n === count}
            onClick={() => onMove(1)}
          >
            ↓
          </Button>
          <Button
            size="small"
            aria-label={`Edit override ${n}`}
            disabled={busy}
            onClick={onEdit}
          >
            Edit
          </Button>
          <Button
            size="small"
            aria-label={`Remove override ${n}`}
            disabled={busy}
            onClick={onRemove}
          >
            Remove
          </Button>
        </div>
      </header>

      <dl className="override-facts">
        <div>
          <dt>Start from</dt>
          <dd>{formatDay(override.startFrom)}</dd>
        </div>
        {override.shape !== null && (
          <div>
            <dt>Shape</dt>
            <dd>
              {override.shape.length > 0
                ? describeShape(override.shape)
                : "asks for nobody"}
            </dd>
          </div>
        )}
        {override.pins.length > 0 && (
          <div>
            <dt>Pins</dt>
            <dd className="override-pins">
              {override.pins.map((pin) => (
                <PinChip
                  key={pin.id}
                  name={pin.name}
                  role={pin.role}
                  custom={!pin.volunteerId}
                  colour={roles.find((r) => r.id === pin.roleId)?.colour}
                />
              ))}
            </dd>
          </div>
        )}
      </dl>
    </article>
  );
}

// What is being edited: nothing, a new Override, or one that exists.
type Editing = { id: string | null } | null;

// OverridesCard is the Rota Defaults' Overrides: rules that pick out some of
// the Shifts the pattern generates and change them as a rota is defined — a
// whole Shape in place of the default, and Pins (ADR 0013). They are a list,
// and a later one wins, so order is part of what an Organiser states.
//
// Like RotaDefaultsCard it is read on the settings screen and under the define
// form, and onSaved is for the define form to plan again.
export default function OverridesCard({
  onSaved,
}: {
  onSaved?: () => void;
} = {}) {
  const { overrides, error, add, update, remove, move } = useOverrides();
  const { roles } = useRoles();
  const { volunteers, error: volunteersError } = useVolunteers();
  const [editing, setEditing] = useState<Editing>(null);
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  // Nothing to pin anybody into, or ask for, until Roles exist.
  const canAdd = roles !== null && roles.length > 0;
  const count = overrides?.length ?? 0;

  async function act(write: () => Promise<void>, fallback: string) {
    setBusy(true);
    setActionError(null);
    try {
      await write();
      onSaved?.();
    } catch (err: unknown) {
      setActionError(err instanceof Error ? err.message : fallback);
    } finally {
      setBusy(false);
    }
  }

  async function save(id: string | null, draft: OverrideDraft) {
    await (id ? update(id, draft) : add(draft));
    setEditing(null);
    onSaved?.();
  }

  return (
    <SettingsSection
      title="Overrides"
      blurb="Rules that change some of the shifts your pattern makes, each time a rota is defined. When two change the same shift, the later one wins."
      action={
        canAdd &&
        editing === null && (
          <Button size="small" onClick={() => setEditing({ id: null })}>
            New override
          </Button>
        )
      }
    >
      {error && (
        <p className="settings-error">Could not load the overrides: {error}</p>
      )}
      {actionError && <p className="settings-error">{actionError}</p>}

      {overrides === null && !error && (
        <p className="settings-empty">Loading…</p>
      )}

      {overrides !== null && count === 0 && editing === null && (
        <p className="settings-empty">
          Nothing is overridden: every shift takes the default shape, and nobody
          is pinned until you pin them on the rota.
        </p>
      )}

      {overrides !== null && roles !== null && (count > 0 || editing) && (
        <ol className="overrides">
          {overrides.map((override) => (
            <li key={override.id}>
              {editing?.id === override.id ? (
                <OverrideEditor
                  number={override.number}
                  override={override}
                  roles={roles}
                  volunteers={volunteers}
                  volunteersError={volunteersError}
                  onSave={(draft) => save(override.id, draft)}
                  onCancel={() => setEditing(null)}
                />
              ) : (
                <OverrideView
                  override={override}
                  count={count}
                  roles={roles}
                  busy={busy || editing !== null}
                  onEdit={() => setEditing({ id: override.id })}
                  onMove={(by) =>
                    void act(
                      () => move(override.id, by),
                      "Failed to move the override",
                    )
                  }
                  onRemove={() =>
                    void act(
                      () => remove(override.id),
                      "Failed to remove the override",
                    )
                  }
                />
              )}
            </li>
          ))}
          {editing?.id === null && (
            <li>
              <OverrideEditor
                number={count + 1}
                override={null}
                roles={roles}
                volunteers={volunteers}
                volunteersError={volunteersError}
                onSave={(draft) => save(null, draft)}
                onCancel={() => setEditing(null)}
              />
            </li>
          )}
        </ol>
      )}

      {count > 0 && (
        <p className="settings-caption">
          Changing an override changes what the next rota starts from. The rotas
          already defined keep what they were given.
        </p>
      )}
    </SettingsSection>
  );
}
