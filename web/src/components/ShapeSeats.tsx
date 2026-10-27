import { seatsOf, type SeatCounts } from "./shape";
import type { ConfiguredRole } from "../types";
import "./ShapeSeats.css";

// ShapeSeats is the rows of a Shape being edited: every Role with how many
// Seats of it, and what that comes to.
//
// Every Role gets a row rather than there being an add-a-Role dance, because
// an Organisation has a handful of Roles and "how many of this one?" is the
// only question there is.
//
// Its own component because two editors ask it: the Shape dialog, for the
// default Shape on the rota and one Shift's own, and the Rota Defaults editor,
// which states the default Shape alongside the Shift pattern.
export default function ShapeSeats({
  roles,
  counts,
  onChange,
}: {
  roles: ConfiguredRole[];
  counts: SeatCounts;
  onChange: (counts: SeatCounts) => void;
}) {
  const total = seatsOf(roles, counts).reduce((sum, s) => sum + s.count, 0);
  return (
    <>
      {roles.map((role) => (
        <label key={role.id} className="shape-form-seat">
          <span className="shape-form-role" data-role-colour={role.colour}>
            {role.name}
          </span>
          <input
            type="number"
            min={0}
            value={counts[role.id] ?? "0"}
            onChange={(e) => onChange({ ...counts, [role.id]: e.target.value })}
          />
        </label>
      ))}

      <p className="shape-form-hint">
        {total > 0
          ? `A shift asks for ${total} ${total === 1 ? "person" : "people"} in total.`
          : "A shift asking for nobody cannot be allocated."}
      </p>
    </>
  );
}
