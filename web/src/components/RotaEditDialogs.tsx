import { useState } from "react";
import Button from "../ui/Button";
import Dialog from "../ui/Dialog";
import type {
  Assignee,
  PersonRef,
  Role,
  Volunteer,
} from "../types";
import { CUSTOM_CHOICE, SERVICE_VOLUNTEER_ROLE, TEAM_LEAD_ROLE } from "../types";
import "./RotaEditDialogs.css";

// The Role to offer for a volunteer before the admin says otherwise: the
// highest-priority one they hold, which the API lists first. It is the right
// answer far more often than not — pinning a team lead is nearly always pinning
// them to lead — and the admin can still choose the other.
function defaultRoleFor(volunteer: Volunteer | null | undefined): Role {
  return volunteer?.roles[0] ?? SERVICE_VOLUNTEER_ROLE;
}

// How a Role reads in a sentence about somebody's place on a shift. The uncapped
// Role is what being on the shift already means, so naming it would be noise;
// anything else is worth saying.
function roleSuffix(role: Role): string {
  return role === SERVICE_VOLUNTEER_ROLE ? "" : `, as ${role.toLowerCase()}`;
}

// Every change to a published rota is recorded against a reason, so both
// dialogs below end in the same field. It is deliberately not pre-filled: a
// placeholder reason would be worse than none, since the cover record is the
// only account of why a rota stopped matching its allocation.
function ReasonField({
  value,
  onChange,
}: {
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <label className="rota-edit-field">
      Reason
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

// ConfirmChangeDialog collects the reason for a change the admin has already
// described by picking chips — a remove, a move or a swap. The summary spells
// out what is about to happen, because a drag that landed one row off is
// otherwise indistinguishable from the one that was meant.
export function ConfirmChangeDialog({
  title,
  summary,
  confirmLabel,
  busy,
  onCancel,
  onConfirm,
}: {
  title: string;
  summary: string;
  confirmLabel: string;
  busy: boolean;
  onCancel: () => void;
  onConfirm: (reason: string) => void;
}) {
  const [reason, setReason] = useState("");

  return (
    <Dialog title={title} onClose={onCancel}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          onConfirm(reason.trim());
        }}
      >
        <p className="rota-edit-summary">{summary}</p>
        <ReasonField value={reason} onChange={setReason} />
        <DialogActions
          confirmLabel={confirmLabel}
          busy={busy}
          canConfirm={reason.trim() !== ""}
          onCancel={onCancel}
        />
      </form>
    </Dialog>
  );
}

// What AssigneeDialog is being used for. Both cases pick a person and a reason;
// they differ in where the incoming person's role comes from.
export type AssigneeChange =
  // leadTaken says the shift already has a team lead. A shift has exactly one,
  // so joining as one is then not offered — and the server refuses it anyway.
  | { kind: "add"; leadTaken: boolean }
  // Whoever comes in takes the outgoing person's place, role included, which is
  // what makes this a replacement rather than a removal followed by an add.
  | { kind: "replace"; outgoing: Assignee };

