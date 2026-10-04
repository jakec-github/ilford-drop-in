import type { Role, ShapeSeat } from "../types";

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
