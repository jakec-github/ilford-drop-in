import { useState } from "react";
import Button from "../ui/Button";
import Dialog from "../ui/Dialog";
import type { ConfiguredRole, ShapeSeat } from "../types";
import "./ShapeForm.css";

// ShapeForm is editing a Shape: every Role, with how many Seats of it is being
// asked for.
//
// One form for both Shapes an admin states — the default one on the settings
// screen, and one Shift's own on the rota (issue #138) — because the question
// is the same in both places and only the words around it differ. What they
// hand in is the same list of Seats, and both are refused by the same rules on
// the server.
//
// Every Role gets a row rather than there being an add-a-Role dance, because a
// drop-in has a handful of Roles and "how many of this one?" is the only
// question there is. Nought is how a Role is left out — the server refuses a
// Seat of nought, so those rows are dropped on the way rather than sent.
//
// Counts are held as strings so that a cleared box stays cleared while it is
// being retyped, rather than snapping to 0 under the cursor.
export default function ShapeForm({
  title,
  intro,
  saveLabel,
  roles,
  shape,
  onSave,
  onClose,
}: {
  title: string;
  // What this Shape is, said in the caller's own terms: the settings screen is
  // stating what every future shift starts from, the rota one evening.
  intro: string;
  saveLabel: string;
  roles: ConfiguredRole[];
  shape: ShapeSeat[];
  onSave: (seats: { roleId: string; count: number }[]) => Promise<void>;
  onClose: () => void;
}) {
  const [counts, setCounts] = useState<Record<string, string>>(() =>
    Object.fromEntries(
      roles.map((role) => [
        role.id,
        String(shape.find((seat) => seat.roleId === role.id)?.count ?? 0),
      ]),
    ),
  );
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const seats = roles
    .map((role) => ({ roleId: role.id, count: Number(counts[role.id] || 0) }))
    .filter((seat) => seat.count > 0);
  const total = seats.reduce((sum, seat) => sum + seat.count, 0);

  async function save() {
    setSaving(true);
    setError(null);
    try {
      await onSave(seats);
      onClose();
    } catch (err: unknown) {
      // The server's own message names the Role whose ceiling was exceeded, or
      // whose Seat somebody is already pinned to, so it is shown as-is and the
      // form stays open on what was typed.
      setError(err instanceof Error ? err.message : "Failed to save the shape");
      setSaving(false);
    }
  }

  return (
    <Dialog title={title} onClose={onClose}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <p className="shape-form-hint">{intro}</p>

        {roles.map((role) => (
          <label key={role.id} className="shape-form-seat">
            <span className="shape-form-role" data-role-colour={role.colour}>
              {role.name}
            </span>
            {/* A capped Role says its ceiling here rather than only refusing at
                it: the box stops at that number, and a number box that will not
                go higher is a puzzle without the reason beside it. */}
            {role.max !== null && (
              <span className="shape-form-max">at most {role.max}</span>
            )}
            <input
              type="number"
              min={0}
              max={role.max ?? undefined}
              value={counts[role.id] ?? "0"}
              onChange={(e) =>
                setCounts({ ...counts, [role.id]: e.target.value })
              }
            />
          </label>
        ))}

        <p className="shape-form-hint">
          {total > 0
            ? `A shift asks for ${total} ${total === 1 ? "person" : "people"} in total.`
            : "A shift asking for nobody cannot be allocated."}
        </p>

        {error && <p className="shape-form-error">{error}</p>}

        <div className="shape-form-actions">
          <Button onClick={onClose} disabled={saving}>
            Cancel
          </Button>
          <Button type="submit" disabled={saving}>
            {saving ? "Saving…" : saveLabel}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
