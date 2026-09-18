import type { Assignee, PersonRef, Role, RotaShift } from "../types";

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
