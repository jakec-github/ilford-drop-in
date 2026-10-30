import { useEffect, useRef, useState, type PointerEvent } from "react";
import Button from "../ui/Button";
import Chip from "../ui/Chip";
import DragHandle from "../ui/DragHandle";
import IconButton from "../ui/IconButton";
import { useDragReorder } from "../ui/useDragReorder";
import { useDraft, useSaveWhenSettled } from "../hooks/useAutosave";
import { useOverrideRule } from "../hooks/useOverrideRule";
import { useOverrides } from "../hooks/useOverrides";
import { useRoles } from "../hooks/useRoles";
import { useRotaDefaults } from "../hooks/useRotaDefaults";
import { useVolunteers } from "../hooks/useVolunteers";
import SaveStatus from "./SaveStatus";
import SettingsSection from "./SettingsSection";
import { settingsSummary } from "./settingsSummary";
import ShapeSeats from "./ShapeSeats";
import { formatLength, lengthChoices, today } from "./patternDates";
import { seatCountsOf, seatsOf, type SeatCounts } from "./shape";
import type {
  ConfiguredRole,
  Override,
  OverrideDraft,
  OverrideGranularity,
  PersonRef,
  ShapeSeat,
  Volunteer,
} from "../types";
import { CUSTOM_CHOICE } from "../types";
import "./OverridesCard.css";

// What a new Override starts as: the commonest thing an Organiser overrides,
// counted from today, a working example of the syntax to edit from, and
// nothing changed yet — so it can be added at once, and the card it becomes is
// where it is stated.
function newOverride(): OverrideDraft {
  // An Override's rule counts from its own DTSTART, a date (ADR 0014).
  const from = today().replaceAll("-", "");
  return {
    rrule: `DTSTART;VALUE=DATE:${from}\nRRULE:FREQ=MONTHLY;BYDAY=1SU`,
    startTime: null,
    lengthMinutes: null,
    shape: null,
    pins: [],
  };
}

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

// OverrideEdit is one Override as its card holds it. A part that is switched
// off keeps what it was set to, so switching it back on does not lose it; only
// what the switches leave on is sent.
interface OverrideEdit {
  rrule: string;
  startOn: boolean;
  startTime: string;
  lengthOn: boolean;
  length: number;
  shapeOn: boolean;
  counts: SeatCounts;
  pins: PinDraft[];
}

// Two hours until somebody picks one, the commonest session.
const DEFAULT_LENGTH = 120;

function editOf(
  override: Override,
  roles: ConfiguredRole[],
  defaultShape: ShapeSeat[],
): OverrideEdit {
  return {
    rrule: override.rrule,
    startOn: override.startTime !== null,
    startTime: override.startTime ?? "",
    lengthOn: override.lengthMinutes !== null,
    length: override.lengthMinutes ?? DEFAULT_LENGTH,
    shapeOn: override.shape !== null,
    // Ticking "Replace the shape" starts from the default, which is what the
    // shift asked for until then, rather than from nobody.
    counts: seatCountsOf(roles, override.shape ?? defaultShape),
    pins: override.pins.map((pin) => ({
      key: pinKey(),
      roleId: pin.roleId,
      person: pin.volunteerId
        ? { volunteerId: pin.volunteerId }
        : { custom: pin.custom ?? pin.name },
      name: pin.name,
    })),
  };
}

function draftOf(edit: OverrideEdit, roles: ConfiguredRole[]): OverrideDraft {
  return {
    rrule: edit.rrule.trim(),
    startTime: edit.startOn ? edit.startTime : null,
    lengthMinutes: edit.lengthOn ? edit.length : null,
    shape: edit.shapeOn ? seatsOf(roles, edit.counts) : null,
    pins: edit.pins.map((p) => ({ roleId: p.roleId, person: p.person })),
  };
}

