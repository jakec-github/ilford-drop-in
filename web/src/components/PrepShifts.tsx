import { useMemo, useState } from "react";
import type { ReactNode } from "react";
import type {
  Assignee,
  PersonRef,
  Preallocation,
  Role,
  RotaShift,
} from "../types";
import { SERVICE_VOLUNTEER_ROLE, TEAM_LEAD_ROLE } from "../types";
import { usePreallocations } from "../hooks/usePreallocations";
import type { RoleColourOf } from "../hooks/useRoles";
import { useRoles } from "../hooks/useRoles";
import { useVolunteers } from "../hooks/useVolunteers";
import {
  ClosureDialog,
  PinDialog,
  ShiftTimesDialog,
  UnpinDialog,
} from "./RotaEditDialogs";
import ShapeForm from "./ShapeForm";
import { describeShape } from "./shape";
import { formatShiftTimes } from "./shiftTimes";
import "./PrepShifts.css";

// "Sun 2 Aug" — the weekday earns its place on this screen, where what an admin
// is checking is that the shifts landed on the evenings they meant.
function formatShiftDate(dateStr: string): string {
  return new Date(dateStr).toLocaleDateString("en-GB", {
    weekday: "short",
    day: "numeric",
    month: "short",
  });
}

// One name in the Pinned or Draft column. Dashed and role-coloured, because
// neither is on the rota: a pin is a promise an admin made, a drafted Seat is a
// guess the solver made and will make again. What tells them apart is the column
// they are in, not the way they are drawn.
function PencilChip({
  name,
  role,
  custom,
  colourOf,
  title,
  children,
}: {
  name: string;
  role: Role;
  custom: boolean;
  colourOf: RoleColourOf;
  title: string;
  // The one thing that can be done to this name, if anything can. Only pins
  // have one.
  children?: ReactNode;
}) {
  return (
    <li
      className={`prep-chip ${custom ? "custom" : "volunteer"}`}
      // The Role's palette token rather than its name: role names are
      // configuration, so `role-${name}` would mint class names no stylesheet
      // has a rule for. A Role the server does not name gets no attribute and
      // the chip's own default.
      data-role-colour={colourOf(role) ?? undefined}
      title={title}
    >
      {name}
      {children}
    </li>
  );
}

// What a pin means. There is one kind, whether an admin made it here or a
// Standing Preallocation seeded it when the rota was defined.
function pinTitle(pin: Preallocation): string {
  // Naming the uncapped Role would be noise — being pinned to a shift already
  // means being pinned to one of its ordinary Seats.
  const role =
    pin.role === SERVICE_VOLUNTEER_ROLE ? "" : ` as ${pin.role.toLowerCase()}`;
  return `${pin.name} is pinned${role} to this shift, and will be placed here when the rota is allocated.`;
}

function draftTitle(assignee: Assignee): string {
  const role =
    assignee.role === SERVICE_VOLUNTEER_ROLE
      ? ""
      : ` as ${assignee.role.toLowerCase()}`;
  return `The last solve put ${assignee.name} here${role}. It is a draft, not a placement, and the next solve may put somebody else here.`;
}

// What this screen can open. Every one of them acts on a single shift, which is
// why they are one union rather than a flag each: only one is ever up.
type PrepDialog =
  | { kind: "pin"; shift: RotaShift }
  | { kind: "unpin"; pin: Preallocation }
  | { kind: "closure"; shift: RotaShift }
  | { kind: "times"; shift: RotaShift }
  | { kind: "shape"; shift: RotaShift };

