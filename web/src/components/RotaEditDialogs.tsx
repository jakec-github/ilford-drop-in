import { useState } from "react";
import Button from "../ui/Button";
import Dialog from "../ui/Dialog";
import type { PersonRef, Role, Volunteer } from "../types";
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

// AddAssigneeDialog picks who joins a shift, in which role, and why. Role is
// asked rather than inferred: an allocated shift already has a team lead, so
// the service would otherwise quietly downgrade an incoming one — fine as a
// default, wrong when the admin is adding a second lead on purpose.
export function AddAssigneeDialog({
  dateLabel,
  volunteers,
  volunteersError,
  busy,
  onCancel,
  onConfirm,
}: {
  dateLabel: string;
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

  function handleChoice(value: string) {
    setChoice(value);
    // Default to the role the volunteer holds on the roster: it is the right
    // answer far more often than not, and the admin can still say otherwise.
    const chosen = volunteers?.find((v) => v.id === value);
    setRole(chosen ? chosen.role : "volunteer");
  }

  return (
    <Dialog title={`Add someone to ${dateLabel}`} onClose={onCancel}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (person)
            onConfirm(person, reason.trim(), isCustom ? undefined : role);
        }}
      >
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
        {!isCustom && (
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
        )}

        <ReasonField value={reason} onChange={setReason} />
        <DialogActions
          confirmLabel="Add"
          busy={busy}
          canConfirm={person !== null && reason.trim() !== ""}
          onCancel={onCancel}
        />
      </form>
    </Dialog>
  );
}
