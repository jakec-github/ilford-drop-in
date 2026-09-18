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
// picker can offer the Seats that are left and name the Roles that are full.
//
// Only pinning asks this. A Shape's Seats bound the allocator and the promises
// made to it, not somebody recording a change to a published rota (ADR 0009) —
// so this answers "what may still be pinned here", and nothing on the
// alterations path consults it.
//
// Roles are matched by id rather than by name, as a pin references one: the
// Shape and the pins on a shift both carry the id, and a Role renamed between
// the two would otherwise read as a Seat nobody had taken.
//
// The Shape's order is kept, because it is the order the Seats are filled, and
// so the order a picker should offer them in. Anything taken in a Role the
// Shape does not ask for is left out entirely rather than counted against
// something: a pin can outlive its Role leaving a Shape, and when it does the
// answer is that this Shape has no Seats of it — not that some other Role is
// fuller than it is.
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