function ShiftRow({
  shift,
  pins,
  drafted,
  colourOf,
  canEditShape,
  error,
  onOpen,
  onUnpin,
}: {
  shift: RotaShift;
  pins: Preallocation[];
  drafted: Assignee[];
  colourOf: RoleColourOf;
  // False while the Roles are still loading: the Shape form asks how many of
  // each Role a shift wants, and there is nothing to ask about until they land.
  canEditShape: boolean;
  // The server's message from a change refused against this shift.
  error: string | null;
  onOpen: (dialog: PrepDialog) => void;
  onUnpin: (pin: Preallocation) => void;
}) {
  const date = formatShiftDate(shift.date);

  return (
    <>
      <tr className={shift.closed ? "prep-row prep-row--closed" : "prep-row"}>
        <th scope="row" className="prep-when">
          <span className="prep-date">{date}</span>
          <span className="prep-time">
            {formatShiftTimes(shift.start, shift.end)}
          </span>
        </th>

        {shift.closed ? (
          // Nothing to ask for, pin or draft on a date the drop-in is not
          // running, so the three columns become one sentence rather than three
          // empty cells to read past.
          <td className="prep-closed" colSpan={3}>
            Closed — the drop-in is not running, and allocation will skip it.
          </td>
        ) : (
          <>
            <td className="prep-asks">
              {shift.shape.length > 0 ? (
                describeShape(shift.shape)
              ) : (
                <span className="prep-empty">
                  Nobody — the rota cannot be allocated until it asks for
                  somebody
                </span>
              )}
            </td>

            <td className="prep-pins">
              {pins.length === 0 ? (
                <span className="prep-none">—</span>
              ) : (
                <ul className="prep-chips">
                  {pins.map((pin) => (
                    <PencilChip
                      key={pin.id}
                      name={pin.name}
                      role={pin.role}
                      custom={pin.custom}
                      colourOf={colourOf}
                      title={pinTitle(pin)}
                    >
                      <button
                        type="button"
                        className="prep-unpin"
                        // The date is in the label because the button is a bare
                        // cross: read out of the row's context, "Remove" alone
                        // does not say which pin, and the rows all look alike.
                        aria-label={`Remove ${pin.name}'s pin on ${date}`}
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
                    </PencilChip>
                  ))}
                </ul>
              )}
            </td>

            <td className="prep-draft">
              {drafted.length === 0 ? (
                <span className="prep-none">—</span>
              ) : (
                <ul className="prep-chips">
                  {drafted.map((a, i) => (
                    <PencilChip
                      // The same custom entry can legitimately appear twice on
                      // one shift — two people from one visiting group — so the
                      // index is part of the key.
                      key={`${a.volunteerId ?? a.name}/${i}`}
                      name={a.name}
                      role={a.role}
                      custom={a.custom}
                      colourOf={colourOf}
                      title={draftTitle(a)}
                    />
                  ))}
                </ul>
              )}
            </td>
          </>
        )}

        <td className="prep-actions">
          {/* A closed shift takes neither pins nor a Shape: there is nobody to
              promise it to and nothing for it to ask for. Its hours and its
              closure are all that is left to change. */}
          {!shift.closed && (
            <>
              <button
                type="button"
                className="prep-action"
                onClick={() => onOpen({ kind: "pin", shift })}
              >
                Pin
              </button>
              {canEditShape && (
                <button
                  type="button"
                  className="prep-action"
                  onClick={() => onOpen({ kind: "shape", shift })}
                >
                  Shape
                </button>
              )}
            </>
          )}
          <button
            type="button"
            className="prep-action"
            onClick={() => onOpen({ kind: "times", shift })}
          >
            Times
          </button>
          <button
            type="button"
            className="prep-action"
            onClick={() => onOpen({ kind: "closure", shift })}
          >
            {shift.closed ? "Reopen" : "Close"}
          </button>
        </td>
      </tr>

      {error && (
        <tr className="prep-row-error">
          <td colSpan={5} role="alert">
            {error}
          </td>
        </tr>
      )}
    </>
  );
}

