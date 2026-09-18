import { useState } from "react";
import Button from "../ui/Button";
import Dialog from "../ui/Dialog";
import type { Assignee, PersonRef, Role, Volunteer } from "../types";
import { CUSTOM_CHOICE } from "../types";
import type { SeatCount } from "./shape";
import { roleSuffix } from "./shifts";
import "./RotaEditDialogs.css";

// No Role is named anywhere in this file, and none may be (ADR 0009). Which
// Roles exist comes down from the server; how many of one a shift has comes
// from that shift's own Shape. Both arrive as props, because a dialog reads no
// hooks of its own.

// The Role to offer before whoever is editing says otherwise: the
// highest-priority one this volunteer holds that is actually on offer, which is
// right far more often than not — somebody is usually being put in for the job
// they mostly do. Falling back to the first option rather than to nothing keeps
// a picker answerable for a custom entry, or for someone the roster records no
// Role for at all.
function defaultRoleFor(
  volunteer: Volunteer | null | undefined,
  offered: Role[],
): Role {
  return (
    offered.find((role) => volunteer?.roles.includes(role)) ?? offered[0] ?? ""
  );
}

// The Roles a picker lists: every one the drop-in offers, plus the one the
// person already holds where that is no longer among them. A rota allocated
// before a Role was retired still names it, and dropping it would quietly move
// somebody into whatever came first.
function withHeldRole(offered: Role[], held: Role): Role[] {
  return held && !offered.includes(held) ? [held, ...offered] : offered;
}

// A Role as an option reads as its own name. The exception is the Role-less
// assignee — an allocation predating the role column — which has no name to
// read and says what it is instead.
function roleLabel(role: Role): string {
  return role || "No role";
}

// Sentences naming one or two Roles read better than a bare list, and a Shape
// of five Roles can leave several full at once.
function listRoles(roles: Role[]): string {
  if (roles.length <= 1) return roles.join("");
  return `${roles.slice(0, -1).join(", ")} and ${roles[roles.length - 1]}`;
}

// Every change to a published rota is recorded against a reason, so both
// dialogs below end in the same field. It is deliberately not pre-filled: a
// placeholder reason would be worse than none, since the cover record is the
// only account of why a rota stopped matching its allocation.
//
// Only a removal insists on one (issue #148). Every other change says what
// happened for itself — a replacement or a swap names who fills the place, an
// add makes the shift better off — where a removal leaves a gap nothing else
// accounts for. So the label marks the field optional wherever it is, rather
// than leaving the editor to infer that from the confirm button staying live.
function ReasonField({
  value,
  required,
  onChange,
}: {
  value: string;
  required: boolean;
  onChange: (value: string) => void;
}) {
  return (
    <label className="rota-edit-field">
      {required ? "Reason" : "Reason (optional)"}
      <input
        type="text"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder="e.g. away that week"
      />
    </label>
  );
}

function DialogActions({
  confirmLabel,
  busy,
  canConfirm,
  onCancel,
}: {
  confirmLabel: string;
  busy: boolean;
  canConfirm: boolean;
  onCancel: () => void;
}) {
  return (
    <div className="rota-edit-actions">
      <Button onClick={onCancel} disabled={busy}>
        Cancel
      </Button>
      <Button type="submit" disabled={!canConfirm || busy}>
        {busy ? "Saving…" : confirmLabel}
      </Button>
    </div>
  );
}

