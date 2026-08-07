import type { ShapeSeat } from "../types";

// How a Shape reads in a line of prose: "1 Team lead, 4 Service volunteer", in
// the order the Seats are filled.
//
// Its own module rather than living with ShapeForm, because both screens that
// show a Shape describe one before offering to edit it — the settings screen
// for the default, the rota for one shift — and a module that exports a
// component exports only components.
export function describeShape(shape: ShapeSeat[]): string {
  return shape.map((seat) => `${seat.count} ${seat.role}`).join(", ");
}
