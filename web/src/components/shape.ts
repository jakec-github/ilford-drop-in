import type { ConfiguredRole, Role, ShapeSeat } from "../types";

// How a Shape reads in a line of prose: "1 Duty lead, 4 Greeter", in the order
// the Seats are filled.
//
// Its own module rather than living with ShapeForm, because both screens that
// show a Shape describe one before offering to edit it — the settings screen
// for the default, the rota for one shift — and a module that exports a
// component exports only components.
export function describeShape(shape: ShapeSeat[]): string {
  return shape.map((seat) => `${seat.count} ${seat.role}`).join(", ");
}

// SeatCount is one Role's standing on one shift: how many Seats its Shape gives
// that Role, and how many of them are already spoken for.
export interface SeatCount {
  roleId: string;
  role: Role;
  seats: number;
  taken: number;
}

// seatCounts pairs a shift's Shape with what is already promised on it, so a
// picker can say which Roles have a Seat free and which a pin would go past.
//
// Only pinning asks this, and only to word a note. A Shape's Seats bound the
// allocator and nothing else (ADR 0010): a pin past them is honoured, and the
// allocator adds nobody else to that Role. Nothing on the alterations path
// consults it either.
//
// Roles are matched by id rather than by name, as a pin references one: the
// Shape and the pins on a shift both carry the id, and a Role renamed between
// the two would otherwise read as a Seat nobody had taken.
//
// The Shape's order is kept, because it is the order the Seats are filled.
// Anything taken in a Role the Shape does not ask for is left out entirely
// rather than counted against something: a pin may name a Role the Shape has
// none of, and when it does the answer is that this Shape has no Seats of it —
// not that some other Role is fuller than it is.
export function seatCounts(
  shape: ShapeSeat[],
  takenRoleIds: string[],
): SeatCount[] {
  return shape.map((seat) => ({
    roleId: seat.roleId,
    role: seat.role,
    seats: seat.count,
    taken: takenRoleIds.filter((id) => id === seat.roleId).length,
  }));
}

// Counts are held as strings so that a cleared box stays cleared while it is
// being retyped, rather than snapping to 0 under the cursor.
export type SeatCounts = Record<string, string>;

// seatCountsOf is the boxes for a Shape: every Role, at however many Seats of
// it the Shape asks for, nought for one it does not name.
export function seatCountsOf(
  roles: ConfiguredRole[],
  shape: ShapeSeat[],
): SeatCounts {
  return Object.fromEntries(
    roles.map((role) => [
      role.id,
      String(shape.find((seat) => seat.roleId === role.id)?.count ?? 0),
    ]),
  );
}

// seatsOf is what the boxes ask for, as the server takes it. Nought is how a
// Role is left out — the server refuses a Seat of nought, so those rows are
// dropped on the way rather than sent.
export function seatsOf(
  roles: ConfiguredRole[],
  counts: SeatCounts,
): { roleId: string; count: number }[] {
  return roles
    .map((role) => ({ roleId: role.id, count: Number(counts[role.id] || 0) }))
    .filter((seat) => seat.count > 0);
}

// seatsKey says which Seats a list asks for, whatever order they come in and
// whether or not a Role is listed at nought.
export function seatsKey(seats: { roleId: string; count: number }[]): string {
  return seats
    .filter((seat) => seat.count > 0)
    .map((seat) => `${seat.roleId}:${seat.count}`)
    .sort()
    .join(",");
}