// ConfirmChangeDialog collects the reason for a change already described by
// picking chips — a remove, a move or a swap.
//
// role is offered only for a move: defaults to what the person already
// held, but can be changed to any Role the drop-in has. What the destination
// shift's Shape asks for does not narrow it — an alteration records what
// happened on the day rather than instructing a solve (ADR 0009).
export function ConfirmChangeDialog({
  title,
  summary,
  confirmLabel,
  role,
  reasonRequired,
  busy,
  onCancel,
  onConfirm,
}: {
  title: string;
  summary: string;
  confirmLabel: string;
  // Defaults the picker to what the person already held, and offers every
  // configured Role beside it. roles is null while they are still loading,
  // which leaves the picker showing the one Role that matters — the one being
  // carried — rather than blocking a move on a list it does not need.
  role?: { initial: Role; roles: Role[] | null };
  // True only for a remove, which leaves the shift short of someone — the
  // server refuses that one without a reason. A move and a swap put whoever
  // leaves on another shift, so both go through with none.
  reasonRequired: boolean;
  busy: boolean;
  onCancel: () => void;
  onConfirm: (reason: string, role?: Role) => void;
}) {
  const [reason, setReason] = useState("");
  const [chosenRole, setChosenRole] = useState<Role>(role?.initial ?? "");

  // The initial Role is always listed, even where it is no longer one the
  // drop-in offers and even where it is no Role at all: a move must be able to
  // leave somebody's Role exactly as it found it, and to put it back after a
  // look at the alternatives. The server accepts a move with no Role — it
  // carries across whatever they held — so "No role" is a real answer here
  // rather than an unfinished form.
  const offered = role?.roles ?? [];
  const options = role
    ? offered.includes(role.initial)
      ? offered
      : [role.initial, ...offered]
    : [];

  return (
    <Dialog title={title} onClose={onCancel}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          onConfirm(reason.trim(), role ? chosenRole : undefined);
        }}
      >
        <p className="rota-edit-summary">{summary}</p>

        {role && (
          <label className="rota-edit-field">
            Role
            <select
              value={chosenRole}
              onChange={(e) => setChosenRole(e.target.value as Role)}
            >
              {options.map((option) => (
                <option key={option} value={option}>
                  {roleLabel(option)}
                </option>
              ))}
            </select>
          </label>
        )}

        <ReasonField
          value={reason}
          required={reasonRequired}
          onChange={setReason}
        />
        <DialogActions
          confirmLabel={confirmLabel}
          busy={busy}
          canConfirm={!reasonRequired || reason.trim() !== ""}
          onCancel={onCancel}
        />
      </form>
    </Dialog>
  );
}

// What AssigneeDialog is being used for. Both cases pick a person, a Role and a
// reason; they differ only in where the Role starts.
export type AssigneeChange =
  | { kind: "add" }
  // Whoever comes in takes the outgoing person's place, role included, which is
  // what makes this a replacement rather than a removal followed by an add. It
  // is where the picker starts, not where it has to end: handing a Seat over to
  // somebody who will do a different job on it is an ordinary thing to record.
  | { kind: "replace"; outgoing: Assignee };

