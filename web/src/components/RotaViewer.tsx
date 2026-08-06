import { useMemo, useState, useRef, useEffect, useCallback } from "react";
import type {
  Assignee,
  PersonRef,
  Preallocation,
  Role,
  RotaChange,
  RotaShift,
  Volunteer,
} from "../types";
import { SERVICE_VOLUNTEER_ROLE, TEAM_LEAD_ROLE } from "../types";
import { usePreallocations } from "../hooks/usePreallocations";
import type { RoleColourOf } from "../hooks/useRoles";
import { useRoles } from "../hooks/useRoles";
import { useVolunteers } from "../hooks/useVolunteers";
import Button from "../ui/Button";
import type { AssigneeChange } from "./RotaEditDialogs";
import {
  AssigneeDialog,
  ClosureDialog,
  ConfirmChangeDialog,
  PinDialog,
  ShiftTimesDialog,
  UnpinDialog,
} from "./RotaEditDialogs";
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
  // Shuts or reopens one shift, on the same terms. Separate from onChange
  // because it is not an alteration: it changes what allocation will do rather
  // than what an allocated rota says.
  onSetClosed: (shiftId: string, closed: boolean) => Promise<void>;
  // Moves one shift's start and end, and with the start its date. Also not an
  // alteration, and unlike a closure not frozen at allocation: the times say
  // when to turn up, and the rota was solved in dates.
  onSetTimes: (shiftId: string, start: string, end: string) => Promise<void>;
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

// "19:30" — the shift's own wall-clock time, read straight off the string
// rather than through `new Date()`, which would redraw it in the reader's zone.
// The drop-in runs at half seven in Ilford whoever is looking.
function timeOfDay(timestamp: string): string {
  return timestamp.slice("2026-02-02T".length, "2026-02-02T19:30".length);
}

// "19:30–21:30", or "All day" for a shift running one midnight to the next.
//
// That second case is not a shift anybody typed: it is what the migration that
// made times mandatory left behind on a deployment where nobody had ever said
// when the drop-in runs. Rendering it as the day it is beats rendering
// "00:00–00:00", and an admin puts the real hours on it from the same row.
function formatShiftTimes(start: string, end: string): string {
  const from = timeOfDay(start);
  const to = timeOfDay(end);
  if (from === "00:00" && to === "00:00") return "All day";
  return `${from}\u2013${to}`;
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
  onReplace: (assignee: Assignee) => void;
  onPickUp: (assignee: Assignee) => void;
  onDragStart: (assignee: Assignee) => void;
  onDragEnd: () => void;
  onSwapWith: (assignee: Assignee) => void;
  onMoveHere: () => void;
  onAdd: () => void;
  // Pinning is the unallocated row's half of editing: those rows have nobody on
  // them to drag, and what an admin can change there is who allocation will be
  // made to place.
  onPin: () => void;
  onUnpin: (pin: Preallocation) => void;
  // Whether this row may be shut or opened at all. False once the rota has been
  // allocated: closure is an allocator input, and the rota was solved around it.
  canSetClosed: boolean;
  onSetClosed: () => void;
  // Editing the hours is offered on every row, allocated or not: the times are
  // descriptive, so nothing about them froze when the rota was solved.
  onEditTimes: () => void;
}