// AssigneeDialog picks who joins a shift and why — either alongside the people
// already on it, or in place of one of them.
//
// Role is stated rather than inferred: the service infers it from the shift and
// the volunteer's own roster role, and those rules are invisible from here. On
// an add to a leadless shift the admin chooses; everywhere else the answer is
// forced and the dialog says what it is instead of asking.
export function AssigneeDialog({
  dateLabel,
  change,
  volunteers,
  volunteersError,
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
  busy: boolean;
  onCancel: () => void;
  // role is omitted for a custom entry, which the API gives no role to.
  onConfirm: (person: PersonRef, reason: string, role?: Role) => void;
}) {
  const [choice, setChoice] = useState("");
  const [customName, setCustomName] = useState("");
  const [role, setRole] = useState<Role>(SERVICE_VOLUNTEER_ROLE);
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

  // Only the add-to-a-leadless-shift case is the admin's to answer; the other
  // two are settled by the shift itself.
  const incomingRole: Role =
    change.kind === "replace"
      ? change.outgoing.role
      : change.leadTaken
        ? SERVICE_VOLUNTEER_ROLE
        : role;

  function handleChoice(value: string) {
    setChoice(value);
    // Default to the role the volunteer holds on the roster: it is the right
    // answer far more often than not, and the admin can still say otherwise.
    const chosen = volunteers?.find((v) => v.id === value);
    setRole(defaultRoleFor(chosen));
  }

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
            onChange={(e) => handleChoice(e.target.value)}
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
            silently. A visiting group is never the team lead anyway. */}
        {!isCustom &&
          change.kind === "add" &&
          (change.leadTaken ? (
            <p className="rota-edit-note">
              {dateLabel} already has a team lead, so whoever you add joins as a
              service volunteer. To change who leads, replace the team lead
              instead.
            </p>
          ) : (
            <label className="rota-edit-field">
              Role
              <select
                value={role}
                onChange={(e) => setRole(e.target.value as Role)}
              >
                <option value={SERVICE_VOLUNTEER_ROLE}>Volunteer</option>
                <option value={TEAM_LEAD_ROLE}>Team lead</option>
              </select>
            </label>
          ))}

        <ReasonField value={reason} onChange={setReason} />
        <DialogActions
          confirmLabel={change.kind === "add" ? "Add" : "Replace"}
          busy={busy}
          canConfirm={person !== null && reason.trim() !== ""}
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
// Role is only ever a question for a volunteer the roster records as a team
// lead: the API refuses to pin anyone else as one, and a custom entry carries
// no role at all. Where the answer is forced the dialog says what it is rather
// than offering a control with one value in it.
export function PinDialog({
  dateLabel,
  volunteers,
  volunteersError,
  leadPinned,
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
  // Whether this shift's single team-lead slot is already spoken for. A second
  // lead is a 409, and the way out is to remove the pin that holds it — which
  // an admin can do for any of them (issue #131).
  leadPinned: boolean;
  // Everyone already pinned here, by the name shown. A custom entry has no id —
  // its text is its identity — so repeating one is a pin that would be refused.
  pinnedNames: string[];
  busy: boolean;
  onCancel: () => void;
  onConfirm: (person: PersonRef, role: Role) => void;
}) {
  const [choice, setChoice] = useState("");
  const [customName, setCustomName] = useState("");
  const [role, setRole] = useState<Role>(SERVICE_VOLUNTEER_ROLE);

  const isCustom = choice === CUSTOM_CHOICE;
  const trimmedName = customName.trim();
  const duplicateName = isCustom && pinnedNames.includes(trimmedName);
  const person: PersonRef | null = isCustom
    ? trimmedName && !duplicateName
      ? { custom: trimmedName }
      : null
    : choice
      ? { volunteerId: choice }
      : null;

  const chosen = volunteers?.find((v) => v.id === choice) ?? null;
  // The Seat is the admin's to fill only when the person holds the Role and it
  // is not already at its ceiling. Team lead's ceiling is one in S1, so one pin
  // is the whole of it; S3 reads real ceilings from the server.
  const canChooseRole =
    (chosen?.roles.includes(TEAM_LEAD_ROLE) ?? false) && !leadPinned;

  function handleChoice(value: string) {
    setChoice(value);
    // Default to the role the volunteer holds on the roster, as adding to a
    // shift does: pinning a team lead is nearly always pinning them to lead.
    const picked = volunteers?.find((v) => v.id === value);
    setRole(defaultRoleFor(picked));
  }

  return (
    <Dialog title={`Pin someone to ${dateLabel}`} onClose={onCancel}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (person)
            onConfirm(person, canChooseRole ? role : SERVICE_VOLUNTEER_ROLE);
        }}
      >
        <p className="rota-edit-summary">
          Whoever you pin is guaranteed this shift when the rota is allocated.
        </p>

        <label className="rota-edit-field">
          Who
          <select
            value={choice}
            onChange={(e) => handleChoice(e.target.value)}
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

        {/* Said before the pin is attempted rather than after: a repeat is
            refused by the server, and there is no reason to make an admin find
            that out by pressing the button. */}
        {duplicateName && (
          <p className="rota-edit-note">
            {trimmedName} is already pinned to {dateLabel}.
          </p>
        )}

        {canChooseRole && (
          <label className="rota-edit-field">
            Role
            <select
              value={role}
              onChange={(e) => setRole(e.target.value as Role)}
            >
              <option value={TEAM_LEAD_ROLE}>Team lead</option>
              <option value={SERVICE_VOLUNTEER_ROLE}>Volunteer</option>
            </select>
          </label>
        )}

        {/* Says what happens instead as well as that the slot is gone, and how
            to get it back: every pin can be removed, so there is always a way
            through. */}
        {chosen?.roles.includes(TEAM_LEAD_ROLE) && leadPinned && (
          <p className="rota-edit-note">
            {dateLabel} already has a team lead pinned, so {chosen.name} is
            pinned as a service volunteer. To pin a different lead, remove that
            pin first.
          </p>
        )}

        <DialogActions
          confirmLabel="Pin"
          busy={busy}
          canConfirm={person !== null}
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