// AssigneeDialog picks who joins a shift and why — either alongside the people
// already on it, or in place of one of them.
//
// Role is stated rather than inferred: the service would otherwise settle it
// from the shift and the roster, and those rules are invisible from here. Every
// Role the drop-in has is on offer, whoever else on the shift already holds one
// and whatever the shift's Shape asks for — the Shape and the roster bind the
// allocator, not somebody recording a change to a rota that has already gone
// out (ADR 0009). The roster still decides the default, since somebody is
// usually put in for the job they mostly do.
export function AssigneeDialog({
  dateLabel,
  change,
  volunteers,
  volunteersError,
  roles,
  busy,
  onCancel,
  onConfirm,
}: {
  dateLabel: string;
  change: AssigneeChange;
  // null while the roster is still loading. Already filtered to the people who
  // can join this shift.
  volunteers: Volunteer[] | null;
  volunteersError: string | null;
  // Every Role the drop-in offers, highest priority first, or null while they
  // are still loading. A volunteer coming in must name one, so until this
  // arrives there is nothing to send and the dialog says so.
  roles: Role[] | null;
  busy: boolean;
  onCancel: () => void;
  // role is omitted for a custom entry, which the API gives no role to.
  onConfirm: (person: PersonRef, reason: string, role?: Role) => void;
}) {
  const [choice, setChoice] = useState("");
  const [customName, setCustomName] = useState("");
  // Empty until somebody picks one; what is actually offered is derived below,
  // so that changing who is coming in re-defaults the Role while an explicit
  // choice survives it.
  const [role, setRole] = useState<Role>("");
  const [reason, setReason] = useState("");

  const isCustom = choice === CUSTOM_CHOICE;
  const trimmedName = customName.trim();
  const person: PersonRef | null = isCustom
    ? trimmedName
      ? { custom: trimmedName }
      : null
    : choice
      ? { volunteerId: choice }
      : null;

  // A replacement carries the outgoing person's Role over, which is what makes
  // it a replacement; an add has none to carry. Either way it is listed even if
  // the drop-in has since retired it, so a rota allocated under the old name
  // can still be covered under it.
  const carried = change.kind === "replace" ? change.outgoing.role : "";
  const options = withHeldRole(roles ?? [], carried);

  const chosen = volunteers?.find((v) => v.id === choice) ?? null;
  // The carried Role wins where there is one; otherwise whatever this volunteer
  // mostly does. An explicit pick outranks both — it is only dropped if it
  // stops being an option, which nothing here does.
  const incomingRole: Role = options.includes(role)
    ? role
    : carried || defaultRoleFor(chosen, options);

  return (
    <Dialog
      title={
        change.kind === "add"
          ? `Add someone to ${dateLabel}`
          : `Replace ${change.outgoing.name}`
      }
      onClose={onCancel}
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (person)
            onConfirm(
              person,
              reason.trim(),
              isCustom ? undefined : incomingRole,
            );
        }}
      >
        {/* One request, not a removal and an add: the outgoing person leaves
            and the incoming one arrives together, so the shift is never
            briefly short-handed and the role passes straight across. */}
        {change.kind === "replace" && (
          <p className="rota-edit-summary">
            Whoever you choose takes {change.outgoing.name}&rsquo;s place on{" "}
            {dateLabel}
            {roleSuffix(change.outgoing.role)}.
          </p>
        )}

        <label className="rota-edit-field">
          Who
          <select
            value={choice}
            onChange={(e) => setChoice(e.target.value)}
            disabled={volunteers === null && volunteersError === null}
          >
            <option value="">
              {volunteers === null && volunteersError === null
                ? "Loading the roster…"
                : "Choose someone…"}
            </option>
            {volunteers?.map((v) => (
              <option key={v.id} value={v.id}>
                {v.fullName}
                {v.active ? "" : " (not active)"}
              </option>
            ))}
            <option value={CUSTOM_CHOICE}>Someone not on the roster…</option>
          </select>
        </label>

        {/* The roster failing is not a dead end: a custom entry needs nothing
            from it, so the picker degrades to that rather than to nothing. */}
        {volunteersError && (
          <p className="rota-edit-note">
            Could not load the roster ({volunteersError}). You can still add
            someone by name.
          </p>
        )}

        {isCustom && (
          <label className="rota-edit-field">
            Name
            <input
              type="text"
              value={customName}
              onChange={(e) => setCustomName(e.target.value)}
              placeholder="e.g. Redbridge youth group"
            />
          </label>
        )}

        {/* Not offered for a custom entry: the alterations API carries a role
            only for a real volunteer, so a choice here would be dropped
            silently. Offered on a replacement as well as an add, because
            handing a Seat to somebody who will do a different job on it is an
            ordinary thing to record — and because the person leaving may have
            no Role at all, which the API will not accept for the one arriving. */}
        {!isCustom && choice !== "" && options.length > 0 && (
          <label className="rota-edit-field">
            Role
            <select
              value={incomingRole}
              onChange={(e) => setRole(e.target.value as Role)}
            >
              {options.map((option) => (
                <option key={option} value={option}>
                  {roleLabel(option)}
                </option>
              ))}
            </select>
          </label>
        )}

        {/* Only reachable before the Roles have arrived, or on a deployment
            where nobody has made any. Either way there is nothing to send: the
            API refuses a volunteer coming in without a Role. */}
        {!isCustom && choice !== "" && options.length === 0 && (
          <p className="rota-edit-note">
            {roles === null
              ? "Still loading the roles…"
              : "There are no roles to put anyone in yet. An organiser makes them on Admin → Settings."}
          </p>
        )}

        {/* Never required here: both an add and a replacement leave the shift
            with at least as many people as it had, so there is no gap for a
            reason to account for (issue #148). */}
        <ReasonField value={reason} required={false} onChange={setReason} />
        <DialogActions
          confirmLabel={change.kind === "add" ? "Add" : "Replace"}
          busy={busy}
          // A volunteer arriving has to name a Role; a custom entry never
          // carries one, so it is answerable on the name alone.
          canConfirm={person !== null && (isCustom || incomingRole !== "")}
          onCancel={onCancel}
        />
      </form>
    </Dialog>
  );
}