function Chip({
  assignee,
  colourOf,
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
  colourOf: RoleColourOf;
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
      // The colour is an attribute rather than a class, and the Role's palette
      // token rather than its name: role names are configuration, so
      // `role-${name}` would mint class names no stylesheet has a rule for,
      // while the palette is closed and index.css has a rule per token. A Role
      // the server does not name — one retired since this rota was allocated —
      // gets no attribute and the chip's own default.
      data-role-colour={colourOf(assignee.role) ?? undefined}
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

// ChipMenu is what a chip offers when tapped in editing mode: everything that
// can be done to one person on one shift. It is the touch and keyboard route to
// everything drag and drop offers — "move or swap" picks the person up, and the
// destination is then chosen from the rows themselves. Replace has no drag
// equivalent: both people are on the same shift, so there is nowhere to drag to.
function ChipMenu({
  name,
  onRemove,
  onReplace,
  onPickUp,
  onClose,
}: {
  name: string;
  onRemove: () => void;
  onReplace: () => void;
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
      <Button size="small" onClick={onReplace}>
        Replace
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
  picked: boolean,
  isSource: boolean,
  canReceive: boolean,
): string {
  if (picked) return `${assignee.name}, the person you are moving`;
  if (isSource) return `${assignee.name}, on the shift you are moving from`;
  if (!canReceive) return `${pending.name} is already on this shift`;
  return `${assignee.name} is already on ${formatShiftDateLong(pending.date)}`;
}

// What a pin means. There is one kind, whether an admin made it here or a
// Standing Preallocation seeded it when the rota was defined, so there is one
// thing to say about it.
function pinTitle(pin: Preallocation): string {
  // Naming the uncapped Role would be noise — being pinned to a shift already
  // means being pinned to one of its ordinary Seats.
  const role =
    pin.role === SERVICE_VOLUNTEER_ROLE ? "" : ` as ${pin.role.toLowerCase()}`;
  return `${pin.name} is pinned${role} to this shift, and will be placed here when the rota is allocated.`;
}

// PreallocationList shows who allocation is already committed to placing on a
// shift it has not run for yet. Not chips: nothing here can be dragged or
// searched for — these people are not on the rota, they are promised to it. The
// one thing that can be done to a pin is taking it off, and every pin can be
// taken off.
function PreallocationList({
  date,
  pins,
  colourOf,
  onUnpin,
}: {
  date: string;
  pins: Preallocation[];
  colourOf: RoleColourOf;
  // Absent unless editing is on.
  onUnpin?: (pin: Preallocation) => void;
}) {
  return (
    <div className="prealloc">
      <span className="prealloc-label" id={`pinned-${date}`}>
        Pinned:
      </span>
      <ul className="prealloc-list" aria-labelledby={`pinned-${date}`}>
        {pins.map((pin) => (
          <li
            key={pin.id}
            className={`prealloc-chip ${pin.custom ? "custom" : "volunteer"}`}
            data-role-colour={colourOf(pin.role) ?? undefined}
            title={pinTitle(pin)}
          >
            {pin.name}
            {onUnpin && (
              <button
                type="button"
                className="prealloc-unpin"
                // The date is in the label because the button is a bare cross:
                // read out of the row's context, "Remove" alone does not say
                // which pin, and the rows all look alike.
                aria-label={`Remove ${pin.name}'s pin on ${formatShiftDateLong(date)}`}
                onClick={() => onUnpin(pin)}
              >
                <svg
                  viewBox="0 0 10 10"
                  width="9"
                  height="9"
                  aria-hidden="true"
                >
                  <path
                    d="M1 1l8 8M9 1L1 9"
                    stroke="currentColor"
                    strokeWidth="1.8"
                    strokeLinecap="round"
                  />
                </svg>
              </button>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}

// ShiftWhen is a row's first column: the day, and under it the hours. Both come
// from the shift itself, which is what makes them worth showing per row — a
// shift keeps the times it was minted with, so one evening running differently
// from the rest shows up here rather than nowhere.
function ShiftWhen({ shift }: { shift: RotaShift }) {
  return (
    <>
      <span className="shift-date">{formatShiftDate(shift.date)}</span>
      <span className="shift-time">
        {formatShiftTimes(shift.start, shift.end)}
      </span>
    </>
  );
}

function ShiftRow({
  shift,
  pins,
  colourOf,
  selectedName,
  onSelectName,
  edit,
}: {
  shift: RotaShift;
  // Everyone already pinned to this shift. Only ever non-empty for an admin
  // looking at a shift whose rota has not been allocated.
  pins: Preallocation[];
  colourOf: RoleColourOf;
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

  // Shutting a date and opening it again is offered on the same terms as
  // pinning: while editing, with nothing being carried, and only where the rota
  // has not been allocated. It is the one editing affordance a closed row has —
  // there is nobody on it to do anything else to.
  const closureButton = edit && !pending && edit.canSetClosed && (
    <button
      type="button"
      className="shift-add shift-closure"
      aria-label={
        shift.closed
          ? `Reopen ${formatShiftDateLong(shift.date)}`
          : `Close ${formatShiftDateLong(shift.date)}`
      }
      onClick={edit.onSetClosed}
    >
      {shift.closed ? "Reopen" : "Close"}
    </button>
  );

  let body;
  if (shift.closed) {
    body = (
      <div className="shift-unallocated">
        <span className="shift-note">Closed</span>
        {closureButton}
      </div>
    );
  } else if (unallocated) {
    body = (
      <div className="shift-unallocated">
        <span className="shift-note">Not yet allocated</span>
        {pins.length > 0 && (
          <PreallocationList
            date={shift.date}
            pins={pins}
            colourOf={colourOf}
            // While someone is being carried the page narrows to placing them,
            // the same way the Add buttons go away: an unallocated shift is not
            // a destination, so its pins are only there to be read.
            onUnpin={edit && !pending ? edit.onUnpin : undefined}
          />
        )}
        {/* Editing an unallocated shift means changing who is promised it —
            there is nobody on it to move around. */}
        {edit && !pending && (
          <div className="shift-actions">
            <button
              type="button"
              className="shift-add shift-pin"
              aria-label={`Pin someone to ${formatShiftDateLong(shift.date)}`}
              onClick={edit.onPin}
            >
              + Pin
            </button>
            {closureButton}
          </div>
        )}
      </div>
    );
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
                colourOf={colourOf}
                selected={a.name === selectedName}
                onClick={() => handleClick(a.name)}
              />
            );
          }
          // One chip either way, not one per state: `draggable` has to survive
          // the re-render that starting a drag causes. onDragStart sets the
          // pending pick, which re-renders this very chip, and React removing
          // the attribute mid-drag cancels the drag in Chromium — the pick
          // registers, the name never moves. So the drag props are
          // unconditional and only what the chip *does* on click or drop
          // changes: its own menu when nothing is in flight, a swap target
          // when someone is being carried.
          const swappable =
            pending !== null && isDestination && edit.canSwapWith(a);
          const picked =
            pending !== null &&
            isSource &&
            samePerson(pending.person, personRef(a));
          return (
            <Chip
              key={key}
              assignee={a}
              colourOf={colourOf}
              selected={a.name === selectedName}
              className={picked ? "lifted" : ""}
              draggable
              disabled={pending !== null && !swappable}
              label={
                pending === null
                  ? `${a.name}, change this shift`
                  : swappable
                    ? `Swap ${pending.name} with ${a.name}`
                    : swapBlockedReason(
                        a,
                        pending,
                        picked,
                        isSource,
                        edit.canReceive,
                      )
              }
              onClick={
                pending === null
                  ? () => edit.onOpenMenu(edit.openMenu === key ? null : key)
                  : () => edit.onSwapWith(a)
              }
              onDragStart={() => edit.onDragStart(a)}
              onDragEnd={edit.onDragEnd}
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
      {edit && !pending ? (
        <button
          type="button"
          className="shift-when shift-when-editable"
          aria-label={`Change when ${formatShiftDateLong(shift.date)} runs`}
          onClick={edit.onEditTimes}
        >
          <ShiftWhen shift={shift} />
        </button>
      ) : (
        <div className="shift-when">
          <ShiftWhen shift={shift} />
        </div>
      )}
      {body}

      {edit && menuAssignee && (
        <ChipMenu
          name={menuAssignee.name}
          onRemove={() => edit.onRemove(menuAssignee)}
          onReplace={() => edit.onReplace(menuAssignee)}
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
  // Someone joining the shift on date, either alongside the people already on
  // it or in place of one of them.
  | { kind: "assignee"; date: string; change: AssigneeChange }
  | {
      kind: "confirm";
      title: string;
      summary: string;
      confirmLabel: string;
      // Fully specified bar the reason, which the dialog collects.
      change: Omit<RotaChange, "reason">;
    }
  // Someone being pinned to, or unpinned from, a shift the rota has not been
  // run for. Not alterations: nothing is on the rota yet to alter.
  | { kind: "pin"; date: string }
  | { kind: "unpin"; pin: Preallocation }
  // A shift being shut or opened again, which is neither an alteration nor a
  // pin: it changes whether the drop-in runs that day at all.
  | { kind: "closure"; shift: RotaShift }
  // A shift's hours being moved, which may move the shift to another day.
  | { kind: "times"; shift: RotaShift };

export default function RotaViewer({
  rotaShifts,
  isAdmin,
  onChange,
  onSetClosed,
  onSetTimes,
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

  // What each Role is drawn in. Public, and fetched whoever is looking: the
  // chips are the rota. A failure leaves every chip in the default colour,
  // which is a rota that reads fine, so it is not reported here.
  const { colourOf } = useRoles();

  // The roster is only needed to add someone, and it is admin-only, so it is
  // not fetched until an admin turns editing on.
  const { volunteers, error: volunteersError } = useVolunteers({
    enabled: editing,
  });

  // Pins only say anything about shifts the rota has not been run for, and only
  // admins see those, so only admins fetch them. Not gated on editing: who is
  // already promised to an unallocated shift is something to read, not a
  // change to make.
  const {
    preallocations,
    error: preallocationsError,
    addPin,
    removePin,
  } = usePreallocations({ enabled: isAdmin });

  const pinsByDate = useMemo(() => {
    const byDate = new Map<string, Preallocation[]>();
    for (const pin of preallocations ?? []) {
      const forDate = byDate.get(pin.date);
      if (forDate) forDate.push(pin);
      else byDate.set(pin.date, [pin]);
    }
    return byDate;
  }, [preallocations]);

  // The public only sees shifts with something to show — allocated or closed.
  // Admins also see unallocated shifts, flagged so they stand out.
  const visibleShifts = useMemo(
    () => (isAdmin ? rotaShifts : rotaShifts.filter((s) => !isUnallocated(s))),
    [rotaShifts, isAdmin],
  );

  // Whether anything on screen can be pinned to at all — only ever true for an
  // admin, since the public is not shown unallocated shifts in the first place.
  const hasUnallocated = useMemo(
    () => visibleShifts.some(isUnallocated),
    [visibleShifts],
  );

  // Whether any row can be shut or opened. A wider set than hasUnallocated: a
  // shift that is already closed is not "not yet allocated", but reopening it
  // is exactly what an admin might be here to do.
  const hasClosable = useMemo(
    () => visibleShifts.some((s) => !s.allocated),
    [visibleShifts],
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

  // Fires one change against a shift, then leaves the page showing whatever the
  // server now says. A refusal is not a failure of the app: the message explains
  // what the change contradicts, so it is shown against the shift it was refused
  // for and nothing is rolled back — nothing was applied.
  async function run(
    date: string,
    apply: () => Promise<void>,
    fallback: string,
  ) {
    setSaving(true);
    try {
      await apply();
      setChangeError(null);
    } catch (err) {
      setChangeError({
        date,
        message: err instanceof Error ? err.message : fallback,
      });
    } finally {
      setSaving(false);
      setDialog(null);
      setPending(null);
      setOpenMenu(null);
    }
  }

  function submit(change: RotaChange) {
    return run(
      change.date,
      () => onChange(change),
      "The change was not applied",
    );
  }

  // Pinning goes through the same path as an alteration but is not one: it
  // changes what allocation will do rather than what a published rota says, so
  // it is its own request and its own reload.
  function submitPin(date: string, person: PersonRef, role: Role) {
    return run(
      date,
      () => addPin({ date, person, role }),
      "The pin was not saved",
    );
  }

  function submitClosure(shift: RotaShift) {
    return run(
      shift.date,
      () => onSetClosed(shift.id, !shift.closed),
      shift.closed ? "The shift was not reopened" : "The shift was not closed",
    );
  }

  function submitTimes(shift: RotaShift, start: string, end: string) {
    return run(
      shift.date,
      () => onSetTimes(shift.id, start, end),
      "The shift times were not saved",
    );
  }

  function submitUnpin(pin: Preallocation) {
    // Only ever called for a manual pin, which is the only kind with an id.
    if (pin.id === null) return;
    const id = pin.id;
    return run(pin.date, () => removePin(id), "The pin was not removed");
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

  // A replacement is one request on one date: the outgoing person leaves and
  // the incoming one arrives together, so the shift is never briefly short of
  // someone and the incoming volunteer takes the role that was just vacated.
  function askReplace(date: string, assignee: Assignee) {
    setOpenMenu(null);
    setChangeError(null);
    setDialog({
      kind: "assignee",
      date,
      change: { kind: "replace", outgoing: assignee },
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
      onReplace: (a) => askReplace(shift.date, a),
      onPickUp: (a) => pickUp(shift.date, a, false),
      onDragStart: (a) => pickUp(shift.date, a, true),
      // Only clears a drag; a pick made by tapping outlives the pointer.
      onDragEnd: () => setPending((p) => (p?.dragging ? null : p)),
      onSwapWith: (a) => askSwap(shift.date, a),
      onMoveHere: () => askMove(shift.date),
      onAdd: () => {
        setChangeError(null);
        setOpenMenu(null);
        setDialog({
          kind: "assignee",
          date: shift.date,
          change: {
            kind: "add",
            // A shift has one team lead. Where it already has one, joining as
            // one is not on offer — the way to change who leads is to replace
            // them, which hands the role over rather than adding a second.
            leadTaken: shift.assignees.some((a) => a.role === TEAM_LEAD_ROLE),
          },
        });
      },
      onPin: () => {
        setChangeError(null);
        setOpenMenu(null);
        setDialog({ kind: "pin", date: shift.date });
      },
      onUnpin: (pin) => {
        setChangeError(null);
        setOpenMenu(null);
        setDialog({ kind: "unpin", pin });
      },
      // An allocated rota was solved around which of its shifts run, so the
      // flag is frozen. Its times are not: they are descriptive, and onEditTimes
      // below is offered whether or not the rota has been run.
      canSetClosed: !shift.allocated,
      onSetClosed: () => {
        setChangeError(null);
        setOpenMenu(null);
        setDialog({ kind: "closure", shift });
      },
      onEditTimes: () => {
        setChangeError(null);
        setOpenMenu(null);
        setDialog({ kind: "times", shift });
      },
    };
  }

  // Who can still join a shift: everyone the roster knows, less the people
  // already on it, whom the server would refuse anyway. Replacing someone draws
  // on the same list, which rules out replacing them with themselves.
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

  // Who can still be pinned to an unallocated shift: the active roster, less
  // anyone already pinned there from either source. Both exclusions matter and
  // for different reasons — the server refuses a pin for an inactive volunteer
  // or a repeat of a manual one, and silently drops a manual pin that repeats a
  // config one, which would look like it had worked.
  function pinnableTo(date: string): Volunteer[] | null {
    if (volunteers === null) return null;
    const pinned = new Set(
      (pinsByDate.get(date) ?? []).map((p) => p.volunteerId).filter(Boolean),
    );
    return volunteers.filter((v) => v.active && !pinned.has(v.id));
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
          {/* Only where there is a shift it applies to. On a rota that has all
              been allocated there is nothing to pin to, and the sentence would
              send an admin looking for a button that is not on any row. */}
          {hasUnallocated && (
            <>
              {" "}
              Shifts the rota has not been run for take pins instead: whoever
              you pin there is guaranteed the shift when it is allocated.
            </>
          )}
          {hasClosable && (
            <>
              {" "}
              Close one for a date the drop-in is not running, up until the rota
              is allocated.
            </>
          )}
        </p>
      )}

      {/* A failed pin load leaves the unallocated rows looking empty when they
          may not be, so it is said out loud rather than swallowed. It does not
          stop the rota itself being read. */}
      {preallocationsError && (
        <p className="rota-notice" role="alert">
          Could not load who is already pinned to unallocated shifts:{" "}
          {preallocationsError}
        </p>
      )}

      <div className="rota-list">
        {visibleShifts.map((shift) => (
          <ShiftRow
            key={shift.date}
            shift={shift}
            pins={pinsByDate.get(shift.date) ?? []}
            colourOf={colourOf}
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

      {editing && dialog?.kind === "assignee" && (
        <AssigneeDialog
          dateLabel={formatShiftDateLong(dialog.date)}
          change={dialog.change}
          volunteers={addableTo(dialog.date)}
          volunteersError={volunteersError}
          busy={saving}
          onCancel={() => setDialog(null)}
          onConfirm={(person, reason, role) =>
            void submit({
              date: dialog.date,
              in: person,
              out:
                dialog.change.kind === "replace"
                  ? personRef(dialog.change.outgoing)
                  : undefined,
              role,
              reason,
            })
          }
        />
      )}

      {editing && dialog?.kind === "pin" && (
        <PinDialog
          dateLabel={formatShiftDateLong(dialog.date)}
          volunteers={pinnableTo(dialog.date)}
          volunteersError={volunteersError}
          // A shift has one team-lead Seat, so a lead already pinned there
          // rules out a second — and it can be given up from here, whichever
          // way it came to be made.
          leadPinned={(pinsByDate.get(dialog.date) ?? []).some(
            (p) => p.role === TEAM_LEAD_ROLE,
          )}
          pinnedNames={(pinsByDate.get(dialog.date) ?? []).map((p) => p.name)}
          busy={saving}
          onCancel={() => setDialog(null)}
          onConfirm={(person, role) =>
            void submitPin(dialog.date, person, role)
          }
        />
      )}

      {editing && dialog?.kind === "closure" && (
        <ClosureDialog
          dateLabel={formatShiftDateLong(dialog.shift.date)}
          closing={!dialog.shift.closed}
          pinnedCount={(pinsByDate.get(dialog.shift.date) ?? []).length}
          busy={saving}
          onCancel={() => setDialog(null)}
          onConfirm={() => void submitClosure(dialog.shift)}
        />
      )}

      {editing && dialog?.kind === "times" && (
        <ShiftTimesDialog
          dateLabel={formatShiftDateLong(dialog.shift.date)}
          start={dialog.shift.start}
          end={dialog.shift.end}
          busy={saving}
          onCancel={() => setDialog(null)}
          onConfirm={(start, end) => void submitTimes(dialog.shift, start, end)}
        />
      )}

      {editing && dialog?.kind === "unpin" && (
        <UnpinDialog
          name={dialog.pin.name}
          dateLabel={formatShiftDateLong(dialog.pin.date)}
          busy={saving}
          onCancel={() => setDialog(null)}
          onConfirm={() => void submitUnpin(dialog.pin)}
        />
      )}
    </div>
  );
}
