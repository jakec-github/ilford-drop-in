import type { DefinedRota, RotaShift, Volunteer } from "./types";

const TEAM_LEAD_ROLE = "Team lead";

interface ApiAssignee {
  volunteerId?: string;
  customEntry?: string;
  name: string;
  role?: string;
  group?: string;
}

interface ApiShift {
  date: string;
  start: string;
  end: string;
  closed: boolean;
  allocated: boolean;
  assignees: ApiAssignee[];
}

interface ListShiftsResponse {
  shifts: ApiShift[];
}

interface ApiVolunteer {
  id: string;
  name: string;
  fullName: string;
  role?: string;
  group?: string;
  gender?: string;
  active: boolean;
}

interface ListVolunteersResponse {
  volunteers: ApiVolunteer[];
}

interface DefineRotaResponse {
  rotation: { id: string; start: string; end: string; shiftCount: number };
  shifts: { id: string; date: string }[];
}

// The API reports a rejected request as {"error": "..."}, and that message is
// written to be read — "shift count must be positive, got 0" tells an admin what
// to change, where a bare 400 does not. Falls back to the status when the body
// is not one of ours.
async function errorMessage(res: Response, fallback: string): Promise<string> {
  try {
    const body = (await res.json()) as { error?: string };
    if (body.error) return body.error;
  } catch {
    // Not a JSON error body; the status is all we have.
  }
  return `${fallback} (${res.status})`;
}

function toVolunteer(v: ApiVolunteer): Volunteer {
  return {
    id: v.id,
    name: v.name,
    fullName: v.fullName,
    role: v.role === TEAM_LEAD_ROLE ? "lead" : "volunteer",
    group: v.group || null,
    gender: v.gender || null,
    active: v.active,
  };
}

function toRotaShift(shift: ApiShift): RotaShift {
  return {
    date: shift.date,
    closed: shift.closed,
    allocated: shift.allocated,
    // Closed shifts carry no meaningful assignees.
    assignees: shift.closed
      ? []
      : shift.assignees.map((a) => ({
          name: a.name,
          role: a.role === TEAM_LEAD_ROLE ? "lead" : "volunteer",
          custom: !a.volunteerId,
          group: a.group || null,
          volunteerId: a.volunteerId || null,
        })),
  };
}

// fetchCurrentAdmin returns the logged-in admin's email, or null if there is no
// active admin session.
export async function fetchCurrentAdmin(): Promise<string | null> {
  const res = await fetch("/auth/me");
  if (res.status === 401) return null;
  if (!res.ok) {
    throw new Error(`Failed to check login state (${res.status})`);
  }
  const data = (await res.json()) as { email: string };
  return data.email;
}

// logout clears the admin session cookie.
export async function logout(): Promise<void> {
  const res = await fetch("/auth/logout", { method: "POST" });
  if (!res.ok) {
    throw new Error(`Failed to log out (${res.status})`);
  }
}

export async function fetchRota(): Promise<RotaShift[]> {
  const today = new Date().toLocaleDateString("en-CA");
  const res = await fetch(`/shifts?from=${today}`);
  if (!res.ok) {
    throw new Error(`Failed to load shifts (${res.status})`);
  }
  const data = (await res.json()) as ListShiftsResponse;
  return data.shifts.map(toRotaShift);
}

// fetchVolunteers returns the whole synced roster, inactive volunteers included,
// already sorted by name server-side. Admin-only.
export async function fetchVolunteers(): Promise<Volunteer[]> {
  const res = await fetch("/volunteers");
  if (!res.ok) {
    throw new Error(`Failed to load volunteers (${res.status})`);
  }
  const data = (await res.json()) as ListVolunteersResponse;
  return data.volunteers.map(toVolunteer);
}

// defineRota defines the next rota — the weeks after the latest existing one —
// and returns the shifts it minted. Admin-only, and deliberately not idempotent:
// two calls define two consecutive rotas, so the caller is expected to show what
// came back rather than treat it as a repeatable action.
export async function defineRota(shiftCount: number): Promise<DefinedRota> {
  const res = await fetch("/rotations", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ shiftCount }),
  });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "Failed to define the rota"));
  }
  const data = (await res.json()) as DefineRotaResponse;
  return {
    id: data.rotation.id,
    start: data.rotation.start,
    end: data.rotation.end,
    shiftDates: data.shifts.map((s) => s.date),
  };
}

// syncVolunteers re-reads the roster sheet into the database. The server uses
// its own service account, so this is a plain authenticated POST with no OAuth
// redirect dance.
export async function syncVolunteers(): Promise<void> {
  const res = await fetch("/auth/sync", { method: "POST" });
  if (!res.ok) {
    throw new Error(`Failed to sync volunteers (${res.status})`);
  }
}
