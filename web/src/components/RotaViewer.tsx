import { useMemo, useState, useRef, useEffect, useCallback } from "react";
import type {
  Assignee,
  PersonRef,
  RotaChange,
  RotaShift,
  Volunteer,
} from "../types";
import { useVolunteers } from "../hooks/useVolunteers";
import Button from "../ui/Button";
import { AddAssigneeDialog, ConfirmChangeDialog } from "./RotaEditDialogs";
import "./RotaViewer.css";

interface RotaViewerProps {
  rotaShifts: RotaShift[];
  // Admins additionally see shifts whose rota has not been allocated yet, and
  // can turn on editing.
  isAdmin: boolean;
  // Records one change to the rota and reloads it. Rejects with the server's
  // own message when the change is refused. Only ever called by the editing
  // affordances, which are unreachable unless isAdmin.
  onChange: (change: RotaChange) => Promise<void>;
}

// A shift that exists but has not been through allocation yet: no assignees,
// and not deliberately closed. Hidden from the public; flagged for admins.
function isUnallocated(shift: RotaShift): boolean {
  return !shift.allocated && !shift.closed;
}

// Whether a shift can be edited at all. A closed shift has nobody on it by
// definition, and an unallocated one has not been through allocation, so
// neither is somewhere a person can be put or taken from.
function isEditable(shift: RotaShift): boolean {
  return !shift.closed && shift.allocated;
}

// How the alterations API names one assignee: real volunteers by id, custom
// (manual) entries by their text, which is all they have.
function personRef(assignee: Assignee): PersonRef {
  return assignee.volunteerId
    ? { volunteerId: assignee.volunteerId }
    : { custom: assignee.name };
}

// Identifies one chip in the whole list, for "which chip's menu is open". The
// index is part of it because the same custom entry can legitimately appear
// twice on a shift — two people from the same visiting group.
function chipKey(shiftDate: string, assignee: Assignee, index: number): string {
  return `${shiftDate}/${assignee.volunteerId ?? assignee.name}/${index}`;
}

function samePerson(a: PersonRef, b: PersonRef): boolean {
  return "volunteerId" in a && "volunteerId" in b
    ? a.volunteerId === b.volunteerId
    : "custom" in a && "custom" in b && a.custom === b.custom;
}

// "2 Feb" — weekday and year are redundant on the rota.
function formatShiftDate(dateStr: string): string {
  const date = new Date(dateStr);
  return date.toLocaleDateString("en-GB", { day: "numeric", month: "short" });
}

// "Sun 2 Feb" — used where a date is read out of the list's context, in a
// dialog or a screen-reader label, and the weekday stops "2 Feb" reading as a
// date the admin has to look up.
function formatShiftDateLong(dateStr: string): string {
  return new Date(dateStr).toLocaleDateString("en-GB", {
    weekday: "short",
    day: "numeric",
    month: "short",
  });
}

// Group membership is shown by a corner dot; the colour just needs to be stable
// per group and tell two groups apart, so we hash the group key into this list.
// The eight are mid-toned (legible on both themes) and spread so the closest
// pair still sits ~32 ΔE apart — no two land as look-alikes.
const GROUP_COLOURS = [
  "#d6455a",
  "#e2711d",
  "#c7a92b",
  "#4c9a2a",
  "#1ca3a3",
  "#3d6fd6",
  "#8b52d6",
  "#c94fa0",
];

function groupColour(key: string): string {
  let hash = 0;
  for (let i = 0; i < key.length; i++) {
    hash = (hash * 31 + key.charCodeAt(i)) >>> 0;
  }
  return GROUP_COLOURS[hash % GROUP_COLOURS.length];
}

function getAllNames(shifts: RotaShift[]): string[] {
  const names = new Set<string>();
  for (const shift of shifts) {
    for (const a of shift.assignees) names.add(a.name);
  }
  return Array.from(names).sort();
}

function getUpcomingShifts(shifts: RotaShift[], name: string): RotaShift[] {
  if (!name) return [];
  return shifts.filter((shift) => shift.assignees.some((a) => a.name === name));
}

