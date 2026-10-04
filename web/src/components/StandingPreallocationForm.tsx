import { useState } from "react";
import Button from "../ui/Button";
import Dialog from "../ui/Dialog";
import type {
  ConfiguredRole,
  NewStandingPreallocation,
  PersonRef,
  Volunteer,
} from "../types";
import { CUSTOM_CHOICE } from "../types";
import { STANDING_RULES } from "./standingRules";

// StandingPreallocationForm is the whole of making one: who, in which Role, on
// which shifts. There is no edit — a promise is made or it is not, and changing
// one is removing it and making the one that was meant — so this only ever
// creates.
export default function StandingPreallocationForm({
  roles,
  volunteers,
  volunteersError,
  onSave,
  onClose,
}: {
  roles: ConfiguredRole[];
  // null while the roster is still loading.
  volunteers: Volunteer[] | null;
  volunteersError: string | null;
  onSave: (standing: NewStandingPreallocation) => Promise<void>;
  onClose: () => void;
}) {
  const [choice, setChoice] = useState("");
  const [customName, setCustomName] = useState("");
  // Empty until somebody picks one; what is actually offered is derived below,
  // so that changing who is being pinned re-defaults the Role while an explicit
  // choice survives it.
  const [roleId, setRoleId] = useState("");
  const [rrule, setRRule] = useState(STANDING_RULES[0].rrule);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const isCustom = choice === CUSTOM_CHOICE;
  const trimmedName = customName.trim();
  const person: PersonRef | null = isCustom
    ? trimmedName
      ? { custom: trimmedName }
      : null
    : choice
      ? { volunteerId: choice }
      : null;

  // Every Role the drop-in has, whoever is being pinned: not holding one is a
  // rule, and rules bind the allocator, not the Organiser (ADR 0010). The
  // default is the highest-priority Role this volunteer holds, since somebody
  // is usually pinned for the job they mostly do; an explicit pick outranks it.
  const chosen = volunteers?.find((v) => v.id === choice) ?? null;
  const pinnedRole =
    roles.find((r) => r.id === roleId) ??
    roles.find((r) => chosen?.roles.includes(r.name)) ??
    roles[0] ??
    null;
  const pinnedRoleId = pinnedRole?.id ?? "";
  // Said, not refused: the pin grants the Role on the shifts it matches.
  const notHeld =
    chosen !== null &&
    pinnedRole !== null &&
    !chosen.roles.includes(pinnedRole.name);

  async function save() {
    if (!person) return;
    setSaving(true);
    setError(null);
    try {
      await onSave({ rrule, roleId: pinnedRoleId, person });
      onClose();
    } catch (err: unknown) {
      // The server's own message names who was already promised those shifts,
      // so it is shown as-is and the form stays open on what was chosen.
      setError(err instanceof Error ? err.message : "Failed to add the pin");
      setSaving(false);
    }
  }

  return (
    <Dialog title="New standing preallocation" onClose={onClose}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <p className="settings-hint">
          Whoever you pin here is pinned to the matching shifts of every rota
          from now on, as it is defined. Rotas that already exist are not
          touched.
        </p>

        <label className="settings-field">
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
          <p className="settings-hint">
            Could not load the roster ({volunteersError}). You can still pin
            someone by name.
          </p>
        )}

        {isCustom && (
          <label className="settings-field">
            Name
            <input
              type="text"
              value={customName}
              onChange={(e) => setCustomName(e.target.value)}
              placeholder="e.g. Redbridge youth group"
            />
          </label>
        )}

        <label className="settings-field">
          Role
          <select
            value={pinnedRoleId}
            onChange={(e) => setRoleId(e.target.value)}
          >
            {roles.map((role) => (
              <option key={role.id} value={role.id}>
                {role.name}
              </option>
            ))}
          </select>
        </label>

        {notHeld && (
          <p className="settings-hint">
            {chosen.name} is not down for {pinnedRole.name} on the roster.
            Pinning them still puts them in it on every shift this matches.
          </p>
        )}

        <label className="settings-field">
          Which shifts
          <select value={rrule} onChange={(e) => setRRule(e.target.value)}>
            {STANDING_RULES.map((rule) => (
              <option key={rule.rrule} value={rule.rrule}>
                {rule.label}
              </option>
            ))}
          </select>
        </label>

        {error && <p className="settings-error">{error}</p>}

        <div className="settings-actions">
          <Button onClick={onClose} disabled={saving}>
            Cancel
          </Button>
          <Button
            type="submit"
            disabled={person === null || pinnedRoleId === "" || saving}
          >
            {saving ? "Saving…" : "Add pin"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
