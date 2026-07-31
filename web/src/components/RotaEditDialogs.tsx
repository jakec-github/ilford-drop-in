import { useState } from "react";
import Button from "../ui/Button";
import Dialog from "../ui/Dialog";
import type { Assignee, PersonRef, Role, Volunteer } from "../types";
import "./RotaEditDialogs.css";

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

// The roster option standing for "not one of these people". Custom entries
// cover a visiting group or anyone else who is not a synced volunteer; they
// have no id, so the rota carries their name as typed.
const CUSTOM_CHOICE = "custom";

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
  const [role, setRole] = useState<Role>("volunteer");
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
        ? "volunteer"
        : role;

  function handleChoice(value: string) {
    setChoice(value);
    // Default to the role the volunteer holds on the roster: it is the right
    // answer far more often than not, and the admin can still say otherwise.
    const chosen = volunteers?.find((v) => v.id === value);
    setRole(chosen ? chosen.role : "volunteer");
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
            {change.outgoing.role === "lead" ? ", as team lead" : ""}.
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
                <option value="volunteer">Volunteer</option>
                <option value="lead">Team lead</option>
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