// PinDialog picks someone to pin to a shift the rota has not been run for yet.
//
// No reason field, unlike every dialog above it. Those record a change to a
// published rota, which contradicts something volunteers have already been
// told; a pin is an instruction to an allocation that has not happened, so
// there is nothing to account for.
//
// Unlike the dialogs above it, this one is held to every allocator rule, because
// a pin is an instruction to a solve that has not run yet (ADR 0009). Two rules
// settle which Roles it may offer: the roster says which a volunteer may be
// promised, and the shift's own Shape says how many Seats of each are left. The
// API enforces both, so offering anything else would be offering a refusal.
export function PinDialog({
  dateLabel,
  volunteers,
  volunteersError,
  seats,
  pinnedNames,
  busy,
  onCancel,
  onConfirm,
}: {
  dateLabel: string;
  // null while the roster is still loading. Already filtered to the people who
  // can be pinned to this shift.
  volunteers: Volunteer[] | null;
  volunteersError: string | null;
  // This shift's Shape with its pins counted against it, in the order the Seats
  // are filled. Empty for a shift asking for nobody, where there is nothing to
  // promise anyone — the way out of both that and a full Role is to edit the
  // Shape or remove a pin, and an unallocated shift allows either (issue #131).
  seats: SeatCount[];
  // Everyone already pinned here, by the name shown. Repeating a name is
  // allowed for a custom entry (issue #195) — an organisation sending two
  // people is two pins reading alike — so this is only ever a note.
  pinnedNames: string[];
  busy: boolean;
  onCancel: () => void;
  onConfirm: (person: PersonRef, role: Role) => void;
}) {
  const [choice, setChoice] = useState("");
  const [customName, setCustomName] = useState("");
  // Empty until somebody picks one; what is actually on offer depends on who is
  // being pinned, and is derived below.
  const [role, setRole] = useState<Role>("");

  const isCustom = choice === CUSTOM_CHOICE;
  const trimmedName = customName.trim();
  const repeatedName = isCustom && pinnedNames.includes(trimmedName);
  const person: PersonRef | null = isCustom
    ? trimmedName
      ? { custom: trimmedName }
      : null
    : choice
      ? { volunteerId: choice }
      : null;

  const chosen = volunteers?.find((v) => v.id === choice) ?? null;
  // Which of this shift's Seats this person could be promised. A custom entry
  // is an outside provider — nothing records what it can do and the API asks
  // nothing either, so every Seat the shift has is open to it (issue #91). A
  // volunteer is held to the roster, which is what the API checks.
  const theirs = seats.filter(
    (seat) => isCustom || (chosen?.roles.includes(seat.role) ?? false),
  );
  const offered = theirs.filter((seat) => seat.taken < seat.seats);
  const full = theirs.filter((seat) => seat.taken >= seat.seats);

  const options = offered.map((seat) => seat.role);
  // An explicit pick stands while it is still on offer; otherwise the
  // highest-priority Seat they could fill, the Shape's order being the order
  // Seats are filled in. Changing who is being pinned therefore re-defaults the
  // Role, and picking a Role that person cannot fill is not representable.
  const pinnedRole = options.includes(role) ? role : (options[0] ?? "");

  // Answering the Who field is what makes the Seats below mean anything — a
  // custom entry's name can still be blank, since what may be promised to an
  // outside group does not depend on what it is called.
  const someone = choice !== "";
  const them = isCustom ? "an outside group" : (chosen?.name ?? "they");

  // Why a Seat this person might have expected is not on offer. Every case has
  // a way through, because an unallocated shift's Shape can still be changed
  // and any pin on it can still be removed (issue #131) — so each says which.
  let note: string | null = null;
  if (!someone) {
    note = null;
  } else if (options.length > 0 && full.length > 0) {
    note = `${dateLabel} has every ${listRoles(full.map((seat) => seat.role))} seat pinned already.`;
  } else if (full.length > 0) {
    const gone = listRoles(full.map((seat) => seat.role));
    note = isCustom
      ? `${dateLabel} is pinned full: every seat it asks for is spoken for. Remove one of those pins, or give the shift another seat.`
      : `${dateLabel} has every ${gone} seat pinned already, and that is all ${them} could fill here. Remove one of those pins, or give the shift another seat.`;
  } else if (seats.length === 0) {
    note = `${dateLabel} does not ask for anybody yet. Say what the shift asks for to make a seat.`;
  } else if (options.length === 0) {
    note = `${dateLabel} does not ask for anything ${them} does. Change what the shift asks for to make a seat.`;
  }

  return (
    <Dialog title={`Pin someone to ${dateLabel}`} onClose={onCancel}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (person && pinnedRole) onConfirm(person, pinnedRole);
        }}
      >
        <p className="rota-edit-summary">
          Whoever you pin is guaranteed this shift when the rota is allocated.
        </p>

        <label className="rota-edit-field">
          Who
          <select
            value={choice}
            onChange={(e) => setChoice(e.target.value)}
            disabled={volunteers === null && volunteersError === null}
          >
            <option value="">
              {volunteers === null && volunteersError === null
                ? "Loading the roster…"
                : "Choose someone…"}
            </option>
            {volunteers?.map((v) => (
              <option key={v.id} value={v.id}>
                {v.fullName}
              </option>
            ))}
            <option value={CUSTOM_CHOICE}>Someone not on the roster…</option>
          </select>
        </label>

        {/* The roster failing is not a dead end: a custom entry needs nothing
            from it, so the picker degrades to that rather than to nothing. */}
        {volunteersError && (
          <p className="rota-edit-note">
            Could not load the roster ({volunteersError}). You can still pin
            someone by name.
          </p>
        )}

        {isCustom && (
          <label className="rota-edit-field">
            Name
            <input
              type="text"
              value={customName}
              onChange={(e) => setCustomName(e.target.value)}
              placeholder="e.g. Redbridge youth group"
            />
          </label>
        )}

        {/* A note, not a refusal: pinning a name twice is how the editor says an
            organisation is sending two people (issue #195). Worth saying,
            because the other reason to be typing it is a slip. */}
        {repeatedName && (
          <p className="rota-edit-note">
            {trimmedName} is already pinned to {dateLabel}. Pinning the name
            again promises a second person from them.
          </p>
        )}

        {options.length > 0 && (
          <label className="rota-edit-field">
            Role
            <select
              value={pinnedRole}
              onChange={(e) => setRole(e.target.value as Role)}
            >
              {options.map((option) => (
                <option key={option} value={option}>
                  {option}
                </option>
              ))}
            </select>
          </label>
        )}

        {note && <p className="rota-edit-note">{note}</p>}

        <DialogActions
          confirmLabel="Pin"
          busy={busy}
          // Every pin names the Seat it fills, so there is nothing to send
          // until one is on offer.
          canConfirm={person !== null && pinnedRole !== ""}
          onCancel={onCancel}
        />
      </form>
    </Dialog>
  );
}

