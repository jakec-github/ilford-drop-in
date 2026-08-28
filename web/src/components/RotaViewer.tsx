import { useMemo, useState, useRef, useEffect, useCallback } from "react";
import { Link } from "wouter";
import type {
  Assignee,
  PersonRef,
  Preallocation,
  Role,
  RotaChange,
  RotaShift,
  Volunteer,
} from "../types";
import { TEAM_LEAD_ROLE } from "../types";
import { usePreallocations } from "../hooks/usePreallocations";
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
import type { Pending, RowEdit } from "./ShiftList";
import ShiftList from "./ShiftList";
import ShapeForm from "./ShapeForm";
import {
  formatShiftDate,
  formatShiftDateLong,
  isUnallocated,
  personRef,
  samePerson,
} from "./shifts";
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
  // Rewrites what one shift asks for. Frozen at allocation like a closure, and
  // for the same reason: the solver filled Seats against it.
  onSetShape: (
    shiftId: string,
    seats: { roleId: string; count: number }[],
  ) => Promise<void>;
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
      // Fully specified bar the reason and, when role is set, the role —
      // both of which the dialog collects.
      change: Omit<RotaChange, "reason" | "role">;
      // Offered only for a move: the Role the moved person takes on the
      // shift they land on. A swap keeps whoever it moves in the role they
      // already held, and a remove has nobody coming in, so neither needs
      // this — see askSwap and askRemove.
      role?: { initial: Role; leadTaken: boolean };
    }
  // Someone being pinned to, or unpinned from, a shift the rota has not been
  // run for. Not alterations: nothing is on the rota yet to alter.
  | { kind: "pin"; date: string }
  | { kind: "unpin"; pin: Preallocation }
  // A shift being shut or opened again, which is neither an alteration nor a
  // pin: it changes whether the drop-in runs that day at all.
  | { kind: "closure"; shift: RotaShift }
  // A shift's hours being moved, which may move the shift to another day.
  | { kind: "times"; shift: RotaShift }
  // What a shift asks for, which is neither of the above: it is what allocation
  // will try to fill, and it is fixed once allocation has.
  | { kind: "shape"; shift: RotaShift };

export default function RotaViewer({
  rotaShifts,
  isAdmin,
  onChange,
  onSetClosed,
  onSetTimes,
  onSetShape,
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
  // The Roles themselves, not only their colours: editing a Shape asks how many
  // of each Role a shift wants, so it needs the list and each Role's ceiling.
  const { roles, colourOf, idOf } = useRoles();

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

  // No draft is read here, deliberately. The drafted names used to be drawn on
  // the unallocated rows of this page, which made the rota two things at once:
  // what has been decided, and what the solver currently guesses. A draft is
  // neither published nor stable — the next solve may say something else — so it
  // belongs where it is worked on, on Admin → Allocation, and the notice below
  // sends an admin there. What stays here is what an admin can decide about a
  // shift the rota has not been run for: the pins, the closures and the Shape.
  //
  // Not reading it also takes the read's solve off this page: a draft read can
  // run a thirty-second CP-SAT solve (ADR 0008), which is a strange thing for
  // opening the rota to trigger.

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
    // The picker names a Role the way the roster spells it; a pin references
    // one by id. A name nothing answers to is a pin that cannot be made, and
    // saying so beats sending a reference the server would refuse.
    const roleId = idOf(role);
    return run(
      date,
      () =>
        roleId === null
          ? Promise.reject(new Error(`There is no role called ${role}`))
          : addPin({ date, person, roleId }),
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
    // A shift has one team-lead Seat, so the choice is only real where the
    // destination has none — same rule onAdd applies below, and the same
    // reason: which one shift's single lead is has been settled by someone
    // already there.
    const leadTaken = rotaShifts
      .find((s) => s.date === to)
      ?.assignees.some((a) => a.role === TEAM_LEAD_ROLE) ?? false;
    setDialog({
      kind: "confirm",
      title: `Move ${pending.name}?`,
      summary: `${pending.name} moves from ${formatShiftDateLong(pending.date)} to ${formatShiftDateLong(to)}.`,
      confirmLabel: "Move",
      change: { date: to, in: pending.person, swapDate: pending.date },
      role: { initial: pending.role, leadTaken },
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
      role: assignee.role,
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
      error: changeError?.date === shift.date ? changeError.message : null,
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
      canEditShape: roles !== null,
      onEditShape: () => {
        setChangeError(null);
        setOpenMenu(null);
        setDialog({ kind: "shape", shift });
      },
      // The rota page is the one screen with allocated shifts on it, so it is
      // the only one that offers moving people between them.
      placement: {
        pending,
        openMenu,
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
          Drag a name to another shift to move them, or onto another name to
          swap. On a touchscreen, tap a name to choose an action: move or
          swap, replace, or remove.
          {/* Only where there is a shift it applies to. On a rota that has all
              been allocated there is nothing to pin to, and the sentence would
              send an admin looking for a button that is not on any row. */}
          {hasUnallocated && (
            <>
              {" "}
              Shifts the rota has not been run for take pins instead: whoever
              you pin there is guaranteed the shift when it is allocated, and
              Shape changes how many places of each Role that shift has.
            </>
          )}
          {hasClosable && (
            <>
              {" "}
              Close one for a date the drop-in is not running, up until the rota
              is allocated.
            </>
          )}{" "}
          {/* Unconditional, unlike the two above: the times only describe the
              shift, so every row takes the edit whether or not it has been
              allocated. */}
          Select a row&rsquo;s date to change when the shift takes place.
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

      {/* Where the dashed rows are coming from and what is done about them.
          isAdmin as well as there being any, for the reason `editing` above is
          derived rather than trusted: losing the session takes the sentence
          away in the same render. */}
      {isAdmin && hasUnallocated && (
        <p className="rota-notice">
          The dashed shifts are the rota in flight — nobody has been placed on
          them yet. Pins, closures and what a shift asks for can be set here;
          the draft the solver makes of them, asking volunteers about it and
          allocating it all happen on{" "}
          <Link href="/admin/allocation">Admin &rarr; Allocation</Link>.
        </p>
      )}

      <ShiftList
        shifts={visibleShifts}
        pinsByDate={pinsByDate}
        colourOf={colourOf}
        selectedName={selectedName}
        onSelectName={setSelectedName}
        rowEdit={editing ? rowEdit : null}
      />

      {editing && dialog?.kind === "confirm" && (
        <ConfirmChangeDialog
          title={dialog.title}
          summary={dialog.summary}
          confirmLabel={dialog.confirmLabel}
          role={dialog.role}
          busy={saving}
          onCancel={() => {
            setDialog(null);
            setPending(null);
          }}
          onConfirm={(reason, role) =>
            void submit({ ...dialog.change, role, reason })
          }
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

      {/* Its own dialog rather than one of the confirm ones, and its errors are
          its own too: a refusal here names the Role whose ceiling was hit or
          the person pinned to a Seat that would go, and the form stays open on
          what was typed so the number can be corrected rather than retyped. */}
      {editing && dialog?.kind === "shape" && roles && (
        <ShapeForm
          title={`What does ${formatShiftDateLong(dialog.shift.date)} ask for?`}
          intro="How many places of each Role this shift has. It starts from the default shape and can differ from every other shift; leave a Role at 0 if this one does not need it."
          saveLabel="Save shape"
          roles={roles}
          shape={dialog.shift.shape}
          onSave={(seats) => onSetShape(dialog.shift.id, seats)}
          onClose={() => setDialog(null)}
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
