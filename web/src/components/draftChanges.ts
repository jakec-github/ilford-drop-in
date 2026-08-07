import type { Assignee, DraftShift } from "../types";

// One difference between the rota an admin was shown and the rota the solver
// produced when they went to allocate it.
//
// Keyed by shift and by person, because that is how the difference reads out
// loud: "Ada is no longer on 9 August". A person whose Role changed is one
// difference rather than a departure and an arrival — same person, same shift,
// and nobody freed up or got taken.
export interface DraftChange {
  shiftId: string;
  kind: "in" | "out" | "role";
  name: string;
  // The Role they are in now, or for a departure the Role they were in.
  role: string;
  // Only on a Role change: the Role they were in before.
  wasRole?: string;
}

// Who a Seat names, as an identity rather than a display: a volunteer by id, or
// a custom entry by the text itself, which is all a custom entry has.
function personKey(assignee: Assignee): string {
  return assignee.volunteerId ?? `custom:${assignee.name}`;
}

function byPerson(assignees: Assignee[]): Map<string, Assignee> {
  return new Map(assignees.map((a) => [personKey(a), a]));
}

// compareDrafts is what changed between two drafts of the same rota, shift by
// shift, in the order the rota is read.
//
// It exists because allocating can refuse: the server re-solves, finds a
// different rota from the one the admin confirmed, and replaces the draft with
// it (ADR 0008). The new rota is already on screen — but "something changed,
// look again" is not an answer an admin can act on, and reading two rotas
// side by side to spot the difference is exactly the work a computer should be
// doing.
export function compareDrafts(
  shown: DraftShift[],
  now: DraftShift[],
): DraftChange[] {
  const nowByShift = new Map(now.map((shift) => [shift.shiftId, shift]));
  const shownByShift = new Map(shown.map((shift) => [shift.shiftId, shift]));

  // Every shift either draft placed anybody on. A shift that emptied entirely
  // is as much a change as one that filled up, and it appears in only one of
  // the two lists.
  const shiftIds = [
    ...new Set([...shown.map((s) => s.shiftId), ...now.map((s) => s.shiftId)]),
  ];

  const changes: DraftChange[] = [];
  for (const shiftId of shiftIds) {
    const before = byPerson(shownByShift.get(shiftId)?.assignees ?? []);
    const after = byPerson(nowByShift.get(shiftId)?.assignees ?? []);

    for (const [key, assignee] of after) {
      const was = before.get(key);
      if (!was) {
        changes.push({
          shiftId,
          kind: "in",
          name: assignee.name,
          role: assignee.role,
        });
      } else if (was.role !== assignee.role) {
        changes.push({
          shiftId,
          kind: "role",
          name: assignee.name,
          role: assignee.role,
          wasRole: was.role,
        });
      }
    }

    for (const [key, assignee] of before) {
      if (!after.has(key)) {
        changes.push({
          shiftId,
          kind: "out",
          name: assignee.name,
          role: assignee.role,
        });
      }
    }
  }
  return changes;
}
