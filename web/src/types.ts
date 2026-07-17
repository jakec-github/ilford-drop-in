// Role a person holds on a given shift. Only these two exist; anything the API
// doesn't recognise as team lead is treated as a service volunteer.
export type Role = "lead" | "volunteer";

// Assignee is one person on a shift: a real volunteer or a custom (manual)
// entry. Role is the role held on this shift, not the volunteer's intrinsic
// role. Group is the volunteer's group key, or null for custom/ungrouped.
export interface Assignee {
  name: string;
  role: Role;
  custom: boolean;
  group: string | null;
}

export interface RotaShift {
  date: string;
  closed: boolean;
  // allocated is false for a minted shift whose rota has not been run yet: it
  // exists but has no assignees. Shown only to admins (with a distinct style).
  allocated: boolean;
  assignees: Assignee[];
}