// One person once on an Override: pinning them again changes their Role,
// since a person fills one Seat on a Shift. Custom entries may repeat.
function withPin(pins: PinDraft[], pin: Omit<PinDraft, "key">): PinDraft[] {
  const id = "volunteerId" in pin.person ? pin.person.volunteerId : null;
  const kept = id
    ? pins.filter(
        (p) => !("volunteerId" in p.person && p.person.volunteerId === id),
      )
    : pins;
  return [...kept, { ...pin, key: pinKey() }];
}

// OverrideCard is one Override, edited where it stands, in two halves an
// Organiser can tell apart at a glance: what it matches — the rule, which
// carries the date it counts from — and what it sets on the shifts it matches. Changes save
// themselves once they stop moving; removing asks first, on the card.
export function OverrideCard({
  override,
  number,
  count,
  roles,
  defaultShape,
  volunteers,
  volunteersError,
  fresh,
  busy,
  held,
  onSave,
  onRemove,
  onGrab,
  onMove,
}: {
  override: Override;
  number: number;
  count: number;
  roles: ConfiguredRole[];
  defaultShape: ShapeSeat[];
  volunteers: Volunteer[] | null;
  volunteersError: string | null;
  // Just added: the card takes focus and comes into view.
  fresh: boolean;
  // Something else is being written: the handle waits for it.
  busy: boolean;
  held: boolean;
  onSave: (draft: OverrideDraft) => Promise<void>;
  onRemove: () => Promise<void>;
  onGrab: (e: PointerEvent<HTMLButtonElement>) => void;
  onMove: (by: -1 | 1) => void;
}) {
  const same = (a: OverrideEdit, b: OverrideEdit) =>
    JSON.stringify(draftOf(a, roles)) === JSON.stringify(draftOf(b, roles));
  const { draft, setDraft, saved } = useDraft({
    source: override,
    toDraft: (o: Override) => editOf(o, roles, defaultShape),
    same,
  });

  // The rule is read as it is typed, and the card saves only once the reading
  // is of what is in the box and found nothing wrong with it.
  const {
    reading,
    current,
    error: readingError,
  } = useOverrideRule(draft.rrule);
  const problems = reading?.problems ?? [];
  const { saveState, error, setError } = useSaveWhenSettled({
    draft,
    saved,
    same,
    save: (edit) => onSave(draftOf(edit, roles)),
    ready:
      current &&
      problems.length === 0 &&
      (!draft.startOn || draft.startTime !== ""),
  });

  const [asking, setAsking] = useState(false);
  const [removing, setRemoving] = useState(false);
  const card = useRef<HTMLElement>(null);
  const ruleBox = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    if (!fresh) return;
    card.current?.scrollIntoView?.({ block: "nearest", behavior: "smooth" });
    ruleBox.current?.focus();
  }, [fresh]);

  const change = (part: Partial<OverrideEdit>) =>
    setDraft((d) => ({ ...d, ...part }));
  const roleOf = (id: string) => roles.find((r) => r.id === id);
  const n = number;
  const editable = !removing;

  function remove() {
    setRemoving(true);
    setError(null);
    onRemove().catch((err: unknown) => {
      setRemoving(false);
      setAsking(false);
      setError(
        err instanceof Error ? err.message : "The override was not removed",
      );
    });
  }

  return (
    <article
      ref={card}
      className={`override${held ? " override--dragging" : ""}${fresh ? " override--fresh" : ""}`}
      aria-label={`Override ${n}`}
    >
      <header className="override-head">
        <span className="override-number" aria-hidden="true">
          {n}
        </span>
        <span className="override-title">Override {n}</span>
        <SaveStatus state={saveState} />
        {asking ? (
          <span className="override-confirm">
            Remove this override?
            <Button size="small" onClick={() => setAsking(false)}>
              Keep
            </Button>
            <Button
              size="small"
              className="override-confirm-remove"
              disabled={removing}
              onClick={remove}
            >
              {removing ? "Removing…" : "Remove"}
            </Button>
          </span>
        ) : (
          <button
            type="button"
            className="override-remove"
            aria-label={`Remove override ${n}`}
            title="Remove"
            disabled={removing}
            onClick={() => setAsking(true)}
          >
            <svg viewBox="0 0 20 20" aria-hidden="true">
              <circle cx="10" cy="10" r="8" />
              <path d="M6 10h8" />
            </svg>
          </button>
        )}
        {/* Order is what decides between two Overrides, so it is moved by
            hand. */}
        <DragHandle
          label={`Reorder override ${n}`}
          title="Drag to reorder: a later override wins. Arrow keys move it a place."
          place={n - 1}
          count={count}
          busy={busy}
          onGrab={onGrab}
          onMove={onMove}
        />
      </header>

      <section
        className="override-part override-part--match"
        aria-label={`Override ${n} matches`}
      >
        <h3 className="override-part-title">Matches</h3>
        <textarea
          ref={ruleBox}
          className="override-rule"
          rows={Math.max(2, draft.rrule.split("\n").length)}
          aria-label={`Override ${n} rule`}
          spellCheck={false}
          value={draft.rrule}
          disabled={!editable}
          onChange={(e) => change({ rrule: e.target.value })}
        />
        <div className="override-reading" aria-live="polite">
          {readingError && <p className="settings-error">{readingError}</p>}
          {draft.rrule.trim() === "" ? (
            <p className="override-problems">Write a rule to match shifts.</p>
          ) : problems.length > 0 ? (
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
          A DTSTART;VALUE=DATE line says the date it counts from, and nothing
          before it is matched; an RRULE line says which dates. With BYHOUR and
          BYMINUTE it matches only the shift the pattern starts at that time;
          without them, every shift on those days.
        </p>
      </section>

      <section
        className="override-part override-part--set"
        aria-label={`Override ${n} sets`}
      >
        <h3 className="override-part-title">Sets</h3>
        <div className="override-props">
          <label>
            <input
              type="checkbox"
              checked={draft.startOn}
              disabled={!editable}
              onChange={(e) => change({ startOn: e.target.checked })}
            />{" "}
            Start time
          </label>
          <div>
            {draft.startOn ? (
              <input
                type="time"
                aria-label={`Override ${n} start time`}
                step={300}
                required
                value={draft.startTime}
                disabled={!editable}
                onChange={(e) => change({ startTime: e.target.value })}
              />
            ) : (
              <span className="override-off">as the pattern generates it</span>
            )}
          </div>

          <label>
            <input
              type="checkbox"
              checked={draft.lengthOn}
              disabled={!editable}
              onChange={(e) => change({ lengthOn: e.target.checked })}
            />{" "}
            Length
          </label>
          <div>
            {draft.lengthOn ? (
              <select
                aria-label={`Override ${n} length`}
                value={draft.length}
                disabled={!editable}
                onChange={(e) => change({ length: Number(e.target.value) })}
              >
                {lengthChoices(draft.length).map((m) => (
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
              checked={draft.shapeOn}
              disabled={!editable}
              onChange={(e) => change({ shapeOn: e.target.checked })}
            />{" "}
            Shape
          </label>
          <div>
            {draft.shapeOn ? (
              <div className="override-shape">
                <ShapeSeats
                  roles={roles}
                  counts={draft.counts}
                  onChange={(counts) => change({ counts })}
                />
              </div>
            ) : (
              <span className="override-off">the default shape</span>
            )}
          </div>

          <span className="override-label">Pins</span>
          <div>
            <div className="override-pins">
              {draft.pins.length === 0 && (
                <span className="override-off">none</span>
              )}
              {draft.pins.map((pin) => {
                const role = roleOf(pin.roleId);
                return (
                  <PinChip
                    key={pin.key}
                    name={pin.name}
                    role={role?.name ?? "unknown role"}
                    custom={"custom" in pin.person}
                    colour={role?.colour}
                    onRemove={() =>
                      change({
                        pins: draft.pins.filter((p) => p.key !== pin.key),
                      })
                    }
                  />
                );
              })}
            </div>
            <PinAdder
              roles={roles}
              volunteers={volunteers}
              volunteersError={volunteersError}
              onAdd={(pin) => change({ pins: withPin(draft.pins, pin) })}
            />
          </div>
        </div>
      </section>

      {error && <p className="settings-error">{error}</p>}
    </article>
  );
}

// OverridesCard is the Rota Defaults' Overrides: rules that pick out some of
// the Shifts the pattern generates and change them as a rota is defined — the
// start, the length, a whole Shape in place of the default, and Pins (ADR
// 0013). They are a list, and a later one wins, so order is part of what an
// Organiser states.
//
// Like RotaDefaultsCard it is read on Config and under the define form, and
// onSaved is for the define form to plan again.
export default function OverridesCard({
  onSaved,
  collapsible = false,
}: {
  onSaved?: () => void;
  // Closed until opened, as on Config; the define form shows it open.
  collapsible?: boolean;
} = {}) {
  const { overrides, error, add, update, remove, reorder } = useOverrides();
  const { roles } = useRoles();
  const { defaults } = useRotaDefaults();
  const { volunteers, error: volunteersError } = useVolunteers();
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  // The id of an Override just added, known only from the list that comes
  // back, as the one that was not there before.
  const [freshId, setFreshId] = useState<string | null>(null);
  const before = useRef<Set<string> | null>(null);
  useEffect(() => {
    if (!before.current || !overrides) return;
    const added = overrides.find((o) => !before.current?.has(o.id));
    before.current = null;
    if (added) setFreshId(added.id);
  }, [overrides]);

  const ids = overrides?.map((o) => o.id) ?? null;
  const drag = useDragReorder(ids, (order) => {
    void act(() => reorder(order), "Failed to move the override");
  });

  // Nothing to pin anybody into, or ask for, until Roles exist.
  const canAdd = roles !== null && roles.length > 0 && overrides !== null;
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

  function addOne() {
    before.current = new Set(ids ?? []);
    void act(() => add(newOverride()), "Failed to add an override");
  }

  async function save(id: string, draft: OverrideDraft) {
    await update(id, draft);
    onSaved?.();
  }

  return (
    <SettingsSection
      title="Overrides"
      collapsible={collapsible}
      summary={settingsSummary(overrides?.length, "override", "overrides")}
      blurb="Rules that change some of the shifts your pattern makes, each time a rota is defined. Each matches some shifts, then sets what is different about them. When two change the same shift, the later one wins."
      action={
        canAdd && (
          <IconButton
            icon="plus"
            variant="filled"
            aria-label="New override"
            disabled={busy}
            onClick={addOne}
          />
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

      {overrides !== null && count === 0 && (
        <p className="settings-empty">
          Nothing is overridden: every shift takes the default shape, and nobody
          is pinned until you pin them on the rota.
        </p>
      )}

      {overrides !== null && roles !== null && count > 0 && (
        <ol {...drag.listProps}>
          {overrides.map((override) => {
            const place = drag.placeOf(override.id);
            return (
              <li key={override.id} {...drag.itemProps(override.id)}>
                <OverrideCard
                  override={override}
                  // Numbered as a drag would leave them, so the number on
                  // each is the one the define preview would call it by.
                  number={place + 1}
                  count={count}
                  roles={roles}
                  defaultShape={defaults?.defaultShape ?? []}
                  volunteers={volunteers}
                  volunteersError={volunteersError}
                  fresh={override.id === freshId}
                  busy={busy}
                  held={drag.held === override.id}
                  onSave={(draft) => save(override.id, draft)}
                  onRemove={() => remove(override.id).then(() => onSaved?.())}
                  onGrab={(e) => drag.grab(override.id, e)}
                  onMove={(by) => drag.moveBy(override.id, by)}
                />
              </li>
            );
          })}
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