// The id of the real volunteer behind a selected name, or null when the name
// belongs only to custom (manual) entries. Custom entries have no calendar
// feed, so they get no copy button.
function getVolunteerId(shifts: RotaShift[], name: string): string | null {
  if (!name) return null;
  for (const shift of shifts) {
    for (const a of shift.assignees) {
      if (a.name === name && a.volunteerId) return a.volunteerId;
    }
  }
  return null;
}

// Copies the volunteer's ICS subscription URL to the clipboard. One link
// covers all their shifts and stays in sync as the rota changes; pasting it
// into Google/Apple Calendar subscribes them.
function CalendarCopyButton({ volunteerId }: { volunteerId: string }) {
  const [copied, setCopied] = useState(false);
  const [failed, setFailed] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);

  useEffect(() => () => clearTimeout(timer.current), []);

  async function handleCopy() {
    const url = `${window.location.origin}/calendars/${volunteerId}.ics`;
    clearTimeout(timer.current);
    try {
      await navigator.clipboard.writeText(url);
      setCopied(true);
      setFailed(false);
    } catch {
      setCopied(false);
      setFailed(true);
    }
    timer.current = setTimeout(() => {
      setCopied(false);
      setFailed(false);
    }, 2000);
  }

  return (
    <button
      type="button"
      className={`calendar-copy${copied ? " copied" : ""}${failed ? " failed" : ""}`}
      onClick={handleCopy}
      aria-label="Copy calendar subscription link"
      title="Copy calendar subscription link"
    >
      <svg viewBox="0 0 20 20" width="15" height="15" aria-hidden="true">
        <rect
          x="3"
          y="4"
          width="14"
          height="13"
          rx="2"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.6"
        />
        <path
          d="M3 8h14M7 2.5v3M13 2.5v3"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.6"
          strokeLinecap="round"
        />
      </svg>
      <span className="calendar-copy-text">
        {copied ? "Copied!" : failed ? "Copy failed" : "Copy calendar link"}
      </span>
    </button>
  );
}

// Pending is the person the admin has picked up, on their way to another shift.
// The same state backs both routes to a move or a swap, so the drop handlers do
// not care which was used.
interface Pending {
  date: string;
  person: PersonRef;
  name: string;
  // True while an HTML5 drag is in flight. The tap route sets it false and the
  // row then grows an explicit "Move here" button; doing that mid-drag would
  // shift every row under the pointer just as it is aiming at one.
  dragging: boolean;
}

// RowEdit is one shift row's half of the editing state: what is in flight, and
// what this row can do about it. Absent when editing is off, which is how the
// row renders exactly as it did before the feature existed.
interface RowEdit {
  pending: Pending | null;
  // Key of the chip whose action menu is open, anywhere in the list, so opening
  // one closes the last.
  openMenu: string | null;
  // The server's message from a change that was refused against this shift.
  error: string | null;
  // Whether the person being carried could land on this shift at all. False
  // when they are already on it: nobody can be on one shift twice, so neither
  // a move nor a swap onto it is possible.
  canReceive: boolean;
  // Whether this shift's assignee could take the carried person's place. False
  // when they are already on the shift being carried from — the swap would put
  // them there twice.
  canSwapWith: (assignee: Assignee) => boolean;
  onOpenMenu: (key: string | null) => void;
  onRemove: (assignee: Assignee) => void;
  onPickUp: (assignee: Assignee) => void;
  onDragStart: (assignee: Assignee) => void;
  onDragEnd: () => void;
  onSwapWith: (assignee: Assignee) => void;
  onMoveHere: () => void;
  onAdd: () => void;
}

