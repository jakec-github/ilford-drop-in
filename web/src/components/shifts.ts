import type { Assignee, PersonRef, Role, RotaShift, ShapeSeat } from "../types";

// The small facts about a shift and the people on it that both the shift rows
// and the screens around them need. Their own module rather than exports from
// ShiftList: a file that exports components exports only components, so that a
// dev-server reload of one row does not tear down the page holding it.

// How a Role reads in a sentence about somebody's place on a shift: " as hot
// food". Every Role is named, because with Roles as configuration there is no
// ordinary one to leave unsaid — a Shape of five says nothing about which of
// them goes without saying, and suppressing one by name is what made the rota
// page unusable on any deployment that did not use the shipped names
// (issue #212).
//
// Empty only for a name the rota records no Role for at all: an allocation
// predating the role column, which has nothing to say rather than something
// suppressed.
export function roleSuffix(role: Role): string {
  return role ? ` as ${role.toLowerCase()}` : "";
}

// A shift that exists but has not been through allocation yet: no assignees,
// and not deliberately closed. Hidden from the public; flagged for Organisers.
export function isUnallocated(shift: RotaShift): boolean {
  return !shift.allocated && !shift.closed;
}

// How the alterations API names one assignee: real volunteers by id, custom
// (manual) entries by their text, which is all they have.
export function personRef(assignee: Assignee): PersonRef {
  return assignee.volunteerId
    ? { volunteerId: assignee.volunteerId }
    : { custom: assignee.name };
}

export function samePerson(a: PersonRef, b: PersonRef): boolean {
  return "volunteerId" in a && "volunteerId" in b
    ? a.volunteerId === b.volunteerId
    : "custom" in a && "custom" in b && a.custom === b.custom;
}

// "2 Feb" — weekday and year are redundant down a list of a rota's own dates.
export function formatShiftDate(dateStr: string): string {
  return new Date(dateStr).toLocaleDateString("en-GB", {
    day: "numeric",
    month: "short",
  });
}

// "Sun 2 Feb" — used where a date is read out of the list's context, in a
// dialog or a screen-reader label, and the weekday stops "2 Feb" reading as a
// date the reader has to look up.
export function formatShiftDateLong(dateStr: string): string {
  return new Date(dateStr).toLocaleDateString("en-GB", {
    weekday: "short",
    day: "numeric",
    month: "short",
  });
}

// RoleGroup is one Role's people on one shift: everybody doing the same job,
// under the name of it.
//
// Generic over who is in it because the two things a shift row shows people for
// are not the same shape — an allocated shift has Assignees, an unallocated one
// has pins and drafted names — and grouping them is the same arithmetic either
// way. All it asks of an entry is the Role it holds.
export interface RoleGroup<T> {
  role: Role;
  people: T[];
}

// groupByRole gathers a shift's people under the Roles they hold, for the
// expanded view of a row — where the Role is named in text rather than left to
// chip colour, which a reader has to already know the convention to decode and
// which says nothing at all to one who cannot see it (issue #66).
//
// The Shape's order first, because that is the order the Seats are filled and
// so the order the shift itself puts its Roles in. Then anything held outside
// it, in the order it appears: an Alteration records what happened on the day
// and is not held to the Shape (ADR 0009), so somebody can genuinely hold a
// Role this shift never asked for, and dropping them would take a name off the
// rota. A Role nobody holds is left out entirely — the expanded view is about
// who is on the shift, not how many Seats went unfilled.
//
// Matched by name rather than by id because that is all a person on a shift
// carries: an Assignee holds the Role it was allocated under, as a name. A Role
// renamed since would fall out of the Shape's order into the trailing group,
// which is the honest answer — the rota records the name it was made with.
export function groupByRole<T extends { role: Role }>(
  shape: ShapeSeat[],
  people: T[],
): RoleGroup<T>[] {
  const groups = new Map<Role, T[]>();

  // Seeded from the Shape so its order wins, then pruned: a Role nobody holds
  // leaves an empty list behind, and an empty list is not a group.
  for (const seat of shape) {
    if (!groups.has(seat.role)) groups.set(seat.role, []);
  }
  for (const person of people) {
    const held = groups.get(person.role);
    if (held) {
      held.push(person);
    } else {
      groups.set(person.role, [person]);
    }
  }

  const grouped = [...groups]
    .filter(([, held]) => held.length > 0)
    .map(([role, held]) => ({ role, people: held }));

  // The unrecorded group last, whatever order it arrived in: it is the one
  // group that is not a job, and it reads as a footnote rather than as the
  // shift's first line.
  return [
    ...grouped.filter(({ role }) => role !== ""),
    ...grouped.filter(({ role }) => role === ""),
  ];
}

// What to head a RoleGroup with. Every Role is its own name; the group holding
// people the rota records no Role for says that, rather than sitting under a
// blank heading the reader has to guess at. Not a Role name invented for the
// occasion — no Role is called this, and none may be (ADR 0009).
export function roleGroupLabel(role: Role): string {
  return role === "" ? "Role not recorded" : role;
}

// Returns the draft deficit for each role in a shifts shape. Only roles with
// a deficit are returned.
//
// Counted per Role, so pins past the Shape (ADR 0010) cannot hide a gap: two
// people in a Role with one Seat leave that Role at no deficit, never a
// negative one that offsets another Role's shortfall, and somebody in a Role
// the Shape asks for none of is not counted against anything.
export function shiftDeficit(
  shape: ShapeSeat[],
  assignees: { role: Role }[],
): { role: string; deficit: number }[] {
  const assigneeCountByRole = assignees.reduce(
    (acc: Record<string, number>, { role }) => {
      if (acc[role]) {
        acc[role] += 1;
      } else {
        acc[role] = 1;
      }
      return acc;
    },
    {},
  );

  return shape
    .map(({ role, count }) => ({
      role,
      // A Role nobody was drafted into is short its whole count, not absent
      // from the answer: without the fallback the subtraction is NaN, NaN > 0
      // is false, and a shift the solver could fill no Seat of some Role on was
      // the one shift that said nothing about it.
      deficit: count - (assigneeCountByRole[role] ?? 0),
    }))
    .filter(({ deficit }) => deficit > 0);
}