// ClosureDialog confirms shutting a shift or opening it back up. Both
// directions go through one dialog because they are one decision seen from
// either side, and both say the same thing: the drop-in either runs that day or
// it does not, and allocation will be worked out accordingly.
//
// Closing is worth confirming despite being reversible, because of what it
// takes away from the row: anyone pinned there is set aside until it reopens,
// which the summary says out loud rather than leaving as a surprise.
export function ClosureDialog({
  dateLabel,
  closing,
  pinnedCount,
  busy,
  onCancel,
  onConfirm,
}: {
  dateLabel: string;
  // True when the shift is being shut, false when it is being reopened.
  closing: boolean;
  // How many people are pinned to it, from either source. Only mentioned when
  // closing, and only when there are any.
  pinnedCount: number;
  busy: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return (
    <Dialog
      title={closing ? `Close ${dateLabel}?` : `Reopen ${dateLabel}?`}
      onClose={onCancel}
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          onConfirm();
        }}
      >
        <p className="rota-edit-summary">
          {closing ? (
            <>
              The drop-in does not run on {dateLabel}, so nobody will be
              allocated there.
              {pinnedCount > 0 && (
                <>
                  {" "}
                  {pinnedCount === 1
                    ? "The person"
                    : `All ${pinnedCount} people`}{" "}
                  pinned there {pinnedCount === 1 ? "is" : "are"} set aside, and
                  comes back if you reopen it.
                </>
              )}
            </>
          ) : (
            <>
              The drop-in runs on {dateLabel} again, and allocation will fill it
              like any other shift.
            </>
          )}
        </p>
        <DialogActions
          confirmLabel={closing ? "Close shift" : "Reopen shift"}
          busy={busy}
          canConfirm
          onCancel={onCancel}
        />
      </form>
    </Dialog>
  );
}