function Chip({
  assignee,
  selected,
  label,
  className,
  draggable,
  disabled,
  onClick,
  onDragStart,
  onDragEnd,
  onDrop,
}: {
  assignee: Assignee;
  selected: boolean;
  // Overrides the accessible name, which is otherwise the chip's text. Editing
  // turns a chip into a control that does something to the person named on it,
  // and "Bob" alone does not say what.
  label?: string;
  className?: string;
  draggable?: boolean;
  disabled?: boolean;
  onClick: () => void;
  onDragStart?: () => void;
  onDragEnd?: () => void;
  onDrop?: () => void;
}) {
  const cls = [
    "chip",
    `role-${assignee.role}`,
    assignee.custom ? "custom" : "volunteer",
    assignee.group ? "has-group" : "",
    selected ? "selected" : "",
    disabled ? "chip-inert" : "",
    className,
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <button
      type="button"
      className={cls}
      aria-label={label}
      draggable={draggable}
      // aria-disabled rather than disabled: the chip stays focusable, so
      // someone tabbing through the rota still hears why this one is not a
      // destination instead of skipping silently past it.
      aria-disabled={disabled}
      onClick={disabled ? undefined : onClick}
      onDragStart={(e) => {
        // Some form of payload is required for a drag to start at all in
        // Firefox; what is actually dragged is held in React state, since the
        // dragover handlers need to know it before any drop happens.
        e.dataTransfer.setData("text/plain", assignee.name);
        e.dataTransfer.effectAllowed = "move";
        onDragStart?.();
      }}
      onDragEnd={onDragEnd}
      onDragOver={onDrop && ((e) => e.preventDefault())}
      onDrop={
        onDrop &&
        ((e) => {
          // Without this the row underneath also takes the drop and reads it as
          // a move onto empty space.
          e.stopPropagation();
          e.preventDefault();
          onDrop();
        })
      }
    >
      {assignee.name}
      {assignee.group && (
        <span
          className="chip-group-dot"
          style={{ background: groupColour(assignee.group) }}
          title={`Group: ${assignee.group}`}
        />
      )}
    </button>
  );
}

// ChipMenu is what a chip offers when tapped in editing mode: the two things
// that can be done to one person on one shift. It is the touch and keyboard
// route to everything drag and drop offers — "move or swap" picks the person
// up, and the destination is then chosen from the rows themselves.
function ChipMenu({
  name,
  onRemove,
  onPickUp,
  onClose,
}: {
  name: string;
  onRemove: () => void;
  onPickUp: () => void;
  onClose: () => void;
}) {
  const first = useRef<HTMLButtonElement>(null);

  // The menu opens after the row's remaining chips in the DOM, so a keyboard
  // user who opened it would otherwise have to tab through them to reach what
  // they just asked for.
  useEffect(() => {
    first.current?.focus();
  }, []);

  return (
    <div className="chip-menu">
      <span className="chip-menu-name">{name}</span>
      <Button ref={first} size="small" onClick={onPickUp}>
        Move or swap
      </Button>
      <Button size="small" onClick={onRemove}>
        Remove
      </Button>
      <Button size="small" onClick={onClose}>
        Cancel
      </Button>
    </div>
  );
}

// Why a chip is not a swap target while someone is being carried. Said out
// loud rather than left as a chip that quietly does nothing, since "why can I
// not drop here" is the question a greyed-out target raises.
function swapBlockedReason(
  assignee: Assignee,
  pending: Pending,
  isSource: boolean,
  canReceive: boolean,
): string {
  if (isSource) return `${assignee.name}, on the shift you are moving from`;
  if (!canReceive) return `${pending.name} is already on this shift`;
  return `${assignee.name} is already on ${formatShiftDateLong(pending.date)}`;
}

