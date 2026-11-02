import {
  useEffect,
  useRef,
  useState,
  type PointerEvent,
  type ReactNode,
} from "react";
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
import type { EditorMode } from "./ModeSwitch";
import NaturalField from "./NaturalField";
import RoleSelector from "./RoleSelector";
import SaveStatus from "./SaveStatus";
import SettingsSection from "./SettingsSection";
import { settingsSummary } from "./settingsSummary";
import ShapeSeats from "./ShapeSeats";
import { formatLength, lengthChoices, today } from "./patternDates";
import { seatCountsOf, seatsOf, type SeatCounts } from "./shape";
import VolunteerPicker from "./VolunteerPicker";
import type {
  ConfiguredRole,
  NaturalEditor,
  Override,
  OverrideDraft,
  OverrideGranularity,
  OverrideRuleTranslation,
  PersonRef,
  ShapeSeat,
  Volunteer,
} from "../types";
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

// Which shifts on a day an Override changes, in the words the card uses.
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
// custom entry, as on the rota. The colour is how it says the Role, so the
// name stands alone on it; the Role is still spoken to a screen reader, and
// shown on hover.
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
    <span
      className="override-pin"
      data-role-colour={colour}
      role="group"
      aria-label={`${name}, ${role}`}
      title={role}
    >
      <Chip
        label={name}
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
  const [person, setPerson] = useState<PersonRef | null>(null);
  const [roleId, setRoleId] = useState("");

  const chosen =
    person && "volunteerId" in person
      ? (volunteers?.find((v) => v.id === person.volunteerId) ?? null)
      : null;
  // The Role they mostly do, until somebody picks one.
  const role =
    roles.find((r) => r.id === roleId) ??
    roles.find((r) => chosen?.roles.includes(r.name)) ??
    roles[0] ??
    null;
  const name =
    person === null
      ? ""
      : "custom" in person
        ? person.custom
        : (chosen?.name ?? "");
  const ready = role !== null && person !== null && name !== "";

  function add() {
    if (!ready) return;
    onAdd({ roleId: role.id, person, name });
    setPerson(null);
    setRoleId("");
  }

  return (
    <div className="override-adder">
      <VolunteerPicker
        label="Who to pin"
        volunteers={volunteers}
        volunteersError={volunteersError}
        value={person}
        onChange={setPerson}
      />
      {person !== null && role !== null && (
        <RoleSelector
          label="Role"
          showLabel={false}
          options={roles.map((r) => r.name)}
          value={role.name}
          onChange={(name) =>
            setRoleId(roles.find((r) => r.name === name)?.id ?? "")
          }
          colourOf={(name) =>
            roles.find((r) => r.name === name)?.colour ?? null
          }
        />
      )}
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

// What an Override can change on the shifts it picks out.
type Change = "start" | "length" | "shape" | "pin";

// ChangePopover is where one change is edited, under its chip. It takes the
// focus as it opens, and closes on Escape or on a press anywhere outside it
// and the chip it hangs from.
function ChangePopover({
  label,
  onClose,
  children,
}: {
  label: string;
  onClose: () => void;
  children: ReactNode;
}) {
  const box = useRef<HTMLDivElement>(null);
  const close = useRef(onClose);
  useEffect(() => {
    close.current = onClose;
  });
  useEffect(() => {
    box.current?.querySelector<HTMLElement>("input, select, button")?.focus();
    const away = (e: Event) => {
      if (!box.current?.parentElement?.contains(e.target as Node)) {
        close.current();
      }
    };
    document.addEventListener("pointerdown", away);
    return () => document.removeEventListener("pointerdown", away);
  }, []);
  return (
    <div
      ref={box}
      className="override-popover"
      role="dialog"
      aria-label={label}
      onKeyDown={(e) => {
        if (e.key !== "Escape") return;
        e.stopPropagation();
        box.current?.parentElement?.querySelector("button")?.focus();
        onClose();
      }}
    >
      {children}
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

// OverrideCard is one Override as one line, when → then: which shifts — the
// description, or the rule, which carries the date it counts from — then only
// what it changes on them, each a chip that opens to be edited. Changes save
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
  natural,
  translateRule,
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
  // Whether what it matches can be described in plain English, and why not.
  natural: NaturalEditor;
  translateRule: (text: string) => Promise<OverrideRuleTranslation>;
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

  // Natural wherever it is available, unless the Organiser picks raw; not
  // remembered. Availability can arrive after the card does, with the Rota
  // Defaults, so it is not frozen into the first render.
  const [chosen, setChosen] = useState<EditorMode | null>(null);
  const mode: EditorMode = natural.available ? (chosen ?? "natural") : "raw";
  // What a Generate said its rule means, shown until the reading of the rule
  // catches up, so the description does not blink.
  const [generated, setGenerated] = useState<string | null>(null);
  // A raw edit the server cannot read would be lost behind the saved rule's
  // description, so it has to be fixed or undone first.
  const rawStuck =
    mode === "raw" &&
    draft.rrule.trim() !== override.rrule.trim() &&
    problems.length > 0;
  const locked = !natural.available
    ? (natural.reason ?? "The natural editor is not available")
    : rawStuck
      ? "Fix or undo the raw edit first"
      : null;
  const description =
    draft.rrule.trim() === ""
      ? ""
      : current && reading
        ? reading.description
        : (generated ?? override.description);
  const granularity =
    current && reading ? reading.granularity : override.granularity;
  const switchMode = (next: EditorMode) => {
    setGenerated(null);
    setChosen(next);
  };

  // Which change is open for editing, or the menu of what else it could
  // change. One at a time, so the line never holds two popovers.
  const [open, setOpen] = useState<Change | "menu" | null>(null);
  const toggle = (c: Change | "menu") => setOpen((o) => (o === c ? null : c));
  const shut = () => setOpen(null);
  // Starting a change turns it on and opens it, so what it is set to is the
  // next thing asked.
  function begin(c: Change) {
    if (c === "start") change({ startOn: true });
    if (c === "length") change({ lengthOn: true });
    if (c === "shape") change({ shapeOn: true });
    setOpen(c);
  }
  function stop(c: Exclude<Change, "pin">) {
    if (c === "start") change({ startOn: false });
    if (c === "length") change({ lengthOn: false });
    if (c === "shape") change({ shapeOn: false });
    setOpen(null);
  }
  const unchanged: [Change, string][] = [
    ...(draft.startOn ? [] : [["start", "Start time"] as [Change, string]]),
    ...(draft.lengthOn ? [] : [["length", "Length"] as [Change, string]]),
    ...(draft.shapeOn ? [] : [["shape", "Shape"] as [Change, string]]),
    ["pin", "Pin somebody"],
  ];
  const seats = seatsOf(roles, draft.counts);
  const changesNothing =
    !draft.startOn &&
    !draft.lengthOn &&
    !draft.shapeOn &&
    draft.pins.length === 0;

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
      <span className="override-number" aria-hidden="true">
        {n}
      </span>

      {/* Which shifts: the description, said in words, or the rule itself. */}
      <div className="override-when">
        {mode === "natural" ? (
          <NaturalField
            descriptions={description ? [description] : []}
            label={`Override ${n} description`}
            placeholder="Describe which shifts it changes in plain English"
            translate={translateRule}
            onTranslated={({ rrule, description: said }) => {
              setGenerated(said);
              change({ rrule });
            }}
            rawLabel="Write the rule yourself"
            onUseRaw={() => setChosen("raw")}
          />
        ) : (
          <>
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
                <p className="override-problems">
                  Write a rule to pick out shifts.
                </p>
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
              A DTSTART;VALUE=DATE line says the date it counts from, and
              nothing before it is changed; an RRULE line says which dates. With
              BYHOUR and BYMINUTE it changes only the shift the pattern starts
              at that time; without them, every shift on those days.
            </p>
          </>
        )}
        <div className="override-when-meta">
          {mode === "natural" && granularity && (
            <GranularityBadge granularity={granularity} />
          )}
          {mode === "natural" ? (
            <button
              type="button"
              className="override-mode"
              onClick={() => switchMode("raw")}
            >
              Edit the rule
            </button>
          ) : (
            // The reason is on the wrapper, because a disabled button shows
            // no title in every browser.
            <span title={locked ?? undefined}>
              <button
                type="button"
                className="override-mode"
                disabled={locked !== null}
                onClick={() => switchMode("natural")}
              >
                Describe it instead
              </button>
            </span>
          )}
        </div>
      </div>

      <svg className="override-arrow" viewBox="0 0 24 24" aria-hidden="true">
        <path d="M4 12h15M14 7l5 5-5 5" />
      </svg>

      {/* What changes on them: only what it changes, each a chip that opens
          to be edited. */}
      <div
        className="override-then"
        role="group"
        aria-label={`What override ${n} changes`}
      >
        {changesNothing && (
          <span className="override-none">Changes nothing yet</span>
        )}
        {draft.startOn && (
          <span className="override-change">
            <button
              type="button"
              className={`override-chip${draft.startTime ? "" : " override-chip--pending"}`}
              aria-expanded={open === "start"}
              disabled={!editable}
              onClick={() => toggle("start")}
            >
              {draft.startTime ? `Starts ${draft.startTime}` : "Start time?"}
            </button>
            {open === "start" && (
              <ChangePopover label={`Override ${n} start`} onClose={shut}>
                <input
                  type="time"
                  aria-label={`Override ${n} start time`}
                  step={300}
                  required
                  value={draft.startTime}
                  onChange={(e) => change({ startTime: e.target.value })}
                />
                <Button size="small" onClick={() => stop("start")}>
                  Keep the pattern's start
                </Button>
              </ChangePopover>
            )}
          </span>
        )}
        {draft.lengthOn && (
          <span className="override-change">
            <button
              type="button"
              className="override-chip"
              aria-expanded={open === "length"}
              disabled={!editable}
              onClick={() => toggle("length")}
            >
              {formatLength(draft.length)}
            </button>
            {open === "length" && (
              <ChangePopover label={`Override ${n} length`} onClose={shut}>
                <select
                  aria-label={`Override ${n} length`}
                  value={draft.length}
                  onChange={(e) => change({ length: Number(e.target.value) })}
                >
                  {lengthChoices(draft.length).map((m) => (
                    <option key={m} value={m}>
                      {formatLength(m)}
                    </option>
                  ))}
                </select>
                <Button size="small" onClick={() => stop("length")}>
                  Keep the pattern's length
                </Button>
              </ChangePopover>
            )}
          </span>
        )}
        {draft.shapeOn && (
          <span className="override-change">
            <button
              type="button"
              className="override-chip"
              aria-label={`Shape: ${
                seats
                  .map((s) => `${s.count} ${roleOf(s.roleId)?.name ?? ""}`)
                  .join(", ") || "nobody"
              }`}
              aria-expanded={open === "shape"}
              disabled={!editable}
              onClick={() => toggle("shape")}
            >
              {seats.length === 0
                ? "Nobody"
                : seats.map((s) => (
                    <span
                      key={s.roleId}
                      className="override-seat"
                      data-role-colour={roleOf(s.roleId)?.colour}
                      title={roleOf(s.roleId)?.name}
                    >
                      {s.count}
                    </span>
                  ))}
            </button>
            {open === "shape" && (
              <ChangePopover label={`Override ${n} shape`} onClose={shut}>
                <ShapeSeats
                  roles={roles}
                  counts={draft.counts}
                  onChange={(counts) => change({ counts })}
                />
                <Button size="small" onClick={() => stop("shape")}>
                  Keep the default shape
                </Button>
              </ChangePopover>
            )}
          </span>
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
              onRemove={
                editable
                  ? () =>
                      change({
                        pins: draft.pins.filter((p) => p.key !== pin.key),
                      })
                  : undefined
              }
            />
          );
        })}
        <span className="override-change">
          <button
            type="button"
            className="override-add"
            aria-label={`Change something else on override ${n}`}
            title="Change something else"
            aria-expanded={open === "menu" || open === "pin"}
            disabled={!editable}
            onClick={() =>
              setOpen((o) => (o === "menu" || o === "pin" ? null : "menu"))
            }
          >
            +
          </button>
          {open === "menu" && (
            <ChangePopover
              label={`What else override ${n} changes`}
              onClose={shut}
            >
              {unchanged.map(([c, name]) => (
                <button
                  key={c}
                  type="button"
                  className="override-menu-item"
                  onClick={() => begin(c)}
                >
                  {name}
                </button>
              ))}
            </ChangePopover>
          )}
          {open === "pin" && (
            <ChangePopover
              label={`Pin somebody on override ${n}`}
              onClose={shut}
            >
              <PinAdder
                roles={roles}
                volunteers={volunteers}
                volunteersError={volunteersError}
                onAdd={(pin) => change({ pins: withPin(draft.pins, pin) })}
              />
            </ChangePopover>
          )}
        </span>
      </div>

      <div className="override-side">
        <SaveStatus state={saveState} />
        {!asking && (
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
      </div>

      {asking && (
        <p className="override-confirm override-wide">
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
        </p>
      )}
      {error && <p className="settings-error override-wide">{error}</p>}
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
  const { overrides, error, add, update, remove, reorder, translateRule } =
    useOverrides();
  const { roles } = useRoles();
  const { defaults } = useRotaDefaults();
  const { volunteers, error: volunteersError } = useVolunteers();
  // Until the Rota Defaults load there is no knowing; raw is the safe guess,
  // and the cards move to natural once they say so.
  const natural = defaults?.overrideNaturalEditor ?? {
    available: false,
    reason: "Loading…",
  };
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
      blurb="Rules that change some of the shifts your pattern makes, each time a rota is defined: which shifts, then what is different about them. When two change the same shift, the later one wins."
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
                  natural={natural}
                  translateRule={translateRule}
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
