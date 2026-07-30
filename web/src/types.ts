// Role a person holds on a given shift. Only these two exist; anything the API
// doesn't recognise as team lead is treated as a service volunteer.
export type Role = "lead" | "volunteer";

// Assignee is one person on a shift: a real volunteer or a custom (manual)
// entry. Role is the role held on this shift, not the volunteer's intrinsic
// role. Group is the volunteer's group key, or null for custom/ungrouped.
// volunteerId is the real volunteer's id, or null for custom entries; it keys
// their ICS calendar feed.
export interface Assignee {
  name: string;
  role: Role;
  custom: boolean;
  group: string | null;
  volunteerId: string | null;
}

// Volunteer is one entry of the admin roster. Role is the volunteer's intrinsic
// role, unlike Assignee.role which is the role held on one shift. Group is their
// group key, or null when they are not in one.
//
// gender is free text as recorded on the roster sheet, so it is shown as-is and
// null when nothing was recorded — never inferred. active is false for someone
// who has stopped volunteering: the roster lists them rather than hiding them.
export interface Volunteer {
  id: string;
  name: string;
  role: Role;
  group: string | null;
  gender: string | null;
  active: boolean;
}

export interface RotaShift {
  date: string;
  closed: boolean;
  // allocated is false for a minted shift whose rota has not been run yet: it
  // exists but has no assignees. Shown only to admins (with a distinct style).
  allocated: boolean;
  assignees: Assignee[];
}