function ShiftRow({
  shift,
  selectedName,
  onSelectName,
  edit,
}: {
  shift: RotaShift;
  selectedName: string;
  onSelectName: (name: string) => void;
  edit: RowEdit | null;
}) {
  function handleClick(name: string) {
    onSelectName(name === selectedName ? "" : name);
  }

  const unallocated = isUnallocated(shift);
  const editable = edit !== null && isEditable(shift);
  const pending = edit?.pending ?? null;
  // The row someone was picked up from is not a destination: moving a person to
  // where they already are is a no-op the server would reject.
  const isSource = pending?.date === shift.date;
  const isDestination =
    editable && pending !== null && !isSource && edit.canReceive;

  const rowCls = [
    "shift-row",
    shift.closed ? "closed" : "",
    unallocated ? "unallocated" : "",
    isDestination ? "drop-target" : "",
  ]
    .filter(Boolean)
    .join(" ");

  // The chip whose action menu is open, if it is one of this row's.
  const menuAssignee =
    (edit &&
      shift.assignees.find(
        (a, i) => chipKey(shift.date, a, i) === edit.openMenu,
      )) ||
    null;

  let body;
  if (shift.closed) {
    body = <span className="shift-note">Closed</span>;
  } else if (unallocated) {
    body = <span className="shift-note">Not yet allocated</span>;
  } else {
    body = (
      <div className="shift-people">
        {shift.assignees.map((a, i) => {
          const key = chipKey(shift.date, a, i);
          if (!editable) {
            return (
              <Chip
                key={key}
                assignee={a}
                selected={a.name === selectedName}
                onClick={() => handleClick(a.name)}
              />
            );
          }
          // Nothing is in flight: the chip is the way into this person's
          // actions.
          if (pending === null) {
            return (
              <Chip
                key={key}
                assignee={a}
                selected={a.name === selectedName}
                label={`${a.name}, change this shift`}
                draggable
                onClick={() =>
                  edit.onOpenMenu(edit.openMenu === key ? null : key)
                }
                onDragStart={() => edit.onDragStart(a)}
                onDragEnd={edit.onDragEnd}
              />
            );
          }

          const swappable = isDestination && edit.canSwapWith(a);
          const picked = isSource && samePerson(pending.person, personRef(a));
          return (
            <Chip
              key={key}
              assignee={a}
              selected={a.name === selectedName}
              className={picked ? "lifted" : ""}
              disabled={!swappable}
              label={
                swappable
                  ? `Swap ${pending.name} with ${a.name}`
                  : swapBlockedReason(a, pending, isSource, edit.canReceive)
              }
              onClick={() => edit.onSwapWith(a)}
              onDrop={swappable ? () => edit.onSwapWith(a) : undefined}
            />
          );
        })}

        {editable && !pending && (
          <button
            type="button"
            className="shift-add"
            aria-label={`Add someone to ${formatShiftDateLong(shift.date)}`}
            onClick={edit.onAdd}
          >
            + Add
          </button>
        )}

        {/* The tap and keyboard equivalent of dropping on empty space. Only
            while a pick is in flight, and never during a drag, where it would
            move the rows out from under the pointer. */}
        {isDestination && !pending.dragging && (
          <Button
            size="small"
            className="shift-move-here"
            onClick={edit.onMoveHere}
          >
            Move {pending.name} here
          </Button>
        )}
      </div>
    );
  }

  return (
    <div
      className={rowCls}
      onDragOver={isDestination ? (e) => e.preventDefault() : undefined}
      onDrop={
        isDestination
          ? (e) => {
              e.preventDefault();
              edit.onMoveHere();
            }
          : undefined
      }
    >
      <div className="shift-date">{formatShiftDate(shift.date)}</div>
      {body}

      {edit && menuAssignee && (
        <ChipMenu
          name={menuAssignee.name}
          onRemove={() => edit.onRemove(menuAssignee)}
          onPickUp={() => edit.onPickUp(menuAssignee)}
          onClose={() => edit.onOpenMenu(null)}
        />
      )}

      {edit?.error && (
        <p className="shift-error" role="alert">
          {edit.error}
        </p>
      )}
    </div>
  );
}

// EditDialog is the one modal the editing flow ever shows. Both kinds end in a
// reason, because the API takes no change without one.
type EditDialog =
  | { kind: "add"; date: string }
  | {
      kind: "confirm";
      title: string;
      summary: string;
      confirmLabel: string;
      // Fully specified bar the reason, which the dialog collects.
      change: Omit<RotaChange, "reason">;
    };

