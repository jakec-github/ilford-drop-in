import { useState } from "react";
import Button from "../ui/Button";
import Dialog from "../ui/Dialog";
import ShapeSeats from "./ShapeSeats";
import { seatCountsOf, seatsKey, seatsOf } from "./shape";
import type { ConfiguredRole, ShapeSeat } from "../types";
import "./ShapeForm.css";

// ShapeForm is editing a Shape: every Role, with how many Seats of it is being
// asked for.
//
// One form for both Shapes an Organiser states — the default one on the settings
// screen, and one Shift's own on the rota (issue #138) — because the question
// is the same in both places and only the words around it differ. What they
// hand in is the same list of Seats, and both are refused by the same rules on
// the server.
//
// The rows themselves are ShapeSeats, which the Rota Defaults editor shares.
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
  const [counts, setCounts] = useState(() => seatCountsOf(roles, shape));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const seats = seatsOf(roles, counts);
  // Saving what is already there would change nothing, and on the Allocation
  // tab it would still mark the draft stale and force a re-solve (issue #149).
  const changed = seatsKey(seats) !== seatsKey(shape);

  async function save() {
    setSaving(true);
    setError(null);
    try {
      await onSave(seats);
      onClose();
    } catch (err: unknown) {
      // The server's own message says what it refused, so it is shown as-is
      // and the form stays open on what was typed.
      setError(err instanceof Error ? err.message : "Failed to save the shape");
      setSaving(false);
    }
  }

  return (
    <Dialog title={title} onClose={onClose} dismissable={!saving}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <p className="shape-form-hint">{intro}</p>

        <ShapeSeats roles={roles} counts={counts} onChange={setCounts} />

        {error && <p className="shape-form-error">{error}</p>}

        <div className="shape-form-actions">
          <Button onClick={onClose} disabled={saving}>
            Cancel
          </Button>
          <Button type="submit" disabled={saving || !changed}>
            {saving ? "Saving…" : saveLabel}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
