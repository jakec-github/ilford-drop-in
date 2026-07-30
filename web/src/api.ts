import type { RotaShift, Volunteer } from "./types";

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

// syncVolunteers re-reads the roster sheet into the database. The server uses
// its own service account, so this is a plain authenticated POST with no OAuth
// redirect dance.
export async function syncVolunteers(): Promise<void> {
  const res = await fetch("/auth/sync", { method: "POST" });
  if (!res.ok) {
    throw new Error(`Failed to sync volunteers (${res.status})`);
  }
}