export default function RotaViewer({
  rotaShifts,
  isAdmin,
  onChange,
}: RotaViewerProps) {
  const [selectedName, setSelectedName] = useState("");
  const [inputValue, setInputValue] = useState("");
  const [open, setOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(-1);
  const containerRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const bannerRef = useRef<HTMLDivElement>(null);

  // Editing is off until an admin asks for it: the rota is read far more often
  // than it is changed, and drag handles on every chip would be in the way of
  // the reading.
  const [editRequested, setEditRequested] = useState(false);
  const [pending, setPending] = useState<Pending | null>(null);
  const [openMenu, setOpenMenu] = useState<string | null>(null);
  const [dialog, setDialog] = useState<EditDialog | null>(null);
  const [saving, setSaving] = useState(false);
  const [changeError, setChangeError] = useState<{
    date: string;
    message: string;
  } | null>(null);

  // Derived rather than trusted: a session can end while the page is open, and
  // an admin who logs out in another tab must not be left looking at drag
  // handles and Add buttons the server would now refuse. Everything the editing
  // mode renders hangs off this, so losing isAdmin takes all of it away at once.
  const editing = isAdmin && editRequested;

  // The roster is only needed to add someone, and it is admin-only, so it is
  // not fetched until an admin turns editing on.
  const { volunteers, error: volunteersError } = useVolunteers({
    enabled: editing,
  });

  // The public only sees shifts with something to show — allocated or closed.
  // Admins also see unallocated shifts, flagged so they stand out.
  const visibleShifts = useMemo(
    () => (isAdmin ? rotaShifts : rotaShifts.filter((s) => !isUnallocated(s))),
    [rotaShifts, isAdmin],
  );

  const allNames = useMemo(() => getAllNames(visibleShifts), [visibleShifts]);
  const upcomingShifts = useMemo(
    () => getUpcomingShifts(visibleShifts, selectedName),
    [visibleShifts, selectedName],
  );
  const selectedVolunteerId = useMemo(
    () => getVolunteerId(visibleShifts, selectedName),
    [visibleShifts, selectedName],
  );

  const filteredNames = allNames.filter((n) =>
    n.toLowerCase().includes(inputValue.toLowerCase()),
  );

  const close = useCallback(() => {
    setOpen(false);
    setInputValue("");
    setActiveIndex(-1);
  }, []);

  useEffect(() => {
    function onMouseDown(e: MouseEvent) {
      if (!containerRef.current?.contains(e.target as Node)) close();
    }
    document.addEventListener("mousedown", onMouseDown);
    return () => document.removeEventListener("mousedown", onMouseDown);
  }, [close]);

  function handleFocus() {
    setOpen(true);
    setInputValue("");
    setActiveIndex(-1);
  }

  function handleBlur(e: React.FocusEvent) {
    if (!containerRef.current?.contains(e.relatedTarget as Node)) close();
  }

  function handleSelect(name: string) {
    setSelectedName(name);
    close();
  }

  function handleClear(e: React.MouseEvent) {
    e.stopPropagation();
    setSelectedName("");
    setInputValue("");
    setOpen(false);
    inputRef.current?.focus();
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (!open) {
      if (e.key === "ArrowDown" || e.key === "Enter") setOpen(true);
      return;
    }
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActiveIndex((i) => Math.min(i + 1, filteredNames.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActiveIndex((i) => Math.max(i - 1, -1));
    } else if (e.key === "Enter" && activeIndex >= 0) {
      handleSelect(filteredNames[activeIndex]);
    } else if (e.key === "Escape") {
      close();
    }
  }

  // --- Editing (admin only) -------------------------------------------------

  function stopEditing() {
    setEditRequested(false);
    setPending(null);
    setOpenMenu(null);
    setDialog(null);
    setChangeError(null);
  }

  // Picking someone up unmounts the menu the pick was made from, which would
  // otherwise drop focus to the top of the document. The banner is the thing
  // that explains the mode, so focus lands there and tabbing carries on into
  // the destinations below it. Not for a drag, where focus is not the point.
  useEffect(() => {
    if (pending && !pending.dragging) bannerRef.current?.focus();
  }, [pending]);

  // Escape backs out of the editing flow a step at a time — drop whoever is
  // being carried, or close an open chip menu — the same way it dismisses the
  // search. Not while a dialog is up: there, Escape belongs to the dialog.
  useEffect(() => {
    if (dialog || (!pending && !openMenu)) return;
    function onKeyDown(e: KeyboardEvent) {
      if (e.key !== "Escape") return;
      if (pending) setPending(null);
      else setOpenMenu(null);
    }
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [pending, openMenu, dialog]);

  // Fires the change, then leaves the rota showing whatever the server now
  // says. A refusal is not a failure of the app: the message explains which
  // volunteer contradicts the shift, so it is shown against that shift and the
  // rota is left exactly as it was — nothing was applied to roll back.
  async function submit(change: RotaChange) {
    setSaving(true);
    try {
      await onChange(change);
      setChangeError(null);
    } catch (err) {
      setChangeError({
        date: change.date,
        message:
          err instanceof Error ? err.message : "The change was not applied",
      });
    } finally {
      setSaving(false);
      setDialog(null);
      setPending(null);
      setOpenMenu(null);
    }
  }

  function askRemove(date: string, assignee: Assignee) {
    setOpenMenu(null);
    setChangeError(null);
    setDialog({
      kind: "confirm",
      title: `Remove ${assignee.name}?`,
      summary: `${assignee.name} comes off the shift on ${formatShiftDateLong(date)}.`,
      confirmLabel: "Remove",
      change: { date, out: personRef(assignee) },
    });
  }

  // A move is a swap with nobody coming back: the change is applied on the
  // destination and reversed on swapDate, which takes them off the shift they
  // came from. One request, so the rota is never briefly missing them or
  // showing them twice.
  function askMove(to: string) {
    if (!pending) return;
    setDialog({
      kind: "confirm",
      title: `Move ${pending.name}?`,
      summary: `${pending.name} moves from ${formatShiftDateLong(pending.date)} to ${formatShiftDateLong(to)}.`,
      confirmLabel: "Move",
      change: { date: to, in: pending.person, swapDate: pending.date },
    });
  }

  function askSwap(to: string, assignee: Assignee) {
    if (!pending) return;
    setDialog({
      kind: "confirm",
      title: `Swap ${pending.name} and ${assignee.name}?`,
      summary:
        `${pending.name} takes ${assignee.name}'s shift on ${formatShiftDateLong(to)}, ` +
        `and ${assignee.name} takes ${pending.name}'s on ${formatShiftDateLong(pending.date)}.`,
      confirmLabel: "Swap",
      change: {
        date: pending.date,
        out: pending.person,
        in: personRef(assignee),
        swapDate: to,
      },
    });
  }

  function pickUp(date: string, assignee: Assignee, dragging: boolean) {
    setOpenMenu(null);
    setChangeError(null);
    setPending({
      date,
      person: personRef(assignee),
      name: assignee.name,
      dragging,
    });
  }

  function rowEdit(shift: RotaShift): RowEdit {
    // Nobody can be on one shift twice, which rules out both ends of a swap
    // independently: the carried person must not already be on the destination,
    // and whoever they trade with must not already be on the shift they came
    // from. The server enforces both; ruling them out here means the admin is
    // not offered a drop that can only end in a refusal.
    const carriedFrom = pending
      ? rotaShifts.find((s) => s.date === pending.date)
      : undefined;
    const alreadyHere =
      pending !== null &&
      shift.assignees.some((a) => samePerson(personRef(a), pending.person));

    return {
      pending,
      openMenu,
      error: changeError?.date === shift.date ? changeError.message : null,
      canReceive: !alreadyHere,
      canSwapWith: (a) =>
        !carriedFrom?.assignees.some((x) =>
          samePerson(personRef(x), personRef(a)),
        ),
      onOpenMenu: setOpenMenu,
      onRemove: (a) => askRemove(shift.date, a),
      onPickUp: (a) => pickUp(shift.date, a, false),
      onDragStart: (a) => pickUp(shift.date, a, true),
      // Only clears a drag; a pick made by tapping outlives the pointer.
      onDragEnd: () => setPending((p) => (p?.dragging ? null : p)),
      onSwapWith: (a) => askSwap(shift.date, a),
      onMoveHere: () => askMove(shift.date),
      onAdd: () => {
        setChangeError(null);
        setOpenMenu(null);
        setDialog({ kind: "add", date: shift.date });
      },
    };
  }

  // Who can still be added to a shift: everyone the roster knows, less the
  // people already on it, whom the server would refuse anyway.
  function addableTo(date: string): Volunteer[] | null {
    if (volunteers === null) return null;
    const onShift = new Set(
      rotaShifts
        .find((s) => s.date === date)
        ?.assignees.map((a) => a.volunteerId)
        .filter(Boolean),
    );
    return volunteers.filter((v) => !onShift.has(v.id));
  }

  return (
    <div className="rota-viewer">
      <div className="rota-heading">
        <h1>Ilford Drop-in Rota</h1>
        {isAdmin && (
          <Button
            size="small"
            aria-pressed={editing}
            onClick={() => (editing ? stopEditing() : setEditRequested(true))}
          >
            {editing ? "Done" : "Edit rota"}
          </Button>
        )}
      </div>

      <div className="rota-search-wrap" ref={containerRef}>
        <div className={`rota-search-input-wrap${open ? " open" : ""}`}>
          <input
            ref={inputRef}
            type="text"
            className="rota-search"
            placeholder="Search by name…"
            value={open ? inputValue : selectedName}
            onChange={(e) => {
              setInputValue(e.target.value);
              setActiveIndex(-1);
            }}
            onFocus={handleFocus}
            onBlur={handleBlur}
            onKeyDown={handleKeyDown}
          />
          {selectedName && !open && (
            <button
              className="rota-search-clear"
              onMouseDown={handleClear}
              tabIndex={-1}
              aria-label="Clear"
            >
              <svg viewBox="0 0 14 14" width="14" height="14">
                <path
                  d="M13 1L1 13M1 1l12 12"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                />
              </svg>
            </button>
          )}
        </div>

        {open && (
          <ul className="rota-search-dropdown" role="listbox">
            {filteredNames.length === 0 ? (
              <li className="rota-search-no-options">No names found</li>
            ) : (
              filteredNames.map((name, i) => (
                <li
                  key={name}
                  role="option"
                  aria-selected={name === selectedName}
                  className={`rota-search-option${i === activeIndex ? " active" : ""}${name === selectedName ? " selected" : ""}`}
                  onMouseDown={() => handleSelect(name)}
                  onMouseEnter={() => setActiveIndex(i)}
                >
                  {name}
                </li>
              ))
            )}
          </ul>
        )}
      </div>

      {selectedName && (
        <div className="upcoming-strip">
          {upcomingShifts.length > 0 ? (
            <>
              <span className="upcoming-strip-label">Upcoming: </span>
              <span className="upcoming-dates">
                {upcomingShifts
                  .slice(0, 5)
                  .map((s) => formatShiftDate(s.date))
                  .join(" · ")}
              </span>
              {selectedVolunteerId && (
                <CalendarCopyButton volunteerId={selectedVolunteerId} />
              )}
            </>
          ) : (
            <span className="upcoming-none">
              No upcoming shifts for {selectedName}
            </span>
          )}
        </div>
      )}

      {/* Says what carrying someone means, and gives a way out of it that is
          not "drop them somewhere". aria-live because entering this mode moves
          no focus, so a screen reader would otherwise not hear about it. */}
      {editing && pending && !pending.dragging && (
        <div
          className="rota-edit-banner"
          role="status"
          ref={bannerRef}
          tabIndex={-1}
        >
          <span>
            Carrying <strong>{pending.name}</strong> from{" "}
            {formatShiftDateLong(pending.date)}. Choose a shift to move them to,
            or a person to swap them with.
          </span>
          <Button size="small" onClick={() => setPending(null)}>
            Cancel
          </Button>
        </div>
      )}

      {editing && !pending && (
        <p className="rota-edit-hint">
          Drag a name onto another shift to move them, or onto another name to
          swap. On a touchscreen, tap a name instead.
        </p>
      )}

      <div className="rota-list">
        {visibleShifts.map((shift) => (
          <ShiftRow
            key={shift.date}
            shift={shift}
            selectedName={selectedName}
            onSelectName={setSelectedName}
            edit={editing ? rowEdit(shift) : null}
          />
        ))}
      </div>

      {editing && dialog?.kind === "confirm" && (
        <ConfirmChangeDialog
          title={dialog.title}
          summary={dialog.summary}
          confirmLabel={dialog.confirmLabel}
          busy={saving}
          onCancel={() => {
            setDialog(null);
            setPending(null);
          }}
          onConfirm={(reason) => void submit({ ...dialog.change, reason })}
        />
      )}

      {editing && dialog?.kind === "add" && (
        <AddAssigneeDialog
          dateLabel={formatShiftDateLong(dialog.date)}
          volunteers={addableTo(dialog.date)}
          volunteersError={volunteersError}
          busy={saving}
          onCancel={() => setDialog(null)}
          onConfirm={(person, reason, role) =>
            void submit({ date: dialog.date, in: person, role, reason })
          }
        />
      )}
    </div>
  );
}