// UnpinDialog confirms taking one manual pin off a shift. Removing a pin is not
// removing anyone from the rota — nobody has been allocated yet — so the
// summary says what is actually being given up: the guarantee, not the shift.
// A datetime-local field carries minutes, not seconds, so a shift's stored
// "2026-02-02T19:30:00" has to lose its tail to land in one. What comes back is
// the shorter spelling, which the API also accepts.
function toLocalInput(timestamp: string): string {
  return timestamp.slice(0, "2026-02-02T19:30".length);
}

// ShiftTimesDialog moves one shift's start and end.
//
// The fields carry a date as well as a time, which is not decoration: a shift's
// date *is* the date of its start, so moving the start to another day moves the
// shift there. Two shifts cannot share a day, and where that is what an edit
// would do the server refuses it and names the day — which is why nothing is
// checked here beyond the end following the start.
//
// Unlike closing a shift, this stays available after the rota has been
// allocated. The times describe when to turn up; the solver worked in dates.
export function ShiftTimesDialog({
  dateLabel,
  start,
  end,
  busy,
  onCancel,
  onConfirm,
}: {
  dateLabel: string;
  // The shift's current times, local wall-clock, as the API spells them.
  start: string;
  end: string;
  busy: boolean;
  onCancel: () => void;
  onConfirm: (start: string, end: string) => void;
}) {
  const [from, setFrom] = useState(toLocalInput(start));
  const [to, setTo] = useState(toLocalInput(end));

  const stated = from !== "" && to !== "";
  const backwards = stated && to <= from;

  return (
    <Dialog title={`When does ${dateLabel} run?`} onClose={onCancel}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          onConfirm(from, to);
        }}
      >
        <label className="rota-edit-field">
          Starts
          <input
            type="datetime-local"
            value={from}
            onChange={(e) => setFrom(e.target.value)}
          />
        </label>
        <label className="rota-edit-field">
          Ends
          <input
            type="datetime-local"
            value={to}
            onChange={(e) => setTo(e.target.value)}
          />
        </label>
        <p className="rota-edit-note">
          {backwards
            ? "A shift has to end after it starts."
            : "Times are the drop-in's own local time. Moving the start to another day moves the shift to that day."}
        </p>
        <DialogActions
          confirmLabel="Save times"
          busy={busy}
          canConfirm={stated && !backwards}
          onCancel={onCancel}
        />
      </form>
    </Dialog>
  );
}

export function UnpinDialog({
  name,
  dateLabel,
  busy,
  onCancel,
  onConfirm,
}: {
  name: string;
  dateLabel: string;
  busy: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return (
    <Dialog title={`Remove pin for ${name}?`} onClose={onCancel}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          onConfirm();
        }}
      >
        <p className="rota-edit-summary">
          {name} is no longer guaranteed {dateLabel}. They can still be
          allocated there when the rota is run.
        </p>
        <DialogActions
          confirmLabel="Remove pin"
          busy={busy}
          canConfirm
          onCancel={onCancel}
        />
      </form>
    </Dialog>
  );
}