// PrepShifts is the rota in flight, shift by shift: when each one runs, what it
// asks for, who is promised it, who the last solve put on it, and everything
// about it that can still be changed.
//
// A table rather than the rota page's stacked rows. This is an admin tool, read
// at a desk, and what it is for is comparing six shifts against each other —
// which of them is short, which has nobody pinned, which one somebody closed.
// The rota page answers a different question (when am I on?) for a different
// reader on a phone, which is why both exist.
//
// Nothing here is gated on anything else (issue #145): the Shape, the pins and
// the hours all move while the answers are coming in, and the draft below
// re-solves around them.
export default function PrepShifts({
  shifts,
  draftByShiftID,
  onSetClosed,
  onSetTimes,
  onSetShape,
}: {
  // The rota in flight's shifts, in date order.
  shifts: RotaShift[];
  // Who the last solve put on each shift, keyed by shift id (ADR 0001). Empty
  // where there is no draft yet.
  draftByShiftID: Map<string, Assignee[]>;
  onSetClosed: (shiftId: string, closed: boolean) => Promise<void>;
  onSetTimes: (shiftId: string, start: string, end: string) => Promise<void>;
  onSetShape: (
    shiftId: string,
    seats: { roleId: string; count: number }[],
  ) => Promise<void>;
}) {
  const [dialog, setDialog] = useState<PrepDialog | null>(null);
  const [saving, setSaving] = useState(false);
  const [changeError, setChangeError] = useState<{
    shiftId: string;
    message: string;
  } | null>(null);

  const { roles, colourOf } = useRoles();
  const { volunteers, error: volunteersError } = useVolunteers();
  const {
    preallocations,
    error: preallocationsError,
    addPin,
    removePin,
  } = usePreallocations();

  const pinsByDate = useMemo(() => {
    const byDate = new Map<string, Preallocation[]>();
    for (const pin of preallocations ?? []) {
      const forDate = byDate.get(pin.date);
      if (forDate) forDate.push(pin);
      else byDate.set(pin.date, [pin]);
    }
    return byDate;
  }, [preallocations]);

  // Who can still be pinned to a shift: the active roster, less anyone already
  // pinned there. Both halves matter — the server refuses a pin for an inactive
  // volunteer, and a repeat of one that already exists.
  function pinnableTo(date: string) {
    if (volunteers === null) return null;
    const pinned = new Set(
      (pinsByDate.get(date) ?? []).map((p) => p.volunteerId).filter(Boolean),
    );
    return volunteers.filter((v) => v.active && !pinned.has(v.id));
  }

  // Fires one change against one shift and leaves the screen showing whatever
  // the server now says. A refusal is not a failure of the app: the message
  // explains what the change contradicts, so it is shown against the row it was
  // refused for and nothing is rolled back — nothing was applied.
  async function run(
    shiftId: string,
    apply: () => Promise<void>,
    fallback: string,
  ) {
    setSaving(true);
    try {
      await apply();
      setChangeError(null);
    } catch (err) {
      setChangeError({
        shiftId,
        message: err instanceof Error ? err.message : fallback,
      });
    } finally {
      setSaving(false);
      setDialog(null);
    }
  }

  function submitPin(shift: RotaShift, person: PersonRef, role: Role) {
    return run(
      shift.id,
      () => addPin({ date: shift.date, person, role }),
      "The pin was not saved",
    );
  }

  function submitUnpin(pin: Preallocation) {
    // Only ever called for a pin the listing gave an id, which is all of them.
    if (pin.id === null) return;
    const id = pin.id;
    const shift = shifts.find((s) => s.date === pin.date);
    return run(
      shift?.id ?? "",
      () => removePin(id),
      "The pin was not removed",
    );
  }

  return (
    <section className="admin-panel prep-shifts">
      <h2>Shifts</h2>

      <p className="prep-intro">
        Every shift of the rota in flight. What each one asks for, who is
        already promised it and when it runs can all be changed until the rota
        is allocated; the hours stay editable afterwards, because they describe
        the shift rather than feed the solver.
      </p>

      {/* A failed pin load leaves the column looking empty when it may not be,
          so it is said out loud rather than swallowed. The rest of the table
          still reads. */}
      {preallocationsError && (
        <p className="prep-notice" role="alert">
          Could not load who is pinned: {preallocationsError}
        </p>
      )}

      {shifts.length === 0 ? (
        <p className="prep-intro">This rota has no shifts.</p>
      ) : (
        <div className="prep-table-wrap">
          <table className="prep-table">
            <thead>
              <tr>
                <th scope="col">When</th>
                <th scope="col">Asks for</th>
                <th scope="col">Pinned</th>
                <th scope="col">Draft</th>
                <th scope="col">
                  <span className="prep-sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {shifts.map((shift) => (
                <ShiftRow
                  key={shift.id}
                  shift={shift}
                  pins={pinsByDate.get(shift.date) ?? []}
                  drafted={draftByShiftID.get(shift.id) ?? []}
                  colourOf={colourOf}
                  canEditShape={roles !== null}
                  error={
                    changeError?.shiftId === shift.id
                      ? changeError.message
                      : null
                  }
                  onOpen={(next) => {
                    setChangeError(null);
                    setDialog(next);
                  }}
                  onUnpin={(pin) => {
                    setChangeError(null);
                    setDialog({ kind: "unpin", pin });
                  }}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}

      {dialog?.kind === "pin" && (
        <PinDialog
          dateLabel={formatShiftDate(dialog.shift.date)}
          volunteers={pinnableTo(dialog.shift.date)}
          volunteersError={volunteersError}
          // A shift has one team-lead Seat, so a lead already pinned there
          // rules out a second — and it can be given up from here, whichever
          // way it came to be made.
          leadPinned={(pinsByDate.get(dialog.shift.date) ?? []).some(
            (p) => p.role === TEAM_LEAD_ROLE,
          )}
          pinnedNames={(pinsByDate.get(dialog.shift.date) ?? []).map(
            (p) => p.name,
          )}
          busy={saving}
          onCancel={() => setDialog(null)}
          onConfirm={(person, role) =>
            void submitPin(dialog.shift, person, role)
          }
        />
      )}

      {dialog?.kind === "unpin" && (
        <UnpinDialog
          name={dialog.pin.name}
          dateLabel={formatShiftDate(dialog.pin.date)}
          busy={saving}
          onCancel={() => setDialog(null)}
          onConfirm={() => void submitUnpin(dialog.pin)}
        />
      )}

      {dialog?.kind === "closure" && (
        <ClosureDialog
          dateLabel={formatShiftDate(dialog.shift.date)}
          closing={!dialog.shift.closed}
          pinnedCount={(pinsByDate.get(dialog.shift.date) ?? []).length}
          busy={saving}
          onCancel={() => setDialog(null)}
          onConfirm={() =>
            void run(
              dialog.shift.id,
              () => onSetClosed(dialog.shift.id, !dialog.shift.closed),
              dialog.shift.closed
                ? "The shift was not reopened"
                : "The shift was not closed",
            )
          }
        />
      )}

      {dialog?.kind === "times" && (
        <ShiftTimesDialog
          dateLabel={formatShiftDate(dialog.shift.date)}
          start={dialog.shift.start}
          end={dialog.shift.end}
          busy={saving}
          onCancel={() => setDialog(null)}
          onConfirm={(start, end) =>
            void run(
              dialog.shift.id,
              () => onSetTimes(dialog.shift.id, start, end),
              "The shift times were not saved",
            )
          }
        />
      )}

      {/* Its own errors rather than the row's: a refusal here names the Role
          whose ceiling was hit or the person pinned to a Seat that would go, and
          the form stays open on what was typed so the number can be corrected
          rather than retyped. */}
      {dialog?.kind === "shape" && roles && (
        <ShapeForm
          title={`What does ${formatShiftDate(dialog.shift.date)} ask for?`}
          intro="How many places of each Role this shift has. It starts from the default shape and can differ from every other shift; leave a Role at 0 if this one does not need it."
          saveLabel="Save shape"
          roles={roles}
          shape={dialog.shift.shape}
          onSave={(seats) => onSetShape(dialog.shift.id, seats)}
          onClose={() => setDialog(null)}
        />
      )}
    </section>
  );
}
