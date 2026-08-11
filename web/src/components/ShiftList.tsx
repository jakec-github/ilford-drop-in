import { useEffect, useRef } from "react";
import type { Assignee, PersonRef, Preallocation, RotaShift } from "../types";
import { SERVICE_VOLUNTEER_ROLE } from "../types";
import type { RoleColourOf } from "../hooks/useRoles";
import Button from "../ui/Button";
import { describeShape } from "./shape";
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

// DraftList shows who the last solve put on a shift it has not been allocated
// for. Chips, because a drafted name is the same kind of thing as an allocated
// one — dashed, because it is pencilled in.
//
// Nothing here is interactive, and that is the design rather than an omission
// (ADR 0008): a hand placement made here would be destroyed by the next solve,
// so the durable way to say "put her there" is the pin affordance on the same
// row. List items rather than buttons, so nothing offers a keyboard user an
// action that does not exist.
function DraftList({
  date,
  assignees,
  colourOf,
  stale,
}: {
  date: string;
  assignees: Assignee[];
  colourOf: RoleColourOf;
  // True when an allocator input has moved under these names and the solve that
  // answers for it has not come back yet. They are faded rather than removed:
  // what the solver last said is still the best guess on screen, and blanking
  // the rota for every pin would make editing feel like it broke something.
  stale: boolean;
}) {
  return (
    <div className={stale ? "prealloc prealloc-stale" : "prealloc"}>
      <span className="prealloc-label" id={`draft-${date}`}>
        Draft:
      </span>
      <ul className="prealloc-list" aria-labelledby={`draft-${date}`}>
        {assignees.map((a, i) => (
          <li
            key={chipKey(date, a, i)}
            className={`chip draft ${a.custom ? "custom" : "volunteer"}${a.group ? " has-group" : ""}`}
            data-role-colour={colourOf(a.role) ?? undefined}
            title={draftTitle(a)}
          >
            {a.name}
            {a.group && <GroupDot group={a.group} />}
          </li>
        ))}
      </ul>
    </div>
  );
}

// ShiftAsksFor says what an unallocated shift is waiting to be filled with.
// Worth a line of its own on those rows because it is the one thing about them
// an admin can still change and cannot see anywhere else — and because a shift
// asking for nobody looks exactly like every other unallocated row until it
// stops the rota being allocated, so it says so here instead.
function ShiftAsksFor({ shape }: { shape: RotaShift["shape"] }) {
  return (
    <span className="shift-shape">
      {shape.length > 0
        ? `Asks for ${describeShape(shape)}`
        : "Asks for nobody — the rota cannot be allocated until it does"}
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
        <span className="shift-note">Not yet allocated</span>
        <ShiftAsksFor shape={shift.shape} />
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
        {/* Under the pins, because it contains them: a pin is honoured by every
            solve, so whoever is pinned above is drafted here too. The order
            reads as the promise first and the guess built on it second. */}
        {drafted.length > 0 && (
          <DraftList
            date={shift.date}
            assignees={drafted}
            colourOf={colourOf}
            stale={draftStale}
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
  draftByShiftID,
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
  // Who the last solve put on each shift, keyed by shift id (ADR 0001). Empty
  // where there is no draft, which is every rota a non-admin is looking at.
  draftByShiftID: Map<string, Assignee[]>;
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
