import type { RotaShift } from "./types";

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
