import { useEffect, useRef } from "react";
import type { Assignee, PersonRef, Preallocation, RotaShift } from "../types";
import { SERVICE_VOLUNTEER_ROLE } from "../types";
import type { RoleColourOf } from "../hooks/useRoles";
import Button from "../ui/Button";
import {
  formatShiftDate,
  formatShiftDateLong,
  isUnallocated,
  personRef,
  samePerson,
} from "./shifts";
import { formatShiftTimes } from "./shiftTimes";
import "./ShiftList.css";

// Whether a shift can have people moved about on it. A closed shift has nobody
// on it by definition, and an unallocated one has not been through allocation,
// so neither is somewhere a person can be put or taken from.
function isEditable(shift: RotaShift): boolean {
  return !shift.closed && shift.allocated;
}

// Identifies one chip in the whole list, for "which chip's menu is open". The
// index is part of it because the same custom entry can legitimately appear
// twice on a shift — two people from the same visiting group.
function chipKey(shiftDate: string, assignee: Assignee, index: number): string {
  return `${shiftDate}/${assignee.volunteerId ?? assignee.name}/${index}`;
}

// Returns the draft deficit for each role in a shifts shape. Only roles with
// a deficit are returned.
function shiftDeficit(
  shape: RotaShift["shape"],
  assignees: Assignee[],
): { role: string; deficit: number }[] {
  const assigneeCountByRole = assignees.reduce(
    (acc: Record<string, number>, { role }) => {
      if (acc[role]) {
        acc[role] += 1;
      } else {
        acc[role] = 1;
      }
      return acc;
    },
    {},
  );

  return shape
    .map(({ role, count }) => ({
      role,
      // A Role nobody was drafted into is short its whole count, not absent
      // from the answer: without the fallback the subtraction is NaN, NaN > 0
      // is false, and a shift the solver could find no team lead for was the
      // one shift that said nothing about it.
      deficit: count - (assigneeCountByRole[role] ?? 0),
    }))
    .filter(({ deficit }) => deficit > 0);
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

// The corner dot marking group membership, on whichever kind of chip is showing
// the person: an allocated one or a drafted one. Group is why two names appear
// together, which is worth seeing on a draft as much as on the rota — it is
// often the explanation of a placement an admin was not expecting.
function GroupDot({ group }: { group: string }) {
  return (
    <span
      className="chip-group-dot"
      style={{ background: groupColour(group) }}
      title={`Group: ${group}`}
    />
  );
}

// The draft of a caller that shows none. A shared empty map rather than one per
// render, so a row's props are the same object between renders.
const NO_DRAFT: Map<string, Assignee[]> = new Map();

// Pending is the person the admin has picked up, on their way to another shift.
// The same state backs both routes to a move or a swap, so the drop handlers do
// not care which was used.
export interface Pending {
  date: string;
  person: PersonRef;
  name: string;
  // True while an HTML5 drag is in flight. The tap route sets it false and the
  // row then grows an explicit "Move here" button; doing that mid-drag would
  // shift every row under the pointer just as it is aiming at one.
  dragging: boolean;
}

// PlacementEdit is one row's half of moving people about an allocated shift.
// Absent where there is nothing allocated to move — the Allocation tab, whose
// rows are all shifts of the rota in flight.
export interface PlacementEdit {
  pending: Pending | null;
  // Key of the chip whose action menu is open, anywhere in the list, so opening
  // one closes the last.
  openMenu: string | null;
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
}

// RowEdit is one shift row's half of the editing state: what is in flight, and
// what this row can do about it. Absent when editing is off, which is how the
// row renders exactly as it did before the feature existed.
export interface RowEdit {
  // The server's message from a change that was refused against this shift.
  error: string | null;
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
  // Whether the Shape can be edited from here at all. False while the Roles
  // have not loaded: the form asks how many of each Role the shift wants, and
  // there is nothing to ask about until the list arrives.
  canEditShape: boolean;
  // Changing what the shift asks for. Offered on the same terms as the closure
  // beside it — while the rota is unallocated — because a Shape is an allocator
  // input too.
  onEditShape: () => void;
  // Moving people between allocated shifts, where there are any.
  placement: PlacementEdit | null;
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
        // Deferred: calling this synchronously re-renders the dragged chip
        // itself (its class/aria-disabled change once `pending` is set)
        // before the browser has finished committing to the drag session it
        // just started, which is enough on its own to make Chromium silently
        // abort it. Pushing the state update to a macrotask lets the browser
        // finish that first.
        setTimeout(() => onDragStart?.(), 0);
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
      {assignee.group && <GroupDot group={assignee.group} />}
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
  // Naming the ordinary Role would be noise — being pinned to a shift already
  // means being pinned to one of its ordinary Seats.
  const role =
    pin.role === SERVICE_VOLUNTEER_ROLE ? "" : ` as ${pin.role.toLowerCase()}`;
  return `${pin.name} is pinned${role} to this shift, and will be placed here when the rota is allocated.`;
}

// What a drafted name means. Deliberately the same sentence shape as pinTitle,
// because these are the two things on an unallocated row that are not on the
// rota — and the difference between them is the whole point: a pin is a promise
// an admin made, a draft Seat is a guess the solver made and will make again.
function draftTitle(assignee: Assignee): string {
  const role =
    assignee.role === SERVICE_VOLUNTEER_ROLE
      ? ""
      : ` as ${assignee.role.toLowerCase()}`;
  return `The last solve put ${assignee.name} here${role}. It is a draft, not a placement, and the next solve may put somebody else here.`;
}

// One name expected on a shift the rota has not been run for, and on what
// footing: an admin's pin, or a Seat the last solve filled.
type Planned =
  { kind: "pin"; pin: Preallocation } | { kind: "draft"; assignee: Assignee };

// How the alterations API would name the person a pin is for. The same shape
// personRef gives an assignee, so the two can be compared.
function pinRef(pin: Preallocation): PersonRef {
  return pin.volunteerId
    ? { volunteerId: pin.volunteerId }
    : { custom: pin.name };
}

// Everybody expected on an unallocated shift, in one list: the pins first, then
// whoever else the last solve put there.
//
// A pin is honoured by every solve, so each one is usually drafted too — and
// showing both would name the same person twice. The pin is the one kept: it is
// the stronger statement, and it is the one that can be taken off.
//
// Matched by person and consumed as it goes, so two customs of the same name —
// two people from one visiting group — line up one pin each rather than both
// folding into the first.
function plannedFor(pins: Preallocation[], drafted: Assignee[]): Planned[] {
  const unmatched = [...drafted];

  const planned: Planned[] = pins.map((pin) => {
    const i = unmatched.findIndex((a) => samePerson(personRef(a), pinRef(pin)));
    if (i !== -1) unmatched.splice(i, 1);
    return { kind: "pin", pin };
  });

  for (const assignee of unmatched) {
    planned.push({ kind: "draft", assignee });
  }
  return planned;
}

// PlannedList shows who is expected on a shift the rota has not been run for:
// the people an admin has pinned to it, and whoever the last solve put in the
// Seats around them.
//
// One row rather than the two it used to be (issue #193). Pins and draft were a
// labelled line each, which named every pinned person twice and left the
// difference between them — the whole point — resting on a label read once at
// the start. It rests on the chips now: a pin is solid and carries the button
// that takes it off, a drafted name is dashed because the next solve may not
// say it again.
//
// Nothing drafted here is interactive, and that is the design rather than an
// omission (ADR 0008): a hand placement made here would be destroyed by the
// next solve, so the durable way to say "put her there" is the pin beside it.
// List items rather than buttons, so nothing offers a keyboard user an action
// that does not exist.
function PlannedList({
  date,
  planned,
  colourOf,
  stale,
  onUnpin,
}: {
  date: string;
  planned: Planned[];
  colourOf: RoleColourOf;
  // True when an allocator input has moved under the drafted names and the
  // solve that answers for it has not come back yet. They are faded rather than
  // removed: what the solver last said is still the best guess on screen, and
  // blanking the rota for every pin would make editing feel like it broke
  // something. The pins do not fade with them — a pin is a decision, and it is
  // as true after the edit as it was before.
  stale: boolean;
  // Absent unless editing is on.
  onUnpin?: (pin: Preallocation) => void;
}) {
  return (
    // No heading on the row. "Draft" was the only thing it ever said and it is
    // the only thing an unallocated shift's names can be, so it was a word
    // spent saying where the reader already is. The list keeps a name for
    // anybody who cannot see where they are.
    <ul
      className="planned-list"
      aria-label={`Expected on ${formatShiftDateLong(date)}`}
    >
      {planned.map((entry, i) =>
        entry.kind === "pin" ? (
          <li
            key={entry.pin.id}
            className={`chip pinned ${entry.pin.custom ? "custom" : "volunteer"}`}
            data-role-colour={colourOf(entry.pin.role) ?? undefined}
            title={pinTitle(entry.pin)}
          >
            {entry.pin.name}
            {onUnpin && (
              <button
                type="button"
                className="chip-unpin"
                // The date is in the label because the button is a bare cross:
                // read out of the row's context, "Remove" alone does not say
                // which pin, and the rows all look alike.
                aria-label={`Remove ${entry.pin.name}'s pin on ${formatShiftDateLong(date)}`}
                onClick={() => onUnpin(entry.pin)}
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
        ) : (
          <li
            key={chipKey(date, entry.assignee, i)}
            className={`chip draft ${entry.assignee.custom ? "custom" : "volunteer"}${entry.assignee.group ? " has-group" : ""}${stale ? " draft-stale" : ""}`}
            data-role-colour={colourOf(entry.assignee.role) ?? undefined}
            title={draftTitle(entry.assignee)}
          >
            {entry.assignee.name}
            {entry.assignee.group && <GroupDot group={entry.assignee.group} />}
          </li>
        ),
      )}
    </ul>
  );
}

// ShiftNeeds shows the deficit for each role in a shifts draft.
//
// It is the row's warning as well as its arithmetic: a shift the solver could
// not fill is somebody to chase, and it looks like every other unallocated row
// until this line says otherwise. So it is worded and coloured as a warning,
// and the row it sits on is marked to match — the colour is the second signal,
// never the only one.
function ShiftNeeds({
  unfilledRoles,
}: {
  unfilledRoles: { role: string; deficit: number }[];
}) {
  if (!unfilledRoles.length) {
    return null;
  }
  return (
    <span className="shift-unfilled">
      Unfilled roles -{" "}
      {unfilledRoles
        .map(({ deficit, role }) => `${role}: ${deficit}`)
        .join(", ")}
    </span>
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
  drafted,
  draftSolved,
  draftStale,
  colourOf,
  selectedName,
  onSelectName,
  edit,
}: {
  shift: RotaShift;
  // Everyone already pinned to this shift. Only ever non-empty for an admin
  // looking at a shift whose rota has not been allocated.
  pins: Preallocation[];
  // Who the last solve put on this shift. Only ever non-empty on the same rows
  // as pins, and for the same reason: a draft only exists for a rota that has
  // not been allocated, and only an admin is shown one.
  drafted: Assignee[];
  draftSolved: boolean;
  draftStale: boolean;
  colourOf: RoleColourOf;
  selectedName: string;
  onSelectName?: (name: string) => void;
  edit: RowEdit | null;
}) {
  function handleClick(name: string) {
    onSelectName?.(name === selectedName ? "" : name);
  }

  const unallocated = isUnallocated(shift);
  // Whether the draft has anything to say about this row at all. Only an
  // unallocated shift has a draft — an allocated one is the rota, and a closed
  // one is asking nobody for anything — and only a solve that has been read
  // makes an empty draft mean "the solver could not fill this" rather than
  // "nobody has asked it yet". A Shape of nobody is left out too: a shift
  // asking for no one is not a full shift, it is one still to be stated.
  const judged = unallocated && draftSolved && shift.shape.length > 0;
  const unfilledRoles = judged ? shiftDeficit(shift.shape, drafted) : [];
  // Every Seat spoken for. Worth marking as much as the gaps are: what an admin
  // is doing on this screen is finding the rows that still need them, and a
  // green edge is what lets the eye skip one.
  const filled = judged && unfilledRoles.length === 0;
  const planned = plannedFor(pins, drafted);

  const placement = edit?.placement ?? null;
  const editable = placement !== null && isEditable(shift);
  const pending = placement?.pending ?? null;
  // The row someone was picked up from is not a destination: moving a person to
  // where they already are is a no-op the server would reject.
  const isSource = pending?.date === shift.date;
  const isDestination =
    editable && pending !== null && !isSource && placement.canReceive;

  const rowCls = [
    "shift-row",
    shift.closed ? "closed" : "",
    unallocated ? "unallocated" : "",
    unfilledRoles.length ? "unfilled" : "",
    filled ? "filled" : "",
    isDestination ? "drop-target" : "",
  ]
    .filter(Boolean)
    .join(" ");

  // The chip whose action menu is open, if it is one of this row's.
  const menuAssignee =
    (placement &&
      shift.assignees.find(
        (a, i) => chipKey(shift.date, a, i) === placement.openMenu,
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
        <ShiftNeeds unfilledRoles={unfilledRoles} />
        {planned.length > 0 && (
          <PlannedList
            date={shift.date}
            planned={planned}
            colourOf={colourOf}
            stale={draftStale}
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
            {/* Only on these rows: the branch this sits in is exactly the open,
                unallocated shifts, which is where a Shape can still change. An
                allocated rota was solved against its Shapes, and a closed shift
                is not asking anybody for anything. */}
            {edit.canEditShape && (
              <button
                type="button"
                className="shift-add shift-shape-edit"
                aria-label={`Change what ${formatShiftDateLong(shift.date)} asks for`}
                onClick={edit.onEditShape}
              >
                Shape
              </button>
            )}
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
            pending !== null && isDestination && placement.canSwapWith(a);
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
                        placement.canReceive,
                      )
              }
              onClick={
                pending === null
                  ? () =>
                      placement.onOpenMenu(
                        placement.openMenu === key ? null : key,
                      )
                  : () => placement.onSwapWith(a)
              }
              onDragStart={() => placement.onDragStart(a)}
              onDragEnd={placement.onDragEnd}
              onDrop={swappable ? () => placement.onSwapWith(a) : undefined}
            />
          );
        })}

        {editable && !pending && (
          <button
            type="button"
            className="shift-add"
            aria-label={`Add someone to ${formatShiftDateLong(shift.date)}`}
            onClick={placement.onAdd}
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
            onClick={placement.onMoveHere}
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
              placement.onMoveHere();
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

      {placement && menuAssignee && (
        <ChipMenu
          name={menuAssignee.name}
          onRemove={() => placement.onRemove(menuAssignee)}
          onReplace={() => placement.onReplace(menuAssignee)}
          onPickUp={() => placement.onPickUp(menuAssignee)}
          onClose={() => placement.onOpenMenu(null)}
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

// ShiftList draws a rota's shifts, one stacked row each: when the shift runs,
// and then whoever is on it — allocated, pinned or drafted — plus whatever this
// caller lets an admin change about it.
//
// It fetches nothing. Everything it draws arrives as props, which is what lets
// the rota page and the Allocation tab render the same rows from two quite
// different sets of hooks without either drifting from the other.
//
// The rows are mobile first, and they are used on an admin screen anyway, which
// `CLAUDE.md` otherwise asks to be designed for the desk. Two reasons, and the
// first is the one that matters: there is one component, so the two screens
// cannot show the same shift differently or be updated apart — which is the
// whole point of the extraction. The second is that these rows read better at a
// desk than the table they replaced on the Allocation tab: what an admin is
// doing there is preparing shifts one at a time, and a row per shift with its
// pins, its draft and its controls together is the shape of that job.
export default function ShiftList({
  shifts,
  pinsByDate,
  draftByShiftID = NO_DRAFT,
  draftSolved = false,
  draftStale = false,
  colourOf,
  selectedName = "",
  onSelectName,
  rowEdit,
}: {
  // The shifts to draw, in the order to draw them. Which shifts those are is
  // the caller's decision: the public rota hides the ones nobody has been
  // allocated to, and the Allocation tab shows only those.
  shifts: RotaShift[];
  // Who is pinned to each shift, keyed by date as the pins themselves are.
  pinsByDate: Map<string, Preallocation[]>;
  // Who the last solve put on each shift, keyed by shift id (ADR 0001). Absent
  // where the caller shows no draft at all — the rota page, which shows what has
  // been decided and leaves the solver's guess to the Allocation tab.
  draftByShiftID?: Map<string, Assignee[]>;
  // True when the draft above was read from a solve that succeeded, which is
  // what makes a shift with nothing drafted on it *unfilled* rather than merely
  // unasked. It cannot be read off the map: a shift the solver put nobody on is
  // left out of the draft entirely, so it looks from here exactly like a rota
  // nobody has solved.
  draftSolved?: boolean;
  // True while a fresh solve is owed to an edit that has already landed. Fades
  // the drafted names; nothing else about the rows changes.
  draftStale?: boolean;
  colourOf: RoleColourOf;
  // The name the reader has picked out of the rota, highlighted wherever it
  // appears. Absent where there is nothing to pick with — the Allocation tab
  // has no search, and its rows have nobody allocated to highlight.
  selectedName?: string;
  onSelectName?: (name: string) => void;
  // What an admin may change about a given row, or null where nothing can be
  // changed. A function of the shift rather than a flag, because what one row
  // offers depends on what is in flight elsewhere in the list.
  rowEdit?: ((shift: RotaShift) => RowEdit) | null;
}) {
  return (
    <div className="shift-list">
      {shifts.map((shift) => (
        <ShiftRow
          key={shift.id}
          shift={shift}
          pins={pinsByDate.get(shift.date) ?? []}
          drafted={draftByShiftID.get(shift.id) ?? []}
          draftSolved={draftSolved}
          draftStale={draftStale}
          colourOf={colourOf}
          selectedName={selectedName}
          onSelectName={onSelectName}
          edit={rowEdit ? rowEdit(shift) : null}
        />
      ))}
    </div>
  );
}
