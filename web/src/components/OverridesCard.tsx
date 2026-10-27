import { useLayoutEffect, useRef, useState, type PointerEvent } from "react";
import Button from "../ui/Button";
import Chip from "../ui/Chip";
import { useOverrideRule } from "../hooks/useOverrideRule";
import { useOverrides } from "../hooks/useOverrides";
import { useRoles } from "../hooks/useRoles";
import { useVolunteers } from "../hooks/useVolunteers";
import SettingsSection from "./SettingsSection";
import ShapeSeats from "./ShapeSeats";
import {
  formatDay,
  formatLength,
  isDate,
  lengthChoices,
  today,
} from "./patternDates";
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
  const [startOn, setStartOn] = useState(override?.startTime != null);
  const [startTime, setStartTime] = useState(override?.startTime ?? "");
  const [lengthOn, setLengthOn] = useState(override?.lengthMinutes != null);
  // Two hours until somebody picks one, the commonest session.
  const [length, setLength] = useState(override?.lengthMinutes ?? 120);
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
  // A start time switched on says when; the time box is empty until then.
  const ready = readable && (!startOn || startTime !== "");

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
        startTime: startOn ? startTime : null,
        lengthMinutes: lengthOn ? length : null,
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
        that time. Without them, it changes every shift on those days. A start
        time moves a shift within its own day, and the shift runs on from there.
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
            checked={startOn}
            onChange={(e) => setStartOn(e.target.checked)}
          />{" "}
          Start time
        </label>
        <div>
          {startOn ? (
            <input
              type="time"
              aria-label={`Override ${number} start time`}
              step={300}
              required
              value={startTime}
              onChange={(e) => setStartTime(e.target.value)}
            />
          ) : (
            <span className="override-off">as the pattern generates it</span>
          )}
        </div>

        <label>
          <input
            type="checkbox"
            checked={lengthOn}
            onChange={(e) => setLengthOn(e.target.checked)}
          />{" "}
          Length
        </label>
        <div>
          {lengthOn ? (
            <select
              aria-label={`Override ${number} length`}
              value={length}
              onChange={(e) => setLength(Number(e.target.value))}
            >
              {lengthChoices(length).map((m) => (
                <option key={m} value={m}>
                  {formatLength(m)}
                </option>
              ))}
            </select>
          ) : (
            <span className="override-off">the pattern's length</span>
          )}
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
        <Button type="submit" disabled={!ready || saving}>
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
  dragging,
  onEdit,
  onGrab,
  onMove,
  onRemove,
}: {
  override: Override;
  count: number;
  roles: ConfiguredRole[];
  busy: boolean;
  dragging: boolean;
  onEdit: () => void;
  onGrab: (e: PointerEvent<HTMLButtonElement>) => void;
  onMove: (by: -1 | 1) => void;
  onRemove: () => void;
}) {
  const n = override.number;
  return (
    <article
      className={`override${dragging ? " override--dragging" : ""}`}
      aria-label={`Override ${n}`}
    >
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
        {/* Order is what decides between two Overrides, so it is moved by
            hand: dragged with a mouse or a finger, or a place at a time with
            the arrow keys once the handle has focus. */}
        <button
          type="button"
          className="override-handle"
          aria-label={`Reorder override ${n}`}
          title="Drag to reorder: a later override wins. Arrow keys move it a place."
          // Not disabled while busy, only marked so: a disabled button drops
          // its focus, and a keyboard user moving one several places would
          // lose the handle after the first.
          disabled={count < 2}
          aria-disabled={busy || undefined}
          onPointerDown={(e) => {
            if (!busy) onGrab(e);
          }}
          onKeyDown={(e) => {
            const by = e.key === "ArrowUp" ? -1 : e.key === "ArrowDown" ? 1 : 0;
            if (by === 0) return;
            e.preventDefault();
            if (!busy && n + by >= 1 && n + by <= count) onMove(by);
          }}
        >
          <svg viewBox="0 0 10 16" width="10" height="16" aria-hidden="true">
            {[3, 8, 13].map((y) =>
              [2.5, 7.5].map((x) => (
                <circle key={`${x}-${y}`} cx={x} cy={y} r="1.5" />
              )),
            )}
          </svg>
        </button>
      </header>

      <dl className="override-facts">
        <div>
          <dt>Start from</dt>
          <dd>{formatDay(override.startFrom)}</dd>
        </div>
        {override.startTime !== null && (
          <div>
            <dt>Start time</dt>
            <dd>{override.startTime}</dd>
          </div>
        )}
        {override.lengthMinutes !== null && (
          <div>
            <dt>Length</dt>
            <dd>{formatLength(override.lengthMinutes)}</dd>
          </div>
        )}
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

// Where an Override stood in the list when a drag began, in the list's own
// coordinates.
interface Slot {
  top: number;
  height: number;
}

// A drag in flight: which Override, the order it would drop in, where every
// Override stood when it began, and how far the pointer has come since.
interface Drag {
  id: string;
  order: string[];
  slots: Map<string, Slot>;
  from: number;
  dy: number;
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
  const { overrides, error, add, update, remove, reorder } = useOverrides();
  const { roles } = useRoles();
  const { volunteers, error: volunteersError } = useVolunteers();
  const [editing, setEditing] = useState<Editing>(null);
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  // A drag in flight. The list keeps its order while it lasts: the dragged
  // Override follows the pointer, by dy, and the others slide out of its way
  // to where the order it would drop in puts them. Only the drop writes, and
  // only then does the list take the new order.
  const [drag, setDrag] = useState<Drag | null>(null);
  // The list captures the pointer rather than the handle, so a drag survives
  // whatever happens to the handle's own item under it.
  const list = useRef<HTMLOListElement>(null);
  const items = useRef(new Map<string, HTMLLIElement>());
  // Where the dropped Override was on screen as it was let go, so it can
  // glide into its slot rather than leap there.
  const settling = useRef<{ id: string; top: number } | null>(null);

  // After a drop has been drawn: start the dropped Override where the pointer
  // left it and slide it home. Run in a layout effect so it never shows a
  // frame in its slot first.
  useLayoutEffect(() => {
    const dropped = settling.current;
    if (!dropped || drag) return;
    settling.current = null;
    const el = items.current.get(dropped.id);
    if (!el || typeof el.animate !== "function") return;
    if (window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) return;
    const dy = dropped.top - el.getBoundingClientRect().top;
    if (Math.abs(dy) < 1) return;
    el.animate([{ transform: `translateY(${dy}px)` }, { transform: "none" }], {
      duration: 150,
      easing: "ease",
    });
  }, [drag, overrides]);

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

  function writeOrder(order: string[]) {
    void act(() => reorder(order), "Failed to move the override");
  }

  function moveBy(id: string, by: -1 | 1) {
    if (!overrides) return;
    const order = overrides.map((o) => o.id);
    const from = order.indexOf(id);
    const to = from + by;
    if (from < 0 || to < 0 || to >= order.length) return;
    [order[from], order[to]] = [order[to], order[from]];
    writeOrder(order);
  }

  // The list's own coordinates, which the items' slides do not move.
  function pointerY(e: PointerEvent) {
    return e.clientY - (list.current?.getBoundingClientRect().top ?? 0);
  }

  function grab(id: string, e: PointerEvent<HTMLButtonElement>) {
    if (e.button !== 0 || !overrides || !list.current) return;
    // Otherwise a drag on a phone scrolls the page, and on a desk selects text.
    e.preventDefault();
    list.current.setPointerCapture(e.pointerId);
    const top = list.current.getBoundingClientRect().top;
    const slots = new Map<string, Slot>();
    for (const o of overrides) {
      const box = items.current.get(o.id)?.getBoundingClientRect();
      if (box) slots.set(o.id, { top: box.top - top, height: box.height });
    }
    const order = overrides.map((o) => o.id);
    setDrag({ id, order, slots, from: pointerY(e), dy: 0 });
  }

  // Where the dragged Override would land: after every other one whose middle
  // its own middle has passed. Both middles are where things stood when the
  // drag began, so the line it crosses going down is the line it crosses
  // coming back, and the slides cannot move it.
  function dragOver(e: PointerEvent<HTMLOListElement>) {
    if (!drag) return;
    const dy = pointerY(e) - drag.from;
    const held = drag.slots.get(drag.id);
    const middle = (held ? held.top + held.height / 2 : 0) + dy;
    const others = drag.order.filter((id) => id !== drag.id);
    const passed = others.filter((id) => {
      const slot = drag.slots.get(id);
      return slot !== undefined && slot.top + slot.height / 2 < middle;
    }).length;
    const order = [
      ...others.slice(0, passed),
      drag.id,
      ...others.slice(passed),
    ];
    setDrag({ ...drag, order, dy });
  }

  function drop() {
    if (!drag || !overrides) return;
    const held = items.current.get(drag.id);
    if (held) {
      settling.current = { id: drag.id, top: held.getBoundingClientRect().top };
    }
    setDrag(null);
    if (drag.order.some((id, i) => id !== overrides[i]?.id)) {
      writeOrder(drag.order);
    }
  }

  // Where each Override is drawn while a drag lasts: the held one wherever the
  // pointer has taken it, and the rest in the slots the order would give them,
  // stacked from the top with the gaps between them as they were.
  const slides = new Map<string, number>();
  if (drag && overrides) {
    const first = drag.slots.get(overrides[0]?.id ?? "");
    const second = drag.slots.get(overrides[1]?.id ?? "");
    const gap = first && second ? second.top - (first.top + first.height) : 0;
    let y = first?.top ?? 0;
    for (const id of drag.order) {
      const slot = drag.slots.get(id);
      if (!slot) continue;
      slides.set(id, id === drag.id ? drag.dy : y - slot.top);
      y += slot.height + gap;
    }
  }

  // Numbered as the drag would leave them, so the number on each is the one
  // the define preview would call it by.
  const shown =
    overrides && drag
      ? overrides.map((o) => ({ ...o, number: drag.order.indexOf(o.id) + 1 }))
      : overrides;

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
        <ol
          ref={list}
          className={`overrides${drag ? " overrides--dragging" : ""}`}
          onPointerMove={dragOver}
          onPointerUp={drop}
          onPointerCancel={() => setDrag(null)}
        >
          {(shown ?? []).map((override) => (
            <li
              key={override.id}
              ref={(el) => {
                if (el) items.current.set(override.id, el);
                else items.current.delete(override.id);
              }}
              className={drag?.id === override.id ? "override-held" : undefined}
              style={
                slides.has(override.id)
                  ? { transform: `translateY(${slides.get(override.id)}px)` }
                  : undefined
              }
            >
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
                  dragging={drag?.id === override.id}
                  onEdit={() => setEditing({ id: override.id })}
                  onGrab={(e) => grab(override.id, e)}
                  onMove={(by) => moveBy(override.id, by)}
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
