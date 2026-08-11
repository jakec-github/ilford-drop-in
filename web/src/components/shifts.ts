import type { Assignee, PersonRef, RotaShift } from "../types";

// The small facts about a shift and the people on it that both the shift rows
// and the screens around them need. Their own module rather than exports from
// ShiftList: a file that exports components exports only components, so that a
// dev-server reload of one row does not tear down the page holding it.

// A shift that exists but has not been through allocation yet: no assignees,
// and not deliberately closed. Hidden from the public; flagged for admins.
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
